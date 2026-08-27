package service

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/store"
)

// Material operation kinds accepted by POST /api/material/operations.
const (
	OpRegister = "REGISTER"
	OpDry      = "DRY"
	OpHold     = "HOLD"
	OpIssue    = "ISSUE"
	OpReturn   = "RETURN"
	OpWeld     = "WELD"
	OpStub     = "STUB"
	OpLoss     = "LOSS"
)

// MaterialOperationRequest is the payload for an atomic consumable operation.
type MaterialOperationRequest struct {
	Type       string              `json:"type"`
	PackageID  string              `json:"package_id"`
	BatchID    string              `json:"batch_id"`
	Spec       string              `json:"spec"`
	Grams      int64               `json:"grams"`
	OvenID     string              `json:"oven_id"`
	LeaseStart domain.Milliseconds `json:"lease_start"`
	LeaseEnd   domain.Milliseconds `json:"lease_end"`
	Timestamp  domain.Milliseconds `json:"timestamp"`
}

// MaterialOperationResult is the outcome of one consumable operation.
type MaterialOperationResult struct {
	PackageID   string                      `json:"package_id"`
	Balance     domain.Grams                `json:"balance"`
	Disposition material.Disposition        `json:"disposition,omitempty"`
	Holding     *material.HoldingGeneration `json:"holding,omitempty"`
	Lease       *material.Lease             `json:"lease,omitempty"`
}

// MaterialOperation executes a drying, holding, issuing, returning, welding,
// stub or loss operation atomically. Issuing, drying and holding additionally
// create a holding generation and acquire an oven lease in the same transaction
// so any conflict rolls back every write.
func (s *Service) MaterialOperation(ctx context.Context, opID string, req MaterialOperationRequest) (MaterialOperationResult, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		return s.applyMaterialOp(ctx, tx, req)
	})
	if err != nil {
		return MaterialOperationResult{}, err
	}
	var out MaterialOperationResult
	if err := json.Unmarshal(body, &out); err != nil {
		return MaterialOperationResult{}, err
	}
	return out, nil
}

func (s *Service) applyMaterialOp(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	if req.Grams < 0 {
		return MaterialOperationResult{}, domain.NewError(domain.CodeMaterialOverdrawn, "service.material", req.Timestamp, "negative grams")
	}
	switch req.Type {
	case OpRegister:
		return s.opRegister(ctx, tx, req)
	case OpDry, OpHold:
		return s.opDryHold(ctx, tx, req)
	case OpIssue:
		return s.opIssue(ctx, tx, req)
	case OpReturn:
		return s.opReturn(ctx, tx, req)
	case OpWeld, OpStub, OpLoss:
		return s.opOutflow(ctx, tx, req)
	default:
		return MaterialOperationResult{}, domain.NewError(domain.CodeMaterialOverdrawn, "service.material", req.Timestamp,
			"unknown operation type "+req.Type)
	}
}

// opRegister registers a uniquely tagged package with its initial integer-gram
// mass and a stock ledger entry.
func (s *Service) opRegister(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	if req.Grams == 0 {
		return MaterialOperationResult{}, domain.NewError(domain.CodeMaterialOverdrawn, "service.material", req.Timestamp, "initial mass must be positive")
	}
	pkg := material.ConsumablePackage{ID: material.PackageID(req.PackageID), BatchID: req.BatchID, Spec: req.Spec, Mass: domain.Grams(req.Grams)}
	if err := tx.SavePackage(ctx, pkg); err != nil {
		return MaterialOperationResult{}, err
	}
	entry := material.MaterialLedgerEntry{
		ID:          newID(),
		PackageID:   pkg.ID,
		Delta:       domain.Grams(req.Grams),
		Disposition: material.DispositionStock,
		OperationID: "",
	}
	if err := tx.AppendLedgerEntry(ctx, pkg.ID, entry); err != nil {
		return MaterialOperationResult{}, err
	}
	return MaterialOperationResult{PackageID: req.PackageID, Balance: domain.Grams(req.Grams), Disposition: material.DispositionStock}, nil
}

// opDryHold creates a holding generation, occupies an oven container and
// optionally acquires an oven lease, atomically.
func (s *Service) opDryHold(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	holding := material.HoldingGeneration{ID: newID(), PackageID: material.PackageID(req.PackageID), OvenID: req.OvenID, StartedAt: req.Timestamp}
	if err := tx.SaveHoldingGeneration(ctx, holding); err != nil {
		return MaterialOperationResult{}, err
	}
	if err := s.occupyContainer(ctx, tx, req); err != nil {
		return MaterialOperationResult{}, err
	}
	lease, err := s.acquireOvenLease(ctx, tx, req)
	if err != nil {
		return MaterialOperationResult{}, err
	}
	return MaterialOperationResult{PackageID: req.PackageID, Holding: &holding, Lease: lease}, nil
}

// opIssue deducts available mass, creates a holding generation, occupies the
// oven container and acquires an oven lease in one transaction.
func (s *Service) opIssue(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	if err := s.deduct(ctx, tx, req, material.DispositionIssued, -domain.Grams(req.Grams)); err != nil {
		return MaterialOperationResult{}, err
	}
	holding := material.HoldingGeneration{ID: newID(), PackageID: material.PackageID(req.PackageID), OvenID: req.OvenID, StartedAt: req.Timestamp}
	if err := tx.SaveHoldingGeneration(ctx, holding); err != nil {
		return MaterialOperationResult{}, err
	}
	if err := s.occupyContainer(ctx, tx, req); err != nil {
		return MaterialOperationResult{}, err
	}
	lease, err := s.acquireOvenLease(ctx, tx, req)
	if err != nil {
		return MaterialOperationResult{}, err
	}
	balance, err := s.balance(ctx, tx, req.PackageID)
	if err != nil {
		return MaterialOperationResult{}, err
	}
	return MaterialOperationResult{PackageID: req.PackageID, Balance: balance, Disposition: material.DispositionIssued, Holding: &holding, Lease: lease}, nil
}

// opReturn credits returned material back to the package balance.
func (s *Service) opReturn(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	if err := s.credit(ctx, tx, req, material.DispositionReturned, domain.Grams(req.Grams)); err != nil {
		return MaterialOperationResult{}, err
	}
	balance, _ := s.balance(ctx, tx, req.PackageID)
	return MaterialOperationResult{PackageID: req.PackageID, Balance: balance, Disposition: material.DispositionReturned}, nil
}

// opOutflow handles WELD, STUB and LOSS as balance-reducing dispositions.
func (s *Service) opOutflow(ctx context.Context, tx store.Store, req MaterialOperationRequest) (MaterialOperationResult, error) {
	var disp material.Disposition
	switch req.Type {
	case OpWeld:
		disp = material.DispositionWelded
	case OpStub:
		disp = material.DispositionStub
	case OpLoss:
		disp = material.DispositionLoss
	}
	if err := s.deduct(ctx, tx, req, disp, -domain.Grams(req.Grams)); err != nil {
		return MaterialOperationResult{}, err
	}
	balance, _ := s.balance(ctx, tx, req.PackageID)
	return MaterialOperationResult{PackageID: req.PackageID, Balance: balance, Disposition: disp}, nil
}

// deduct reduces the package balance by a negative delta after checking the
// available mass and the conservation invariant.
func (s *Service) deduct(ctx context.Context, tx store.Store, req MaterialOperationRequest, disp material.Disposition, delta domain.Grams) error {
	ledger, err := tx.Ledger(ctx, material.PackageID(req.PackageID))
	if err != nil {
		return err
	}
	if ledger.Balance() < domain.Grams(req.Grams) {
		return domain.NewError(domain.CodeMaterialOverdrawn, "service.material", req.Timestamp,
			"insufficient available mass")
	}
	entry := material.MaterialLedgerEntry{ID: newID(), PackageID: material.PackageID(req.PackageID), Delta: delta, Disposition: disp}
	return tx.AppendLedgerEntry(ctx, entry.PackageID, entry)
}

// credit adds a positive delta for returned material.
func (s *Service) credit(ctx context.Context, tx store.Store, req MaterialOperationRequest, disp material.Disposition, delta domain.Grams) error {
	entry := material.MaterialLedgerEntry{ID: newID(), PackageID: material.PackageID(req.PackageID), Delta: delta, Disposition: disp}
	return tx.AppendLedgerEntry(ctx, entry.PackageID, entry)
}

// occupyContainer enforces the single-occupancy, anti-cross-batch rule.
func (s *Service) occupyContainer(ctx context.Context, tx store.Store, req MaterialOperationRequest) error {
	if req.OvenID == "" {
		return nil
	}
	pkg, err := tx.Package(ctx, material.PackageID(req.PackageID))
	if err != nil {
		return err
	}
	if occ, err := tx.ContainerOccupancy(ctx, req.OvenID); err == nil {
		if occ.PackageID != material.PackageID(req.PackageID) || occ.BatchID != pkg.BatchID {
			return domain.NewError(domain.CodeContainerContam, "service.material", req.Timestamp,
				"container occupied by a different package or batch")
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return tx.SetContainerOccupancy(ctx, material.ContainerOccupancy{ContainerID: req.OvenID, PackageID: material.PackageID(req.PackageID), BatchID: pkg.BatchID})
}

// acquireOvenLease acquires an oven lease when a lease interval is supplied.
func (s *Service) acquireOvenLease(ctx context.Context, tx store.Store, req MaterialOperationRequest) (*material.Lease, error) {
	if req.OvenID == "" || req.LeaseStart >= req.LeaseEnd {
		return nil, nil
	}
	candidate := material.Lease{
		ID:         newID(),
		ResourceID: req.OvenID,
		Operation:  req.Type,
		Start:      req.LeaseStart,
		End:        req.LeaseEnd,
		Version:    1,
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	existing, err := tx.Leases(ctx, req.OvenID)
	if err != nil {
		return nil, err
	}
	if err := material.CheckConflict(existing, candidate); err != nil {
		return nil, err
	}
	if err := tx.SaveLease(ctx, candidate); err != nil {
		return nil, err
	}
	return &candidate, nil
}

// balance returns the current package balance (available on-hand mass).
func (s *Service) balance(ctx context.Context, tx store.Store, packageID string) (domain.Grams, error) {
	ledger, err := tx.Ledger(ctx, material.PackageID(packageID))
	if err != nil {
		return 0, err
	}
	return ledger.Balance(), nil
}
