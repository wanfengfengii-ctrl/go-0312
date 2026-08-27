package task

import (
	"testing"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
)

func baseLockRequest() LockRequest {
	return LockRequest{
		TaskID:           "task-1",
		DesignID:         "d1",
		DesignVersion:    2,
		ProcessID:        "p1",
		ProcessVersion:   2,
		SectionHeat:      "H-100",
		SectionThickness: 30000000, // 30 mm
		Revision: catalog.CatalogRevision{
			DesignID:       "d1",
			DesignVersion:  2,
			ProcessID:      "p1",
			ProcessVersion: 2,
			MaterialRules: []catalog.MaterialRule{
				{HeatNumber: "H-100", Thickness: 30000000, BatchID: "B-1"},
			},
		},
		DesignEnd: 1000000,
		GrooveZones: []GrooveZone{
			{ID: "z1", Side: SideA, Interval: domain.Interval{Start: 0, End: 1000000}},
			{ID: "z2", Side: SideB, Interval: domain.Interval{Start: 0, End: 1000000}},
		},
		Passes: []WeldPass{
			{ID: "A1", Side: SideA, Sequence: 1, LayerID: "l1", Interval: domain.Interval{Start: 0, End: 1000000}},
			{ID: "B1", Side: SideB, Sequence: 1, LayerID: "l1", Interval: domain.Interval{Start: 0, End: 1000000}},
		},
	}
}

func TestValidateLockValid(t *testing.T) {
	if err := ValidateLock(baseLockRequest(), 0); err != nil {
		t.Fatalf("expected valid lock, got %v", err)
	}
}

func TestValidateLockStaleRevision(t *testing.T) {
	req := baseLockRequest()
	req.DesignVersion = 1 // superseded by revision 2
	err := ValidateLock(req, 0)
	if err == nil || err.Code != domain.CodeStaleRevision {
		t.Fatalf("expected STALE_REVISION, got %v", err)
	}
}

func TestValidateLockHeatMismatch(t *testing.T) {
	req := baseLockRequest()
	req.SectionHeat = "H-999"
	err := ValidateLock(req, 0)
	if err == nil || err.Code != domain.CodeHeatMismatch {
		t.Fatalf("expected HEAT_MISMATCH, got %v", err)
	}
}

func TestValidateLockIntervalGap(t *testing.T) {
	req := baseLockRequest()
	req.GrooveZones = []GrooveZone{
		{ID: "z1", Side: SideA, Interval: domain.Interval{Start: 0, End: 500000}},
		{ID: "z2", Side: SideB, Interval: domain.Interval{Start: 700000, End: 1000000}},
	}
	err := ValidateLock(req, 0)
	if err == nil || err.Code != domain.CodeIntervalGap {
		t.Fatalf("expected INTERVAL_GAP, got %v", err)
	}
}

func TestValidateLockIntervalOverlap(t *testing.T) {
	req := baseLockRequest()
	req.GrooveZones = []GrooveZone{
		{ID: "z1", Side: SideA, Interval: domain.Interval{Start: 0, End: 600000}},
		{ID: "z2", Side: SideA, Interval: domain.Interval{Start: 500000, End: 1000000}},
	}
	err := ValidateLock(req, 0)
	if err == nil || err.Code != domain.CodeIntervalOverlap {
		t.Fatalf("expected INTERVAL_OVERLAP, got %v", err)
	}
}

func TestValidateLockNegativeInterval(t *testing.T) {
	req := baseLockRequest()
	req.GrooveZones = []GrooveZone{
		{ID: "z1", Side: SideA, Interval: domain.Interval{Start: -5, End: 1000000}},
	}
	err := ValidateLock(req, 0)
	if err == nil {
		t.Fatal("expected error for negative interval, got nil")
	}
}

func TestValidateLockGraphCycle(t *testing.T) {
	req := baseLockRequest()
	req.Passes = []WeldPass{
		{ID: "A1", Side: SideA, Sequence: 1, LayerID: "l1", Interval: domain.Interval{Start: 0, End: 500000}, Preds: []string{"A2"}},
		{ID: "A2", Side: SideA, Sequence: 2, LayerID: "l1", Interval: domain.Interval{Start: 500000, End: 1000000}, Preds: []string{"A1"}},
	}
	err := ValidateLock(req, 0)
	if err == nil || err.Code != domain.CodeGraphCycle {
		t.Fatalf("expected GRAPH_CYCLE, got %v", err)
	}
}

func TestSymmetricOrderAlternates(t *testing.T) {
	req := baseLockRequest()
	order, _ := SymmetricOrder(req.Passes)
	if len(order) != 2 || order[0] != "A1" || order[1] != "B1" {
		t.Fatalf("unexpected symmetric order: %v", order)
	}
}
