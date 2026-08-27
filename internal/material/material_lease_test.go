package material

import (
	"testing"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

func TestLedgerConservation(t *testing.T) {
	ledger := Ledger{PackageID: "PKG-1", Entries: []MaterialLedgerEntry{
		{PackageID: "PKG-1", Delta: 1000, Disposition: DispositionStock},
		{PackageID: "PKG-1", Delta: -300, Disposition: DispositionIssued},
		{PackageID: "PKG-1", Delta: -100, Disposition: DispositionStub},
	}}
	if got := ledger.Balance(); got != 600 {
		t.Fatalf("balance = %d, want 600", got)
	}
	if err := ledger.CheckConservation(1000); err != nil {
		t.Fatalf("expected conservation, got %v", err)
	}
}

func TestLedgerOverdrawn(t *testing.T) {
	ledger := Ledger{PackageID: "PKG-2", Entries: []MaterialLedgerEntry{
		{PackageID: "PKG-2", Delta: 100, Disposition: DispositionStock},
		{PackageID: "PKG-2", Delta: -300, Disposition: DispositionIssued},
	}}
	err := ledger.CheckConservation(100)
	if err == nil || err.Code != domain.CodeMaterialOverdrawn {
		t.Fatalf("expected MATERIAL_OVERDRAWN, got %v", err)
	}
}

func TestLeaseOverlapConflict(t *testing.T) {
	existing := []Lease{
		{ID: "l1", ResourceID: "W1", Start: 0, End: 100, Version: 1},
	}
	candidate := Lease{ID: "l2", ResourceID: "W1", Start: 50, End: 150}
	err := CheckConflict(existing, candidate)
	if err == nil || err.Code != domain.CodeLeaseConflict {
		t.Fatalf("expected LEASE_CONFLICT, got %v", err)
	}
}

func TestLeaseBoundaryContactAllowed(t *testing.T) {
	existing := []Lease{
		{ID: "l1", ResourceID: "W1", Start: 0, End: 100, Version: 1},
	}
	candidate := Lease{ID: "l2", ResourceID: "W1", Start: 100, End: 200}
	if err := CheckConflict(existing, candidate); err != nil {
		t.Fatalf("boundary contact should be allowed, got %v", err)
	}
}

func TestLeaseExpiresAtEnd(t *testing.T) {
	l := Lease{ID: "l1", ResourceID: "W1", Start: 0, End: 100}
	if l.ExpiredAt(99) {
		t.Fatal("lease should be valid at t=99")
	}
	if !l.ExpiredAt(100) {
		t.Fatal("lease should expire exactly at t=100")
	}
}

func TestDeviceCallRetryCeiling(t *testing.T) {
	c := DeviceCall{ID: "c1", Status: DevicePending, RetrySeq: MaxRetries}
	if c.CanRetry() {
		t.Fatal("call at retry ceiling must not retry")
	}
	c.RetrySeq = MaxRetries - 1
	if !c.CanRetry() {
		t.Fatal("call below ceiling should retry")
	}
}
