// Package store implements the transactional persistence boundary shared by
// every business component. It provides an embedded SQL relational database
// (pure-Go SQLite) so that consumable deductions, holding-generation creation,
// lease acquisition, evidence writes, repair closure and terminal verdicts
// commit or roll back atomically, and so the service restores task generations,
// ledgers, leases, device calls, valid pass prefixes and terminal credentials
// after a restart.
package store

import (
	"context"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

// ErrNotFound is returned when a requested row does not exist. Callers map it
// to a stable domain error at the service boundary.
var ErrNotFound = errors.New("store: not found")

// Store is the aggregate persistence boundary. Every method runs within the
// database's transactional guarantees. Callers use WithTx for multi-write
// atomicity across material, lease, evidence, repair and terminal operations.
type Store interface {
	CatalogStore
	TaskStore
	MaterialStore
	LeaseStore
	EvidenceStore
	RepairStore
	VerdictStore

	// WithTx runs fn inside a single transaction. fn receives a Store whose
	// operations participate in the transaction. Any returned error rolls the
	// transaction back.
	WithTx(ctx context.Context, fn func(Store) error) error

	// Close releases the underlying database handle.
	Close() error
}

// CatalogStore persists versioned rule catalog revisions.
type CatalogStore interface {
	SaveRevision(ctx context.Context, rev catalog.CatalogRevision) error
	Revision(ctx context.Context, id catalog.RevisionID) (catalog.CatalogRevision, error)
	LatestRevision(ctx context.Context) (catalog.CatalogRevision, error)
}

// TaskStore persists node tasks and their locked snapshots.
type TaskStore interface {
	SaveTask(ctx context.Context, t task.NodeTask) error
	Task(ctx context.Context, id task.TaskID) (task.NodeTask, error)
	ListTasks(ctx context.Context) ([]task.NodeTask, error)
}

// MaterialStore persists consumable packages, the integer-gram ledger, holding
// generations and container occupancy.
type MaterialStore interface {
	SavePackage(ctx context.Context, p material.ConsumablePackage) error
	Package(ctx context.Context, id material.PackageID) (material.ConsumablePackage, error)
	AppendLedgerEntry(ctx context.Context, pkg material.PackageID, e material.MaterialLedgerEntry) error
	Ledger(ctx context.Context, pkg material.PackageID) (material.Ledger, error)
	SaveHoldingGeneration(ctx context.Context, g material.HoldingGeneration) error
	ContainerOccupancy(ctx context.Context, containerID string) (material.ContainerOccupancy, error)
	SetContainerOccupancy(ctx context.Context, o material.ContainerOccupancy) error
}

// LeaseStore persists resources, leases and scripted device calls.
type LeaseStore interface {
	SaveResource(ctx context.Context, r material.Resource) error
	Lease(ctx context.Context, id string) (material.Lease, error)
	Leases(ctx context.Context, resourceID string) ([]material.Lease, error)
	SaveLease(ctx context.Context, l material.Lease) error
	UpdateLeaseEnd(ctx context.Context, id string, newEnd domain.Milliseconds, version int64) error
	SaveDeviceCall(ctx context.Context, c material.DeviceCall) error
	DeviceCall(ctx context.Context, id string) (material.DeviceCall, error)
}

// EvidenceStore persists append-only evidence events and the derived,
// versioned projections rebuilt and validated within transactions.
type EvidenceStore interface {
	AppendEvidence(ctx context.Context, e thermal.EvidenceEvent) error
	Evidence(ctx context.Context, taskID task.TaskID) ([]thermal.EvidenceEvent, error)
	SavePassPrefix(ctx context.Context, taskID task.TaskID, completed []string, version int64) error
	PassPrefix(ctx context.Context, taskID task.TaskID) (thermal.PassPrefixProjection, error)
	SaveThermalBarrier(ctx context.Context, taskID task.TaskID, established bool, version int64) error
	ThermalBarrier(ctx context.Context, taskID task.TaskID) (thermal.ThermalBarrierProjection, error)
}

// RepairStore persists defects, deterministic repair generations, gouging
// records and retest results.
type RepairStore interface {
	CreateDefect(ctx context.Context, d repair.Defect) error
	Defect(ctx context.Context, id string) (repair.Defect, error)
	CreateRepair(ctx context.Context, r repair.RepairGeneration) error
	CreateGouging(ctx context.Context, g repair.GougingRecord) error
	CreateRetest(ctx context.Context, r repair.RetestResult) error
	Repairs(ctx context.Context, taskID task.TaskID) ([]repair.RepairGeneration, error)
	RepairCount(ctx context.Context, taskID task.TaskID) (int64, error)
	Retests(ctx context.Context, repairID string) ([]repair.RetestResult, error)
}

// VerdictStore persists dual-person reviews, the single non-overridable
// terminal verdict and idempotency records keyed by Operation-Id.
type VerdictStore interface {
	CreateReview(ctx context.Context, r repair.Review) error
	Reviews(ctx context.Context, taskID task.TaskID) ([]repair.Review, error)
	Verdict(ctx context.Context, taskID task.TaskID) (repair.TerminalVerdict, error)
	SaveVerdict(ctx context.Context, v repair.TerminalVerdict) error
	Idempotency(ctx context.Context, operationID string) (IdempotencyRecord, error)
	SaveIdempotency(ctx context.Context, rec IdempotencyRecord) error
}

// IdempotencyRecord stores an operation identifier, a normalized content hash
// and the exact response payload so a lost response can be safely replayed.
type IdempotencyRecord struct {
	OperationID string
	ContentHash string
	Response    []byte
}

// ErrVerdictConflict is returned when a second terminal verdict is written for
// the same task; the caller surfaces it as domain.CodeTerminalConflict.
var ErrVerdictConflict = errors.New("store: terminal verdict already exists")

// isUniqueViolation reports whether err is a SQLite uniqueness constraint
// failure, which the terminal and idempotency barriers rely upon.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite surfaces constraint errors with the SQLITE_CONSTRAINT
	// code; matching on the message text is the stable, driver-agnostic fallback.
	s := err.Error()
	return contains(s, "UNIQUE constraint failed") || contains(s, "constraint failed")
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
