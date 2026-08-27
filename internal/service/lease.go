package service

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
	"truss-thickplate-weld-restraint-release/internal/store"
)

// AcquireLeaseRequest is the payload for acquiring a time-bounded exclusive
// lease on a resource.
type AcquireLeaseRequest struct {
	ResourceID string              `json:"resource_id"`
	Operation  string              `json:"operation"`
	Start      domain.Milliseconds `json:"start"`
	End        domain.Milliseconds `json:"end"`
}

// RenewLeaseRequest is the payload for a compare-and-swap lease renewal.
type RenewLeaseRequest struct {
	NewEnd  domain.Milliseconds `json:"new_end"`
	Version int64               `json:"version"`
}

// AcquireLease validates the candidate interval and acquires the lease,
// rejecting any overlapping interval on the same resource with LEASE_CONFLICT.
func (s *Service) AcquireLease(ctx context.Context, opID string, req AcquireLeaseRequest) (material.Lease, error) {
	candidate := material.Lease{
		ID:         newID(),
		ResourceID: req.ResourceID,
		Operation:  req.Operation,
		Start:      req.Start,
		End:        req.End,
		Version:    1,
	}
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		if err := candidate.Validate(); err != nil {
			return nil, err
		}
		existing, err := tx.Leases(ctx, req.ResourceID)
		if err != nil {
			return nil, err
		}
		if err := material.CheckConflict(existing, candidate); err != nil {
			return nil, err
		}
		if err := tx.SaveLease(ctx, candidate); err != nil {
			return nil, err
		}
		return candidate, nil
	})
	if err != nil {
		return material.Lease{}, err
	}
	var out material.Lease
	if err := json.Unmarshal(body, &out); err != nil {
		return material.Lease{}, err
	}
	return out, nil
}

// RenewLease extends a lease's end time with a compare-and-swap on the version.
func (s *Service) RenewLease(ctx context.Context, opID string, leaseID string, req RenewLeaseRequest) (material.Lease, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		existing, err := tx.Lease(ctx, leaseID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, domain.NewError(domain.CodeLeaseConflict, "service.lease.renew", 0, "lease not found")
			}
			return nil, err
		}
		if req.Version != existing.Version {
			return nil, domain.NewError(domain.CodeLeaseConflict, "service.lease.renew", req.NewEnd,
				"lease version mismatch")
		}
		if req.NewEnd <= existing.Start {
			return nil, domain.NewError(domain.CodeLeaseConflict, "service.lease.renew", req.NewEnd,
				"renewal end before lease start")
		}
		if err := tx.UpdateLeaseEnd(ctx, leaseID, req.NewEnd, req.Version); err != nil {
			return nil, err
		}
		existing.End = req.NewEnd
		return existing, nil
	})
	if err != nil {
		return material.Lease{}, err
	}
	var out material.Lease
	if err := json.Unmarshal(body, &out); err != nil {
		return material.Lease{}, err
	}
	return out, nil
}
