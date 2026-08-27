package service

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/store"
	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

// CreateReviewRequest is the payload for an independent person review.
type CreateReviewRequest struct {
	PersonID     string              `json:"person_id"`
	Role         string              `json:"role"`
	EvidenceHash string              `json:"evidence_hash"`
	CreatedAt    domain.Milliseconds `json:"created_at"`
}

// CreateVerdictRequest is the payload for the terminal arbitration.
type CreateVerdictRequest struct {
	Type      string              `json:"type"`
	CreatedAt domain.Milliseconds `json:"created_at"`
}

// CreateReview validates the reviewer's qualification against the task's
// catalog revision and records the review.
func (s *Service) CreateReview(ctx context.Context, opID string, taskID task.TaskID, req CreateReviewRequest) (repair.Review, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		t, err := tx.Task(ctx, taskID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.review")
		}
		qualified := false
		if rev, err := tx.Revision(ctx, catalog.RevisionID(t.CatalogRevision)); err == nil {
			for _, q := range rev.Qualifications {
				if q.PersonID == req.PersonID && (q.Role == "" || q.Role == req.Role) &&
					req.CreatedAt >= q.ValidFrom && req.CreatedAt <= q.ValidTo {
					qualified = true
					break
				}
			}
		}
		r := repair.Review{
			ID:           newID(),
			TaskID:       string(taskID),
			PersonID:     req.PersonID,
			Role:         req.Role,
			Qualified:    qualified,
			EvidenceHash: req.EvidenceHash,
			CreatedAt:    req.CreatedAt,
		}
		if err := tx.CreateReview(ctx, r); err != nil {
			return nil, err
		}
		return r, nil
	})
	if err != nil {
		return repair.Review{}, err
	}
	var out repair.Review
	if err := json.Unmarshal(body, &out); err != nil {
		return repair.Review{}, err
	}
	return out, nil
}

// CreateVerdict performs the single-writer terminal competition. Release
// additionally requires a closed pass prefix, an established thermal barrier
// and complete retest coverage; any competing outcome returns TERMINAL_CONFLICT
// and never overwrites the stored credential.
func (s *Service) CreateVerdict(ctx context.Context, opID string, taskID task.TaskID, req CreateVerdictRequest) (repair.TerminalVerdict, error) {
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		t, err := tx.Task(ctx, taskID)
		if err != nil {
			return nil, ensureNotFound(err, domain.CodePrefixViolation, "service.verdict")
		}

		vtype := repair.VerdictType(req.Type)
		if vtype != repair.VerdictRelease && vtype != repair.VerdictIsolate && vtype != repair.VerdictCancel {
			return nil, domain.NewError(domain.CodeTerminalConflict, "service.verdict", req.CreatedAt,
				"unknown verdict type "+req.Type)
		}

		if existing, err := tx.Verdict(ctx, taskID); err == nil {
			if existing.Type != vtype {
				return nil, domain.NewError(domain.CodeTerminalConflict, "service.verdict", req.CreatedAt,
					"existing verdict "+string(existing.Type))
			}
			return existing, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}

		reviews, err := tx.Reviews(ctx, taskID)
		if err != nil {
			return nil, err
		}
		verdict := repair.TerminalVerdict{TaskID: string(taskID), Type: vtype, Version: 1}
		if err := verdict.ValidReviews(reviews); err != nil {
			err.Path = "service.verdict"
			return nil, err
		}

		if vtype == repair.VerdictRelease {
			if err := s.checkReleaseReady(ctx, tx, t); err != nil {
				return nil, err
			}
			verdict.Credential = newID()
		}
		if err := tx.SaveVerdict(ctx, verdict); err != nil {
			if errors.Is(err, store.ErrVerdictConflict) {
				existing, _ := tx.Verdict(ctx, taskID)
				return nil, domain.NewError(domain.CodeTerminalConflict, "service.verdict", req.CreatedAt,
					"existing verdict "+string(existing.Type))
			}
			return nil, err
		}
		return verdict, nil
	})
	if err != nil {
		return repair.TerminalVerdict{}, err
	}
	var out repair.TerminalVerdict
	if err := json.Unmarshal(body, &out); err != nil {
		return repair.TerminalVerdict{}, err
	}
	return out, nil
}

// checkReleaseReady enforces the release preconditions beyond dual review:
// every pass completed, the thermal barrier established and every repair fully
// retested.
func (s *Service) checkReleaseReady(ctx context.Context, tx store.Store, t task.NodeTask) error {
	prefix, err := tx.PassPrefix(ctx, t.ID)
	if err != nil || len(prefix.Completed) != len(t.Passes()) {
		return domain.NewError(domain.CodePrefixViolation, "service.verdict", 0,
			"pass prefix not closed")
	}
	barrier, err := tx.ThermalBarrier(ctx, t.ID)
	if err != nil || !barrier.Established {
		return domain.NewError(domain.CodeThermalOutOfRange, "service.verdict", 0,
			"thermal barrier not established")
	}
	events, err := tx.Evidence(ctx, t.ID)
	if err == nil {
		if !hasKind(events, thermal.KindVisual) || !hasKind(events, thermal.KindUltrasonic) {
			return domain.NewError(domain.CodePrefixViolation, "service.verdict", 0,
				"visual or ultrasonic inspection missing")
		}
	}
	repairs, err := tx.Repairs(ctx, t.ID)
	if err != nil {
		return err
	}
	for _, r := range repairs {
		retests, err := tx.Retests(ctx, r.ID)
		if err != nil {
			return err
		}
		passed := false
		for _, rt := range retests {
			if rt.Passed {
				passed = true
			}
		}
		if !passed {
			return domain.NewError(domain.CodePrefixViolation, "service.verdict", 0,
				"repair not fully retested")
		}
	}
	return nil
}

func hasKind(events []thermal.EvidenceEvent, kind thermal.EvidenceKind) bool {
	for _, e := range events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}
