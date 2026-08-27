package task

import (
	"sort"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

// Prefix is the current valid construction prefix: the set of completed pass
// IDs, ordered by the public symmetric sequence rule.
type Prefix struct {
	Completed []string
}

// SymmetricOrder returns the public two-sided sequence: A1, B1, A2, B2, ...
// within a single side, sequence numbers may not be skipped.
func SymmetricOrder(passes []WeldPass) ([]string, []string) {
	var a, b []string
	for _, p := range passes {
		if p.Side == SideA {
			a = append(a, p.ID)
		} else {
			b = append(b, p.ID)
		}
	}
	sort.Slice(a, func(i, j int) bool { return seqOf(a[i], passes) < seqOf(a[j], passes) })
	sort.Slice(b, func(i, j int) bool { return seqOf(b[i], passes) < seqOf(b[j], passes) })

	var order []string
	for i := 0; i < len(a) || i < len(b); i++ {
		if i < len(a) {
			order = append(order, a[i])
		}
		if i < len(b) {
			order = append(order, b[i])
		}
	}
	return order, a
}

func seqOf(id string, passes []WeldPass) int64 {
	for _, p := range passes {
		if p.ID == id {
			return p.Sequence
		}
	}
	return 0
}

// AppendPass validates that appending candidate to the current prefix is legal:
// the pass belongs to the current generation, all locked predecessors are
// completed, the symmetric order is respected, and no side may outpace the
// other by more than one or skip its own sequence number. A legal append must
// be the exact next pass in SymmetricOrder.
func (t *NodeTask) AppendPass(completed []string, candidate string) *domain.DomainError {
	order, _ := SymmetricOrder(t.Passes())

	done := make(map[string]bool, len(completed))
	for _, c := range completed {
		done[c] = true
	}

	p, ok := t.PassByID(candidate)
	if !ok {
		return domain.NewError(domain.CodePrefixViolation, "task.prefix", 0, "unknown pass "+candidate)
	}
	if done[candidate] {
		return domain.NewError(domain.CodePrefixViolation, "task.prefix", 0, "pass already completed "+candidate)
	}
	for _, pred := range p.Preds {
		if !done[pred] {
			return domain.NewError(domain.CodePrefixViolation, "task.prefix", 0, "predecessor incomplete "+pred)
		}
	}

	// The next legal pass is the first not-yet-completed pass in symmetric order.
	var next string
	for _, id := range order {
		if !done[id] {
			next = id
			break
		}
	}
	if candidate != next {
		return domain.NewError(domain.CodePrefixViolation, "task.prefix", 0,
			"out-of-sequence pass "+candidate+" (expected "+next+")")
	}
	return nil
}
