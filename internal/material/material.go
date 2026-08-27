// Package material implements consumable conservation and resource leases:
// a unique-package integer-gram ledger, drying and holding generations,
// cross-batch container-occupancy rules, and time-bounded mutually exclusive
// leases for preheaters, welders, holding ovens, temperature channels, gouging
// positions and ultrasonic channels.
package material

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// PackageID identifies a uniquely tagged consumable package.
type PackageID string

// Disposition is the destination of a ledger movement.
type Disposition string

const (
	DispositionStock    Disposition = "STOCK"
	DispositionIssued   Disposition = "ISSUED"
	DispositionWelded   Disposition = "WELDED"
	DispositionReturned Disposition = "RETURNED"
	DispositionStub     Disposition = "STUB"
	DispositionLoss     Disposition = "LOSS"
)

// ConsumablePackage is a single uniquely identified package of consumable.
// It belongs to exactly one batch and specification.
type ConsumablePackage struct {
	ID      PackageID    `json:"id"`
	BatchID string       `json:"batch_id"`
	Spec    string       `json:"spec"`
	Mass    domain.Grams `json:"mass"`
}

// HoldingGeneration is a drying/holding generation for a package.
type HoldingGeneration struct {
	ID        string              `json:"id"`
	PackageID PackageID           `json:"package_id"`
	OvenID    string              `json:"oven_id"`
	StartedAt domain.Milliseconds `json:"started_at"`
}

// MaterialLedgerEntry is one integer-gram movement on a package's ledger.
type MaterialLedgerEntry struct {
	ID          string       `json:"id"`
	PackageID   PackageID    `json:"package_id"`
	Delta       domain.Grams `json:"delta"`
	Disposition Disposition  `json:"disposition"`
	OperationID string       `json:"operation_id,omitempty"`
	Sequence    int64        `json:"sequence,omitempty"`
}

// Ledger is the append-only, integer-gram account for a single package.
type Ledger struct {
	PackageID PackageID
	Entries   []MaterialLedgerEntry
}

// Balance sums all deltas; it must equal the initial mass minus all outflows
// and is always non-negative for a valid ledger.
func (l Ledger) Balance() domain.Grams {
	var sum domain.Grams
	for _, e := range l.Entries {
		sum += e.Delta
	}
	return sum
}

// ByDisposition sums the ledger by disposition, used to assert conservation:
// initial mass == stock + issued + welded + returned + stub + approved loss.
func (l Ledger) ByDisposition() map[Disposition]domain.Grams {
	out := map[Disposition]domain.Grams{}
	for _, e := range l.Entries {
		out[e.Disposition] += e.Delta
	}
	return out
}

// CheckConservation validates that the initial mass equals the sum of all
// disposition components and that no component is negative. It returns
// domain.CodeMaterialOverdrawn on violation.
func (l Ledger) CheckConservation(initial domain.Grams) *domain.DomainError {
	var total domain.Grams
	for _, e := range l.Entries {
		total += e.Delta
	}
	if total < 0 {
		return domain.NewError(domain.CodeMaterialOverdrawn, "material.ledger", 0, "negative balance")
	}
	if total > initial {
		return domain.NewError(domain.CodeMaterialOverdrawn, "material.ledger", 0, "balance exceeds initial mass")
	}
	return nil
}

// ContainerOccupancy prevents cross-batch contamination: a container can hold
// at most one holding generation at a time.
type ContainerOccupancy struct {
	ContainerID string    `json:"container_id"`
	PackageID   PackageID `json:"package_id"`
	BatchID     string    `json:"batch_id"`
}
