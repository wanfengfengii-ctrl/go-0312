package repair

import (
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

func TestComputeClosureSortedDedup(t *testing.T) {
	in := ClosureInput{
		Defect: Defect{PassIDs: []string{"P2"}},
		Passes: map[string]PassRef{
			"P1": {LayerID: "l1", ZoneID: "z1", Heat: "H-100", Holding: "HG-1"},
			"P2": {LayerID: "l1", ZoneID: "z1", Heat: "H-100", Holding: "HG-1"},
			"P3": {LayerID: "l1", ZoneID: "z1", Heat: "H-100", Holding: "HG-1"},
		},
		Adjacency: map[string][]string{
			"P2": {"P1"},
		},
	}
	got := ComputeClosure(in)
	// All three passes share heat H-100 and holding HG-1, so closure includes all.
	if len(got) != 3 {
		t.Fatalf("closure size = %d, want 3: %+v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].PassID >= got[i].PassID {
			t.Fatalf("closure not sorted: %+v", got)
		}
	}
}

func TestValidReviewsRequiresTwoDistinct(t *testing.T) {
	v := TerminalVerdict{TaskID: "t1"}
	err := v.ValidReviews([]Review{
		{PersonID: "alice", Qualified: true},
	})
	if err == nil {
		t.Fatal("expected error for single reviewer")
	}

	err = v.ValidReviews([]Review{
		{PersonID: "alice", Qualified: true},
		{PersonID: "alice", Qualified: true},
	})
	if err == nil {
		t.Fatal("expected error for duplicate reviewer")
	}

	err = v.ValidReviews([]Review{
		{PersonID: "alice", Qualified: true},
		{PersonID: "bob", Qualified: true},
	})
	if err != nil {
		t.Fatalf("expected valid reviews, got %v", err)
	}
}

func TestValidReviewsRejectsUnqualified(t *testing.T) {
	v := TerminalVerdict{TaskID: "t1"}
	err := v.ValidReviews([]Review{
		{PersonID: "alice", Qualified: true},
		{PersonID: "bob", Qualified: false},
	})
	if err == nil {
		t.Fatal("expected error for unqualified reviewer")
	}
}

func TestTerminalConflictSingleWinner(t *testing.T) {
	a := NewTerminalArbiter()
	release := TerminalVerdict{TaskID: "t1", Type: VerdictRelease, Credential: "cred-1"}
	isolate := TerminalVerdict{TaskID: "t1", Type: VerdictIsolate}

	if _, err := a.Decide("t1", release); err != nil {
		t.Fatalf("first decide should succeed, got %v", err)
	}
	got, err := a.Decide("t1", isolate)
	if err == nil || err.Code != domain.CodeTerminalConflict {
		t.Fatalf("expected TERMINAL_CONFLICT, got %v", err)
	}
	if got.Type != VerdictRelease {
		t.Fatalf("existing verdict should be preserved, got %s", got.Type)
	}
	if got.Credential != "cred-1" {
		t.Fatal("credential must not be overwritten")
	}
}
