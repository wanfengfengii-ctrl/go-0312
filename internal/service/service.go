// Package service orchestrates the documented business flows on top of the
// transactional store: catalog revision management, task creation and locking,
// atomic material operations and leases, evidence and device-call handling,
// defect repair closure and dual-person terminal arbitration. Every write is
// wrapped in Operation-Id idempotency so a lost response can be replayed
// safely and a reused identifier with different content is rejected.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/store"
)

// Service coordinates all business flows against a transactional store.
type Service struct {
	store store.Store
}

// New builds a Service over the given store.
func New(s store.Store) *Service { return &Service{store: s} }

// Store exposes the underlying store for read-only handlers.
func (s *Service) Store() store.Store { return s.store }

// idempotent runs work inside a transaction keyed by Operation-Id and returns
// the JSON response bytes. A safe replay (same content) returns the stored
// response; a reused identifier with different content returns
// IDEMPOTENCY_CONFLICT.
func (s *Service) idempotent(ctx context.Context, opID string, canonical []byte, work func(store.Store) (any, error)) ([]byte, error) {
	if opID == "" {
		return nil, domain.NewError(domain.CodeIdempotencyConflict, "service.idempotency", 0, "missing Operation-Id")
	}
	hash := contentHash(canonical)

	// Fast path: a previously committed operation replays its stored response.
	if rec, err := s.store.Idempotency(ctx, opID); err == nil {
		if rec.ContentHash != hash {
			return nil, domain.NewError(domain.CodeIdempotencyConflict, "service.idempotency", 0,
				"operation id reused with different content")
		}
		return rec.Response, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var respBytes []byte
	err := s.store.WithTx(ctx, func(tx store.Store) error {
		// Re-check inside the transaction so a concurrent identical write on the
		// single writer connection cannot double-commit.
		if rec, err := tx.Idempotency(ctx, opID); err == nil {
			if rec.ContentHash != hash {
				return domain.NewError(domain.CodeIdempotencyConflict, "service.idempotency", 0,
					"operation id reused with different content")
			}
			respBytes = rec.Response
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		r, err := work(tx)
		if err != nil {
			return err
		}
		respBytes, err = json.Marshal(r)
		if err != nil {
			return err
		}
		return tx.SaveIdempotency(ctx, store.IdempotencyRecord{
			OperationID: opID,
			ContentHash: hash,
			Response:    respBytes,
		})
	})
	if err != nil {
		return nil, err
	}
	return respBytes, nil
}

// contentHash returns the hex SHA-256 of the canonical payload bytes.
func contentHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// newID returns a random hex identifier for new aggregates and events.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "id-0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// canonicalJSON is the canonical byte form used for content hashing.
func canonicalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
