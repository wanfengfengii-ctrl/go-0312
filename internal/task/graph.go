package task

import (
	"sort"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

// ValidateTopology checks that the pass graph is acyclic and that every
// dependency references a known pass. It returns domain.CodeGraphCycle when a
// cycle exists, or a sorted reason list for missing references.
func (t *NodeTask) ValidateTopology() *domain.DomainError {
	passes := t.Passes()
	index := make(map[string]int, len(passes))
	for i, p := range passes {
		index[p.ID] = i
	}

	adj := make([][]int, len(passes))
	missing := map[string]bool{}
	for i, p := range passes {
		for _, pred := range p.Preds {
			j, ok := index[pred]
			if !ok {
				missing[pred] = true
				continue
			}
			adj[i] = append(adj[i], j)
		}
	}

	if len(missing) > 0 {
		var reasons []string
		for id := range missing {
			reasons = append(reasons, "unknown predecessor "+id)
		}
		sort.Strings(reasons)
		return domain.NewError(domain.CodeGraphCycle, "task.topology", 0, reasons...)
	}

	if cycle := findCycle(adj); len(cycle) > 0 {
		ids := make([]string, len(cycle))
		for i, c := range cycle {
			ids[i] = passes[c].ID
		}
		return domain.NewError(domain.CodeGraphCycle, "task.topology", 0, "cycle "+join(ids, "->"))
	}
	return nil
}

// findCycle returns a cycle path (node indices) or nil if the graph is acyclic.
func findCycle(adj [][]int) []int {
	n := len(adj)
	state := make([]uint8, n) // 0 unvisited, 1 visiting, 2 done
	var stack []int
	var dfs func(int) []int
	dfs = func(u int) []int {
		state[u] = 1
		stack = append(stack, u)
		for _, v := range adj[u] {
			switch state[v] {
			case 1:
				// cycle: reconstruct from stack
				start := 0
				for i, s := range stack {
					if s == v {
						start = i
						break
					}
				}
				return append(append([]int(nil), stack[start:]...), v)
			case 0:
				if c := dfs(v); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[u] = 2
		return nil
	}
	for u := 0; u < n; u++ {
		if state[u] == 0 {
			if c := dfs(u); c != nil {
				return c
			}
		}
	}
	return nil
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
