package service_test

import (
	"context"
	"errors"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

func TestModel_DeviceCallPayloadIsIsolatedFromLaterRepairGeneration(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)

	_, err = svc.CreateRevision(ctx, "revision", service.CreateRevisionRequest{
		ID:             "revision-1",
		DesignID:       "design-1",
		DesignVersion:  1,
		ProcessID:      "process-1",
		ProcessVersion: 1,
		EffectiveTime:  1,
		MaterialRules: []catalog.MaterialRule{{
			HeatNumber: "heat-1", Thickness: 30_000_000, BatchID: "batch-1", BatchSpec: "ER50-6",
		}},
		ThresholdSets: []catalog.ThresholdSet{{
			ID: "threshold-1", InterpassMin: domain.MustFixed(100, 0), InterpassMax: domain.MustFixed(300, 0), PreheatCoverage: domain.MustFixed(100, 0),
		}},
	})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	_, err = svc.CreateTask(ctx, "task", service.CreateTaskRequest{
		ID: "task-1", Zone: "zone", Component: "component", Node: "node", DesignEnd: 1_000_000,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	locked, err := svc.LockTask(ctx, "lock", "task-1", service.LockTaskRequest{
		DesignID:         "design-1",
		DesignVersion:    1,
		ProcessID:        "process-1",
		ProcessVersion:   1,
		RevisionID:       "revision-1",
		SectionHeat:      "heat-1",
		SectionThickness: 30_000_000,
		GrooveZones: []task.GrooveZone{{
			ID: "zone-1", Side: task.SideA, Interval: domain.Interval{Start: 0, End: 1_000_000},
		}},
		Passes: []task.WeldPass{{
			ID: "pass-1", Side: task.SideA, Sequence: 1, LayerID: "layer-1", ZoneID: "zone-1", Heat: "heat-1", Holding: "holding-1", Interval: domain.Interval{Start: 0, End: 1_000_000},
		}},
	})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}

	oldPending, err := svc.WriteEvidence(ctx, "old-preheat", locked.ID, service.EvidenceRequest{
		Kind:        string(thermal.KindPreheat),
		Generation:  int64(locked.Generation),
		LogicalTime: 10,
		Temperature: domain.MustFixed(15125, 2),
		Coverage:    domain.MustFixed(100, 0),
		ResourceID:  "thermocouple-old",
	})
	if err != nil {
		t.Fatalf("start old preheat call: %v", err)
	}
	if oldPending.Accepted || oldPending.DeviceCallID == "" {
		t.Fatalf("old preheat first attempt should be pending, got %+v", oldPending)
	}
	defect, err := svc.CreateDefect(ctx, "defect", locked.ID, service.CreateDefectRequest{
		Grade: "SLAG", Start: 100, End: 200, PassIDs: []string{"pass-1"}, DetectedAt: 20,
	})
	if err != nil {
		t.Fatalf("create defect: %v", err)
	}
	_, err = svc.CreateRepair(ctx, "repair", locked.ID, service.CreateRepairRequest{
		DefectID: defect.ID, GougeVolume: 1000, CreatedAt: 21,
	})
	if err != nil {
		t.Fatalf("create repair generation: %v", err)
	}

	var currentCallID string
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "superseded call keeps its malformed retry",
			run: func(t *testing.T) {
				got, err := svc.RetryDeviceCall(ctx, "old-retry-malformed", oldPending.DeviceCallID, service.RetryDeviceCallRequest{LogicalTime: 30})
				if err != nil {
					t.Fatalf("retry old call: %v", err)
				}
				if got.Status != material.DevicePending || got.RetrySeq != 1 || got.Outcome != material.ScriptMalformed || got.Evidence != nil {
					t.Fatalf("old call malformed retry = %+v", got)
				}
			},
		},
		{
			name: "superseded success remains audit only",
			run: func(t *testing.T) {
				got, err := svc.RetryDeviceCall(ctx, "old-retry-success", oldPending.DeviceCallID, service.RetryDeviceCallRequest{LogicalTime: 31})
				if err != nil {
					t.Fatalf("complete old call: %v", err)
				}
				if got.Status != material.DeviceSucceeded || got.RetrySeq != 2 || got.Outcome != material.ScriptSuccess {
					t.Fatalf("old call completion = %+v", got)
				}
				events, err := svc.Store().Evidence(ctx, locked.ID)
				if err != nil {
					t.Fatalf("read evidence: %v", err)
				}
				if len(events) != 0 {
					t.Fatalf("superseded payload leaked into current evidence: %+v", events)
				}
			},
		},
		{
			name: "old reading cannot admit current weld",
			run: func(t *testing.T) {
				_, err := svc.WriteEvidence(ctx, "weld-before-current-preheat", locked.ID, service.EvidenceRequest{
					Kind: string(thermal.KindWeldPass), Generation: 2, LogicalTime: 32, PassID: "pass-1", Temperature: domain.MustFixed(180, 0),
				})
				var de *domain.DomainError
				if !errors.As(err, &de) || de.Code != domain.CodeThermalOutOfRange {
					t.Fatalf("current weld should require current preheat, got %v", err)
				}
				graph, err := svc.GetGraph(ctx, locked.ID)
				if err != nil {
					t.Fatalf("get graph: %v", err)
				}
				if graph.Generation != 2 || len(graph.Completed) != 0 {
					t.Fatalf("old call advanced current graph: %+v", graph)
				}
			},
		},
		{
			name: "current call starts pending",
			run: func(t *testing.T) {
				got, err := svc.WriteEvidence(ctx, "current-preheat", locked.ID, service.EvidenceRequest{
					Kind: string(thermal.KindPreheat), Generation: 2, LogicalTime: 40, Temperature: domain.MustFixed(1675, 1), Coverage: domain.MustFixed(100, 0), ResourceID: "thermocouple-current",
				})
				if err != nil {
					t.Fatalf("start current preheat: %v", err)
				}
				if got.Accepted || got.DeviceCallID == "" {
					t.Fatalf("current preheat first attempt should be pending, got %+v", got)
				}
				currentCallID = got.DeviceCallID
			},
		},
		{
			name: "current call is malformed once",
			run: func(t *testing.T) {
				got, err := svc.RetryDeviceCall(ctx, "current-retry-malformed", currentCallID, service.RetryDeviceCallRequest{LogicalTime: 41})
				if err != nil {
					t.Fatalf("retry current call: %v", err)
				}
				if got.Status != material.DevicePending || got.RetrySeq != 1 || got.Outcome != material.ScriptMalformed || got.Evidence != nil {
					t.Fatalf("current malformed retry = %+v", got)
				}
			},
		},
		{
			name: "current success records its real reading",
			run: func(t *testing.T) {
				got, err := svc.RetryDeviceCall(ctx, "current-retry-success", currentCallID, service.RetryDeviceCallRequest{LogicalTime: 42})
				if err != nil {
					t.Fatalf("complete current call: %v", err)
				}
				if got.Status != material.DeviceSucceeded || got.RetrySeq != 2 || got.Outcome != material.ScriptSuccess || got.Evidence == nil || !got.Evidence.Accepted || !got.Evidence.BarrierEstablished {
					t.Fatalf("current call completion = %+v", got)
				}
				events, err := svc.Store().Evidence(ctx, locked.ID)
				if err != nil {
					t.Fatalf("read evidence: %v", err)
				}
				wantTemperature := domain.MustFixed(1675, 1)
				wantCoverage := domain.MustFixed(100, 0)
				if len(events) != 1 || events[0].Generation != 2 || events[0].Kind != thermal.KindPreheat || events[0].Temperature.Cmp(wantTemperature) != 0 || events[0].Coverage.Cmp(wantCoverage) != 0 {
					t.Fatalf("current real reading not recorded exactly once: %+v", events)
				}
			},
		},
		{
			name: "current weld becomes effective after current reading",
			run: func(t *testing.T) {
				got, err := svc.WriteEvidence(ctx, "weld-after-current-preheat", locked.ID, service.EvidenceRequest{
					Kind: string(thermal.KindWeldPass), Generation: 2, LogicalTime: 50, PassID: "pass-1", Temperature: domain.MustFixed(180, 0),
				})
				if err != nil {
					t.Fatalf("write current weld: %v", err)
				}
				if !got.Accepted || got.EventID == "" || got.PrefixVersion != 1 {
					t.Fatalf("current weld result = %+v", got)
				}
				graph, err := svc.GetGraph(ctx, locked.ID)
				if err != nil {
					t.Fatalf("get graph: %v", err)
				}
				if len(graph.Completed) != 1 || graph.Completed[0] != "pass-1" {
					t.Fatalf("current weld missing from graph: %+v", graph)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
