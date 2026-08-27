// Package repair implements defect return-to-repair and terminal arbitration:
// deterministic repair-closure sets, new repair generations that isolate late
// receipts from superseded generations, dual-person review, repair-count limits
// and the single-writer terminal competition among release, crack-risk
// isolation and cancellation.
package repair

import (
	"sort"

	"truss-thickplate-weld-restraint-release/internal/domain"
)

// DefectGrade classifies a defect severity.
type DefectGrade string

const (
	GradeSlag     DefectGrade = "SLAG"
	GradePorosity DefectGrade = "POROSITY"
	GradeCrack    DefectGrade = "CRACK"
)

// Defect is a detected defect at integer-micrometer coordinates.
type Defect struct {
	ID         string              `json:"id"`
	TaskID     string              `json:"task_id"`
	Grade      DefectGrade         `json:"grade"`
	Start      domain.Micrometers  `json:"start"`
	End        domain.Micrometers  `json:"end"`
	PassIDs    []string            `json:"pass_ids"`
	DetectedAt domain.Milliseconds `json:"detected_at"`
}

// RepairMember is one deterministic member of a repair closure.
type RepairMember struct {
	PassID   string `json:"pass_id"`
	ZoneID   string `json:"zone_id"`
	Relation string `json:"relation"`
}

// RepairGeneration is a new generation created for a repair; it isolates late
// receipts from the superseded generation.
type RepairGeneration struct {
	ID        string              `json:"id"`
	TaskID    string              `json:"task_id"`
	Number    int64               `json:"number"`
	DefectID  string              `json:"defect_id"`
	Members   []RepairMember      `json:"members"`
	CreatedAt domain.Milliseconds `json:"created_at"`
}

// GougingRecord permanently retains the removed defect volume.
type GougingRecord struct {
	ID       string             `json:"id"`
	DefectID string             `json:"defect_id"`
	RepairID string             `json:"repair_id"`
	Volume   domain.Micrometers `json:"volume"`
}

// RetestResult records retest coverage for a repair generation.
type RetestResult struct {
	ID        string              `json:"id"`
	RepairID  string              `json:"repair_id"`
	ZoneIDs   []string            `json:"zone_ids"`
	Passed    bool                `json:"passed"`
	CreatedAt domain.Milliseconds `json:"created_at"`
}

// Closure computes the deterministic repair closure of directly hit passes,
// expanding same-layer adjacency, shared heat-affected zones, matching base
// heat and matching holding generation. The result is sorted by (zone, layer,
// pass) and de-duplicated.
type ClosureInput struct {
	Defect    Defect
	Passes    map[string]PassRef
	Adjacency map[string][]string // passID -> adjacent pass IDs (HAZ)
}

// PassRef is the minimal pass identity needed for closure expansion.
type PassRef struct {
	LayerID string
	ZoneID  string
	Heat    string
	Holding string
}

// ComputeClosure returns the unique, sorted, de-duplicated closure member set.
func ComputeClosure(in ClosureInput) []RepairMember {
	seen := map[string]RepairMember{}
	var queue []string
	for _, p := range in.Defect.PassIDs {
		queue = append(queue, p)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ref, ok := in.Passes[id]
		if !ok {
			continue
		}
		key := id
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = RepairMember{PassID: id, ZoneID: ref.ZoneID, Relation: "direct"}
		// Same-layer adjacency via HAZ table.
		for _, adj := range in.Adjacency[id] {
			if _, exists := seen[adj]; !exists {
				if ar, ok := in.Passes[adj]; ok {
					seen[adj] = RepairMember{PassID: adj, ZoneID: ar.ZoneID, Relation: "adjacent"}
				}
			}
		}
		// Same heat / holding generation expansion.
		for pid, pr := range in.Passes {
			if _, exists := seen[pid]; exists {
				continue
			}
			if pr.Heat == ref.Heat && pr.Heat != "" {
				queue = append(queue, pid)
			} else if pr.Holding == ref.Holding && pr.Holding != "" {
				queue = append(queue, pid)
			}
		}
	}

	members := make([]RepairMember, 0, len(seen))
	for _, m := range seen {
		members = append(members, m)
	}
	sort.Slice(members, func(a, b int) bool {
		if members[a].ZoneID != members[b].ZoneID {
			return members[a].ZoneID < members[b].ZoneID
		}
		return members[a].PassID < members[b].PassID
	})
	return members
}
