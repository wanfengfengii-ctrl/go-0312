package service_test

import (
	"context"
	"errors"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
)

func TestModel_MaterialBalanceTracksCommittedLedger(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *service.Service)
	}{
		{
			name: "a second operation cannot spend the first operation's grams again",
			run: func(t *testing.T, svc *service.Service) {
				ctx := context.Background()
				if _, err := svc.MaterialOperation(ctx, "register-P-1", service.MaterialOperationRequest{
					Type: service.OpRegister, PackageID: "P-1", BatchID: "B-1", Grams: 100,
				}); err != nil {
					t.Fatalf("register package: %v", err)
				}

				first, err := svc.MaterialOperation(ctx, "issue-1", service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-1", Grams: 60, Timestamp: 1,
				})
				if err != nil {
					t.Fatalf("first issue: %v", err)
				}
				if first.Balance != 40 {
					t.Fatalf("first issue balance = %d, want 40", first.Balance)
				}

				overdraw := service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-1", Grams: 60, OvenID: "OV-overdraw",
					LeaseStart: 10, LeaseEnd: 20, Timestamp: 2,
				}
				_, err = svc.MaterialOperation(ctx, "issue-2", overdraw)
				var domainErr *domain.DomainError
				if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeMaterialOverdrawn {
					t.Fatalf("second issue error = %v, want MATERIAL_OVERDRAWN", err)
				}

				ledger, err := svc.Store().Ledger(ctx, material.PackageID("P-1"))
				if err != nil {
					t.Fatalf("read ledger: %v", err)
				}
				if ledger.Balance() != 40 || len(ledger.Entries) != 2 {
					t.Fatalf("ledger after rejected issue = %d with %d entries, want 40 with 2 entries", ledger.Balance(), len(ledger.Entries))
				}
				if _, err := svc.Store().Idempotency(ctx, "issue-2"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("rejected issue idempotency record error = %v, want store.ErrNotFound", err)
				}
				if _, err := svc.Store().ContainerOccupancy(ctx, "OV-overdraw"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("rejected issue container occupancy error = %v, want store.ErrNotFound", err)
				}
				leases, err := svc.Store().Leases(ctx, "OV-overdraw")
				if err != nil || len(leases) != 0 {
					t.Fatalf("leases after rejected issue = %+v, err = %v; want none", leases, err)
				}

				corrected := overdraw
				corrected.Grams = 40
				accepted, err := svc.MaterialOperation(ctx, "issue-2", corrected)
				if err != nil || accepted.Balance != 0 {
					t.Fatalf("corrected retry = %+v, err = %v; want balance 0", accepted, err)
				}
				replayed, err := svc.MaterialOperation(ctx, "issue-2", corrected)
				if err != nil || replayed.Balance != accepted.Balance {
					t.Fatalf("safe replay = %+v, err = %v; want unchanged balance %d", replayed, err, accepted.Balance)
				}
			},
		},
		{
			name: "a rolled back issue leaves the committed balance spendable",
			run: func(t *testing.T, svc *service.Service) {
				ctx := context.Background()
				for _, req := range []service.MaterialOperationRequest{
					{Type: service.OpRegister, PackageID: "P-blocker", BatchID: "B-blocker", Grams: 100},
					{Type: service.OpRegister, PackageID: "P-target", BatchID: "B-target", Grams: 100},
				} {
					if _, err := svc.MaterialOperation(ctx, "register-"+req.PackageID, req); err != nil {
						t.Fatalf("register %s: %v", req.PackageID, err)
					}
				}
				if _, err := svc.MaterialOperation(ctx, "occupy-oven", service.MaterialOperationRequest{
					Type: service.OpDry, PackageID: "P-blocker", OvenID: "OV-busy", Timestamp: 1,
				}); err != nil {
					t.Fatalf("occupy oven: %v", err)
				}

				failed := service.MaterialOperationRequest{
					Type: service.OpIssue, PackageID: "P-target", Grams: 60, OvenID: "OV-busy", Timestamp: 2,
				}
				_, err := svc.MaterialOperation(ctx, "target-issue", failed)
				var domainErr *domain.DomainError
				if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeContainerContam {
					t.Fatalf("conflicting issue error = %v, want CONTAINER_CONTAMINATION", err)
				}

				failed.OvenID = ""
				accepted, err := svc.MaterialOperation(ctx, "target-issue", failed)
				if err != nil || accepted.Balance != 40 {
					t.Fatalf("retry after rollback = %+v, err = %v; want balance 40", accepted, err)
				}
				ledger, err := svc.Store().Ledger(ctx, material.PackageID("P-target"))
				if err != nil || ledger.Balance() != 40 || len(ledger.Entries) != 2 {
					t.Fatalf("target ledger = %+v, err = %v; want two entries and balance 40", ledger, err)
				}
			},
		},
		{
			name: "different packages keep independent balances",
			run: func(t *testing.T, svc *service.Service) {
				ctx := context.Background()
				for _, packageID := range []string{"P-A", "P-B"} {
					if _, err := svc.MaterialOperation(ctx, "register-"+packageID, service.MaterialOperationRequest{
						Type: service.OpRegister, PackageID: packageID, BatchID: "batch-" + packageID, Grams: 100,
					}); err != nil {
						t.Fatalf("register %s: %v", packageID, err)
					}
					got, err := svc.MaterialOperation(ctx, "issue-"+packageID, service.MaterialOperationRequest{
						Type: service.OpIssue, PackageID: packageID, Grams: 60,
					})
					if err != nil || got.Balance != 40 {
						t.Fatalf("issue %s = %+v, err = %v; want balance 40", packageID, got, err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			tt.run(t, service.New(st))
		})
	}
}
