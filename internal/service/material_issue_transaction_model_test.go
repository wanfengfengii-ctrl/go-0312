package service_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func TestModel_MaterialIssueTransactionBoundary(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, svc *service.Service, st store.Store)
	}{
		{
			name: "overlapping oven lease rejects issue without committing any boundary state",
			run: func(t *testing.T, ctx context.Context, svc *service.Service, st store.Store) {
				if _, err := svc.MaterialOperation(ctx, "register-p1", service.MaterialOperationRequest{
					Type: service.OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 100,
				}); err != nil {
					t.Fatalf("register package: %v", err)
				}
				if _, err := svc.AcquireLease(ctx, "reserve-oven", service.AcquireLeaseRequest{
					ResourceID: "OV-1", Operation: service.OpHold, Start: 0, End: 100,
				}); err != nil {
					t.Fatalf("reserve oven: %v", err)
				}

				got, err := svc.MaterialOperation(ctx, "issue-p1", service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-1", Grams: 60, OvenID: "OV-1",
					LeaseStart: 50, LeaseEnd: 150, Timestamp: 50,
				})
				var domainErr *domain.DomainError
				if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeLeaseConflict {
					t.Fatalf("issue error = %v, want %s", err, domain.CodeLeaseConflict)
				}
				if got != (service.MaterialOperationResult{}) {
					t.Fatalf("failed issue returned committed state: %+v", got)
				}
				ledger, err := st.Ledger(ctx, material.PackageID("P-1"))
				if err != nil {
					t.Fatalf("load ledger: %v", err)
				}
				if ledger.Balance() != 100 || len(ledger.Entries) != 1 {
					t.Fatalf("ledger changed after lease conflict: balance=%d entries=%d", ledger.Balance(), len(ledger.Entries))
				}
				if occupancy, err := st.ContainerOccupancy(ctx, "OV-1"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("oven occupied after lease conflict: occupancy=%+v err=%v", occupancy, err)
				}
				leases, err := st.Leases(ctx, "OV-1")
				if err != nil || len(leases) != 1 || leases[0].Start != 0 || leases[0].End != 100 {
					t.Fatalf("leases changed after conflict: leases=%+v err=%v", leases, err)
				}
				if record, err := st.Idempotency(ctx, "issue-p1"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed issue saved Operation-Id response: record=%+v err=%v", record, err)
				}
			},
		},
		{
			name: "nonconflicting leased issue returns and replays balance holding and lease",
			run: func(t *testing.T, ctx context.Context, svc *service.Service, st store.Store) {
				if _, err := svc.MaterialOperation(ctx, "register-p1", service.MaterialOperationRequest{
					Type: service.OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 100,
				}); err != nil {
					t.Fatalf("register package: %v", err)
				}
				req := service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-1", Grams: 60, OvenID: "OV-1",
					LeaseStart: 50, LeaseEnd: 150, Timestamp: 50,
				}
				got, err := svc.MaterialOperation(ctx, "issue-p1", req)
				if err != nil {
					t.Fatalf("issue: %v", err)
				}
				if got.Balance != 40 || got.Holding == nil || got.Holding.PackageID != "P-1" || got.Holding.OvenID != "OV-1" {
					t.Fatalf("issue result missing balance or holding: %+v", got)
				}
				if got.Lease == nil || got.Lease.ResourceID != "OV-1" || got.Lease.Start != 50 || got.Lease.End != 150 {
					t.Fatalf("issue result missing lease: %+v", got)
				}
				replay, err := svc.MaterialOperation(ctx, "issue-p1", req)
				if err != nil || !reflect.DeepEqual(replay, got) {
					t.Fatalf("Operation-Id replay = %+v, %v; want %+v", replay, err, got)
				}
				ledger, err := st.Ledger(ctx, material.PackageID("P-1"))
				if err != nil || ledger.Balance() != 40 || len(ledger.Entries) != 2 {
					t.Fatalf("replay changed ledger: balance=%d entries=%d err=%v", ledger.Balance(), len(ledger.Entries), err)
				}
			},
		},
		{
			name: "issue without oven resource preserves ordinary material behavior",
			run: func(t *testing.T, ctx context.Context, svc *service.Service, st store.Store) {
				if _, err := svc.MaterialOperation(ctx, "register-p1", service.MaterialOperationRequest{
					Type: service.OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 100,
				}); err != nil {
					t.Fatalf("register package: %v", err)
				}
				got, err := svc.MaterialOperation(ctx, "issue-p1", service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-1", Grams: 60, Timestamp: 50,
				})
				if err != nil || got.Balance != 40 || got.Disposition != material.DispositionIssued || got.Lease != nil {
					t.Fatalf("resource-free issue = %+v, %v", got, err)
				}
			},
		},
		{
			name: "cross-batch container rejection rolls back issue",
			run: func(t *testing.T, ctx context.Context, svc *service.Service, st store.Store) {
				for _, registration := range []service.MaterialOperationRequest{
					{Type: service.OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 100},
					{Type: service.OpRegister, PackageID: "P-2", BatchID: "B-2", Grams: 100},
				} {
					if _, err := svc.MaterialOperation(ctx, "register-"+registration.PackageID, registration); err != nil {
						t.Fatalf("register %s: %v", registration.PackageID, err)
					}
				}
				if _, err := svc.MaterialOperation(ctx, "hold-p1", service.MaterialOperationRequest{
					Type: service.OpHold, PackageID: "P-1", OvenID: "OV-1", Timestamp: 10,
				}); err != nil {
					t.Fatalf("occupy oven: %v", err)
				}
				_, err := svc.MaterialOperation(ctx, "issue-p2", service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-2", Grams: 60, OvenID: "OV-1", Timestamp: 20,
				})
				var domainErr *domain.DomainError
				if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeContainerContam {
					t.Fatalf("issue error = %v, want %s", err, domain.CodeContainerContam)
				}
				ledger, ledgerErr := st.Ledger(ctx, material.PackageID("P-2"))
				if ledgerErr != nil || ledger.Balance() != 100 || len(ledger.Entries) != 1 {
					t.Fatalf("rejected issue changed ledger: balance=%d entries=%d err=%v", ledger.Balance(), len(ledger.Entries), ledgerErr)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			test.run(t, context.Background(), service.New(st), st)
		})
	}
}
