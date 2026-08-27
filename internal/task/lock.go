package task

import (
	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
)

// LockRequest is the complete immutable snapshot a lock operation validates and
// fixes into a task generation. It is the single failure boundary for stale
// summaries, heat mismatch, illegal intervals and cyclic topology: any of these
// rolls back the whole lock, leaving no half-locked generation.
type LockRequest struct {
	TaskID           TaskID
	DesignID         string
	DesignVersion    int64
	ProcessID        string
	ProcessVersion   int64
	SectionHeat      string
	SectionThickness domain.Micrometers
	Revision         catalog.CatalogRevision
	DesignEnd        domain.Micrometers
	GrooveZones      []GrooveZone
	Passes           []WeldPass
}

// ValidateLock runs every lock-time invariant and returns the first stable
// failure, or nil when the snapshot is valid and fixable.
func ValidateLock(req LockRequest, now domain.Milliseconds) *domain.DomainError {
	if req.DesignVersion < req.Revision.DesignVersion || req.ProcessVersion < req.Revision.ProcessVersion {
		return domain.NewError(domain.CodeStaleRevision, "task.lock", now,
			"design or process summary superseded")
	}

	if !heatMatches(req.Revision.MaterialRules, req.SectionHeat, req.SectionThickness) {
		return domain.NewError(domain.CodeHeatMismatch, "task.lock", now,
			"section heat "+req.SectionHeat+" does not match any material rule")
	}

	// Groove zones on each side must independently cover the design weld
	// interval [0, DesignEnd) without illegal overlap.
	for _, side := range []Side{SideA, SideB} {
		intervals := make([]domain.Interval, 0, len(req.GrooveZones))
		for _, z := range req.GrooveZones {
			if z.Side == side {
				intervals = append(intervals, z.Interval)
			}
		}
		if len(intervals) == 0 {
			continue
		}
		if _, err := domain.ValidateCoverage(0, req.DesignEnd, intervals); err != nil {
			err.Path = "task.lock"
			err.LogicalTime = now
			return err
		}
	}

	probe := &NodeTask{Weld: Weld{Layers: []WeldLayer{{Passes: req.Passes}}}}
	if err := probe.ValidateTopology(); err != nil {
		err.LogicalTime = now
		return err
	}
	return nil
}

func heatMatches(rules []catalog.MaterialRule, heat string, thickness domain.Micrometers) bool {
	for _, r := range rules {
		if r.HeatNumber == heat && r.Thickness == thickness {
			return true
		}
	}
	return false
}
