package repair

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// Review is an independent review by a qualified person.
type Review struct {
	ID           string              `json:"id"`
	TaskID       string              `json:"task_id"`
	PersonID     string              `json:"person_id"`
	Role         string              `json:"role"`
	Qualified    bool                `json:"qualified"`
	EvidenceHash string              `json:"evidence_hash"`
	CreatedAt    domain.Milliseconds `json:"created_at"`
}

// VerdictType is the single, non-overridable terminal outcome.
type VerdictType string

const (
	VerdictRelease VerdictType = "RELEASE"
	VerdictIsolate VerdictType = "CRACK_RISK_ISOLATION"
	VerdictCancel  VerdictType = "CANCEL"
)

// TerminalVerdict is the unique terminal outcome plus its release credential.
type TerminalVerdict struct {
	TaskID     string      `json:"task_id"`
	Type       VerdictType `json:"type"`
	Credential string      `json:"credential,omitempty"`
	Version    int64       `json:"version"`
}

// ReviewSet validates the dual-person review rule: exactly two distinct,
// qualified people must review before a verdict may be reached.
func (v TerminalVerdict) ValidReviews(reviews []Review) *domain.DomainError {
	people := map[string]bool{}
	qualified := 0
	for _, r := range reviews {
		if !r.Qualified {
			return domain.NewError(domain.CodePrefixViolation, "repair.review", r.CreatedAt, "reviewer not qualified "+r.PersonID)
		}
		if people[r.PersonID] {
			return domain.NewError(domain.CodePrefixViolation, "repair.review", r.CreatedAt, "duplicate reviewer "+r.PersonID)
		}
		people[r.PersonID] = true
		qualified++
	}
	if qualified < 2 {
		return domain.NewError(domain.CodePrefixViolation, "repair.review", 0, "insufficient distinct qualified reviewers")
	}
	return nil
}

// TerminalArbiter decides the single terminal outcome among concurrent
// candidates. The first candidate wins; every subsequent candidate conflicts
// with domain.CodeTerminalConflict and must not overwrite the credential.
type TerminalArbiter struct {
	verdicts map[string]TerminalVerdict
}

func NewTerminalArbiter() *TerminalArbiter {
	return &TerminalArbiter{verdicts: map[string]TerminalVerdict{}}
}

// Decide records the terminal outcome for a task. It returns the winning
// verdict and a domain error (CodeTerminalConflict) for any competing outcome.
func (a *TerminalArbiter) Decide(taskID string, candidate TerminalVerdict) (TerminalVerdict, *domain.DomainError) {
	if existing, ok := a.verdicts[taskID]; ok {
		if existing.Type != candidate.Type {
			return existing, domain.NewError(domain.CodeTerminalConflict, "repair.verdict", 0,
				"existing verdict "+string(existing.Type))
		}
		return existing, nil
	}
	a.verdicts[taskID] = candidate
	return candidate, nil
}

// Result returns the recorded terminal verdict for a task, if any.
func (a *TerminalArbiter) Result(taskID string) (TerminalVerdict, bool) {
	v, ok := a.verdicts[taskID]
	return v, ok
}
