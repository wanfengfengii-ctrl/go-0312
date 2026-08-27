// Package task models the node welding task and the append-only
// node—weld—layer—pass evidence graph, including direction, predecessor
// relations, acyclic topology validation and the two-sided symmetric prefix
// rule.
package task

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// TaskID identifies a node welding task.
type TaskID string

// Generation is the monotonic immutable task generation number.
type Generation int64

// Side is a groove side of a two-sided (double-V) joint.
type Side string

const (
	SideA Side = "A"
	SideB Side = "B"
)

// NodeTask is the root aggregate: an engineering zone, a component section and
// a node with a locked snapshot and a generation.
type NodeTask struct {
	ID              TaskID     `json:"id"`
	Zone            string     `json:"zone"`
	Component       string     `json:"component"`
	Node            string     `json:"node"`
	Status          Status     `json:"status"`
	Generation      Generation `json:"generation"`
	CatalogRevision string     `json:"catalog_revision,omitempty"`
	Weld            Weld       `json:"weld"`
}

// Status is the lifecycle state of a task.
type Status string

const (
	StatusDraft   Status = "DRAFT"
	StatusLocked  Status = "LOCKED"
	StatusWelding Status = "WELDING"
	StatusRepair  Status = "REPAIR"
	StatusClosed  Status = "CLOSED"
)

// Weld is the design weld being built.
type Weld struct {
	ID          string                  `json:"id"`
	DesignStart domain.Micrometers      `json:"design_start"`
	DesignEnd   domain.Micrometers      `json:"design_end"`
	GrooveZones []GrooveZone            `json:"groove_zones"`
	Layers      []WeldLayer             `json:"layers"`
	Adjacencies []HeatAffectedAdjacency `json:"adjacencies,omitempty"`
}

// GrooveZone is a partition of the groove with a side and a continuous interval.
type GrooveZone struct {
	ID       string          `json:"id"`
	Side     Side            `json:"side"`
	Interval domain.Interval `json:"interval"`
}

// WeldLayer is a layer of weld metal.
type WeldLayer struct {
	ID       string     `json:"id"`
	Sequence int64      `json:"sequence"`
	Passes   []WeldPass `json:"passes"`
}

// WeldPass is a single pass; Preds are pass IDs that must complete first.
// ZoneID, Heat and Holding carry the groove-zone, base-metal heat and consumable
// holding generation used by the deterministic repair-closure rule.
type WeldPass struct {
	ID       string          `json:"id"`
	Side     Side            `json:"side"`
	Sequence int64           `json:"sequence"`
	LayerID  string          `json:"layer_id"`
	ZoneID   string          `json:"zone_id,omitempty"`
	Heat     string          `json:"heat,omitempty"`
	Holding  string          `json:"holding,omitempty"`
	Interval domain.Interval `json:"interval"`
	Preds    []string        `json:"preds,omitempty"`
}

// PassDependency is a directed predecessor edge between passes.
type PassDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// HeatAffectedAdjacency records a shared heat-affected zone between two passes.
type HeatAffectedAdjacency struct {
	PassA string `json:"pass_a"`
	PassB string `json:"pass_b"`
}

// Passes returns the flattened list of passes in the task graph.
func (t *NodeTask) Passes() []WeldPass {
	var out []WeldPass
	for _, l := range t.Weld.Layers {
		out = append(out, l.Passes...)
	}
	return out
}

// PassByID returns a pass by ID and whether it exists.
func (t *NodeTask) PassByID(id string) (WeldPass, bool) {
	for _, p := range t.Passes() {
		if p.ID == id {
			return p, true
		}
	}
	return WeldPass{}, false
}
