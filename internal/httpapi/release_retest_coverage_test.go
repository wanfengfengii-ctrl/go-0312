package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/httpapi"
	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/service"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

func TestModel_ReleaseRequiresCompleteRepairRetestCoverage(t *testing.T) {
	tests := []struct {
		name        string
		withRepair  bool
		zoneIDs     []string
		wantRelease bool
	}{
		{name: "no repair keeps release behavior", wantRelease: true},
		{name: "complete affected-zone coverage releases", withRepair: true, zoneIDs: []string{"z1", "z2", "z3"}, wantRelease: true},
		{name: "empty coverage is rejected", withRepair: true, zoneIDs: []string{}, wantRelease: false},
		{name: "unrelated zone is rejected", withRepair: true, zoneIDs: []string{"outside"}, wantRelease: false},
		{name: "partial affected-zone coverage is rejected", withRepair: true, zoneIDs: []string{"z1"}, wantRelease: false},
	}

	for caseIndex, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := service.New(st)

			_, err = svc.CreateRevision(ctx, "revision", service.CreateRevisionRequest{
				ID: "R1", DesignID: "D1", DesignVersion: 1, ProcessID: "P1", ProcessVersion: 1, EffectiveTime: 1,
				MaterialRules: []catalog.MaterialRule{{HeatNumber: "H-100", Thickness: 30000000, BatchID: "B1", BatchSpec: "ER50-6"}},
				Qualifications: []catalog.Qualification{
					{PersonID: "alice", Role: "WELD_INSPECTOR", ValidFrom: 0, ValidTo: 1000},
					{PersonID: "bob", Role: "WELD_INSPECTOR", ValidFrom: 0, ValidTo: 1000},
				},
			})
			if err != nil {
				t.Fatalf("create revision: %v", err)
			}

			zones := []task.GrooveZone{
				{ID: "z1", Side: task.SideA, Interval: domain.Interval{Start: 0, End: 100}},
				{ID: "z2", Side: task.SideA, Interval: domain.Interval{Start: 100, End: 200}},
				{ID: "z3", Side: task.SideA, Interval: domain.Interval{Start: 200, End: 300}},
			}
			passes := []task.WeldPass{
				{ID: "A1", Side: task.SideA, Sequence: 1, LayerID: "L1", ZoneID: "z1", Heat: "H-100", Holding: "HG-1", Interval: zones[0].Interval},
				{ID: "A2", Side: task.SideA, Sequence: 2, LayerID: "L2", ZoneID: "z2", Heat: "H-100", Holding: "HG-2", Interval: zones[1].Interval},
				{ID: "A3", Side: task.SideA, Sequence: 3, LayerID: "L3", ZoneID: "z3", Heat: "H-100", Holding: "HG-3", Interval: zones[2].Interval},
			}
			if _, err := svc.CreateTask(ctx, "task", service.CreateTaskRequest{ID: "T1", Zone: "Z", Component: "C", Node: "N", DesignEnd: 300}); err != nil {
				t.Fatalf("create task: %v", err)
			}
			locked, err := svc.LockTask(ctx, "lock", task.TaskID("T1"), service.LockTaskRequest{
				DesignID: "D1", DesignVersion: 1, ProcessID: "P1", ProcessVersion: 1, RevisionID: "R1",
				SectionHeat: "H-100", SectionThickness: 30000000, GrooveZones: zones, Passes: passes,
			})
			if err != nil {
				t.Fatalf("lock task: %v", err)
			}
			if err := st.SavePassPrefix(ctx, locked.ID, []string{"A1", "A2", "A3"}, 1); err != nil {
				t.Fatalf("save closed prefix: %v", err)
			}
			if err := st.SaveThermalBarrier(ctx, locked.ID, true, 1); err != nil {
				t.Fatalf("save thermal barrier: %v", err)
			}
			for i, kind := range []thermal.EvidenceKind{thermal.KindVisual, thermal.KindUltrasonic} {
				if err := st.AppendEvidence(ctx, thermal.EvidenceEvent{ID: string(rune('v' + i)), TaskID: "T1", Generation: 1, Kind: kind, LogicalTime: 20}); err != nil {
					t.Fatalf("append %s evidence: %v", kind, err)
				}
			}
			for i, person := range []string{"alice", "bob"} {
				if err := st.CreateReview(ctx, repair.Review{ID: string(rune('a' + i)), TaskID: "T1", PersonID: person, Role: "WELD_INSPECTOR", Qualified: true, CreatedAt: 30}); err != nil {
					t.Fatalf("create review: %v", err)
				}
			}

			if tc.withRepair {
				defect, err := svc.CreateDefect(ctx, "defect", locked.ID, service.CreateDefectRequest{Grade: "SLAG", Start: 10, End: 20, PassIDs: []string{"A1"}, DetectedAt: 40})
				if err != nil {
					t.Fatalf("create defect: %v", err)
				}
				generation, err := svc.CreateRepair(ctx, "repair", locked.ID, service.CreateRepairRequest{DefectID: defect.ID, GougeVolume: 10, CreatedAt: 41})
				if err != nil {
					t.Fatalf("create repair: %v", err)
				}
				if len(generation.Members) != 3 {
					t.Fatalf("repair closure has %d members, want 3: %+v", len(generation.Members), generation.Members)
				}
				if _, err := svc.CreateRetest(ctx, "retest", locked.ID, service.CreateRetestRequest{RepairID: generation.ID, ZoneIDs: tc.zoneIDs, Passed: true, CreatedAt: 50}); err != nil {
					t.Fatalf("create retest: %v", err)
				}
			}

			body, err := json.Marshal(service.CreateVerdictRequest{Type: "RELEASE", CreatedAt: 60})
			if err != nil {
				t.Fatalf("marshal verdict request: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/tasks/T1/verdicts", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Operation-Id", "verdict-"+string(rune('0'+caseIndex)))
			response := httptest.NewRecorder()
			httpapi.NewServerWithStore(st).Handler().ServeHTTP(response, req)

			if tc.wantRelease {
				if response.Code != http.StatusOK {
					t.Fatalf("release status = %d, want 200; body=%s", response.Code, response.Body.String())
				}
				var verdict repair.TerminalVerdict
				if err := json.Unmarshal(response.Body.Bytes(), &verdict); err != nil || verdict.Type != repair.VerdictRelease || verdict.Credential == "" {
					t.Fatalf("release response = %s, decode error=%v", response.Body.String(), err)
				}
				return
			}

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("incomplete retest status = %d, want 422; body=%s", response.Code, response.Body.String())
			}
			var failure struct {
				Code string `json:"code"`
				Path string `json:"path"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatalf("decode rejection: %v", err)
			}
			if failure.Code != string(domain.CodePrefixViolation) || failure.Path != "service.verdict" {
				t.Fatalf("rejection = %+v, want stable PREFIX_VIOLATION at service.verdict", failure)
			}
			if verdict, err := st.Verdict(ctx, locked.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected release persisted terminal verdict %+v (err=%v)", verdict, err)
			}
		})
	}
}
