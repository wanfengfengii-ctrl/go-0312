// Package catalog implements the plate-welding process and material rule
// catalog: versioned design and process summaries, base-metal heat and
// thickness rules, groove and pass intervals, consumable and heat-treatment
// programs, inspection mappings and qualification rules. It is the source of
// truth that task locking snapshots from, and it refuses stale summaries.
package catalog

import (
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// RevisionID identifies an immutable catalog revision.
type RevisionID string

// DesignSummary is a versioned welding design summary.
type DesignSummary struct {
	ID      string
	Version int64
	Title   string
	Content string
}

// ProcessSummary is a versioned welding procedure summary (WPS/PQR).
type ProcessSummary struct {
	ID      string
	Version int64
	Content string
}

// CatalogRevision pins a design version, a process summary version and an
// effective logical time. It is the immutable snapshot tasks lock against.
type CatalogRevision struct {
	ID             RevisionID          `json:"id"`
	DesignID       string              `json:"design_id"`
	DesignVersion  int64               `json:"design_version"`
	ProcessID      string              `json:"process_id"`
	ProcessVersion int64               `json:"process_version"`
	EffectiveTime  domain.Milliseconds `json:"effective_time"`
	MaterialRules  []MaterialRule      `json:"material_rules"`
	ThresholdSets  []ThresholdSet      `json:"threshold_sets"`
	DryingPrograms []DryingProgram     `json:"drying_programs"`
	Qualifications []Qualification     `json:"qualifications"`
}

// MaterialRule binds a base-metal heat, a plate thickness and the consumable
// batch identity a component section must match.
type MaterialRule struct {
	HeatNumber string             `json:"heat_number"`
	Thickness  domain.Micrometers `json:"thickness"`
	BatchID    string             `json:"batch_id"`
	BatchSpec  string             `json:"batch_spec"`
}

// BaseMetalHeat identifies a mill heat of base metal.
type BaseMetalHeat struct {
	HeatNumber string
	Grade      string
}

// ConsumableBatch identifies a lot and specification of welding consumable.
type ConsumableBatch struct {
	BatchID string
	Spec    string
}

// DryingProgram prescribes drying and holding temperatures with durations.
type DryingProgram struct {
	ID             string              `json:"id"`
	DryingTemp     domain.Fixed        `json:"drying_temp"`
	HoldingTemp    domain.Fixed        `json:"holding_temp"`
	DryingDuration domain.Milliseconds `json:"drying_duration"`
}

// ThresholdSet holds interval-window, thermal and timing thresholds.
type ThresholdSet struct {
	ID              string              `json:"id"`
	InterpassMin    domain.Fixed        `json:"interpass_min"`
	InterpassMax    domain.Fixed        `json:"interpass_max"`
	PreheatCoverage domain.Fixed        `json:"preheat_coverage"`
	ExposureLimit   domain.Milliseconds `json:"exposure_limit"`
	StopWorkLimit   domain.Milliseconds `json:"stop_work_limit"`
}

// Qualification records a person's role qualification and validity window.
type Qualification struct {
	PersonID  string              `json:"person_id"`
	Role      string              `json:"role"`
	ValidFrom domain.Milliseconds `json:"valid_from"`
	ValidTo   domain.Milliseconds `json:"valid_to"`
}

// Catalog is the read boundary for rule lookups during locking. Implementations
// may reject stale summary references with domain.CodeStaleRevision.
type Catalog interface {
	// LatestRevision returns the current catalog revision.
	LatestRevision() (CatalogRevision, error)
	// Revision returns a specific revision by ID.
	Revision(id RevisionID) (CatalogRevision, error)
	// ValidateSummary ensures a design/process summary is current, returning
	// domain.CodeStaleRevision when a referenced version is superseded.
	ValidateSummary(designID string, designVersion, processVersion int64, now domain.Milliseconds) error
}

// Consumable looks up a consumable batch by identity.
func (r CatalogRevision) Consumable(batchID string) (ConsumableBatch, bool) {
	for _, m := range r.MaterialRules {
		if m.BatchID == batchID {
			return ConsumableBatch{BatchID: m.BatchID, Spec: m.BatchSpec}, true
		}
	}
	return ConsumableBatch{}, false
}
