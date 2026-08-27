package service

import (
	"context"
	"errors"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

func TestModel_WeldPassRequiresActiveResourceLease(t *testing.T) {
	tests := []struct {
		name          string
		resourceID    string
		leaseResource string
		leaseStart    domain.Milliseconds
		leaseEnd      domain.Milliseconds
		logicalTime   domain.Milliseconds
		wantExpired   bool
	}{
		{name: "no lease", resourceID: "W-1", logicalTime: 50, wantExpired: true},
		{name: "different resource lease", resourceID: "W-1", leaseResource: "W-2", leaseStart: 0, leaseEnd: 200, logicalTime: 50, wantExpired: true},
		{name: "before lease start", resourceID: "W-1", leaseResource: "W-1", leaseStart: 20, leaseEnd: 100, logicalTime: 19, wantExpired: true},
		{name: "exactly at lease end", resourceID: "W-1", leaseResource: "W-1", leaseStart: 0, leaseEnd: 100, logicalTime: 100, wantExpired: true},
		{name: "after lease end", resourceID: "W-1", leaseResource: "W-1", leaseStart: 0, leaseEnd: 100, logicalTime: 150, wantExpired: true},
		{name: "active lease", resourceID: "W-1", leaseResource: "W-1", leaseStart: 0, leaseEnd: 100, logicalTime: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newMemService(t)
			seedRevision(t, svc)
			tsk := lockTask(t, svc)

			if err := svc.store.SaveThermalBarrier(ctx, tsk.ID, true, 1); err != nil {
				t.Fatalf("establish thermal barrier: %v", err)
			}
			if err := svc.store.SavePassPrefix(ctx, tsk.ID, nil, 7); err != nil {
				t.Fatalf("seed pass prefix: %v", err)
			}
			if tt.leaseResource != "" {
				_, err := svc.AcquireLease(ctx, "lease-"+tt.name, AcquireLeaseRequest{
					ResourceID: tt.leaseResource,
					Operation:  "WELD",
					Start:      tt.leaseStart,
					End:        tt.leaseEnd,
				})
				if err != nil {
					t.Fatalf("acquire lease: %v", err)
				}
			}

			res, err := svc.WriteEvidence(ctx, "weld-"+tt.name, tsk.ID, EvidenceRequest{
				Kind:        string(thermal.KindWeldPass),
				Generation:  1,
				LogicalTime: tt.logicalTime,
				PassID:      "A1",
				Temperature: domain.MustFixed(150, 0),
				ResourceID:  tt.resourceID,
			})

			if tt.wantExpired {
				var de *domain.DomainError
				if !errors.As(err, &de) || de.Code != domain.CodeLeaseExpired {
					t.Fatalf("expected LEASE_EXPIRED, got result %+v, error %v", res, err)
				}
				events, readErr := svc.store.Evidence(ctx, tsk.ID)
				if readErr != nil {
					t.Fatalf("read evidence after rejection: %v", readErr)
				}
				if len(events) != 0 {
					t.Fatalf("rejected weld appended evidence: %+v", events)
				}
				prefix, readErr := svc.store.PassPrefix(ctx, tsk.ID)
				if readErr != nil {
					t.Fatalf("read prefix after rejection: %v", readErr)
				}
				if prefix.Version != 7 || len(prefix.Completed) != 0 {
					t.Fatalf("rejected weld changed prefix: %+v", prefix)
				}
				barrier, readErr := svc.store.ThermalBarrier(ctx, tsk.ID)
				if readErr != nil || !barrier.Established {
					t.Fatalf("rejected weld changed thermal barrier: %+v, %v", barrier, readErr)
				}
				return
			}

			if err != nil || !res.Accepted {
				t.Fatalf("valid active lease was rejected: result %+v, error %v", res, err)
			}
			events, readErr := svc.store.Evidence(ctx, tsk.ID)
			if readErr != nil || len(events) != 1 || events[0].Kind != thermal.KindWeldPass {
				t.Fatalf("valid weld evidence not recorded once: %+v, %v", events, readErr)
			}
			prefix, readErr := svc.store.PassPrefix(ctx, tsk.ID)
			if readErr != nil || prefix.Version != 8 || len(prefix.Completed) != 1 || prefix.Completed[0] != "A1" {
				t.Fatalf("valid weld did not advance prefix: %+v, %v", prefix, readErr)
			}
		})
	}
}
