package service

import (
	"context"
	"encoding/json"

	"truss-thickplate-weld-restraint-release/internal/catalog"
	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/store"
)

// CreateRevisionRequest is the payload for registering a rule-catalog revision.
type CreateRevisionRequest struct {
	ID             string                  `json:"id"`
	DesignID       string                  `json:"design_id"`
	DesignVersion  int64                   `json:"design_version"`
	ProcessID      string                  `json:"process_id"`
	ProcessVersion int64                   `json:"process_version"`
	EffectiveTime  domain.Milliseconds     `json:"effective_time"`
	MaterialRules  []catalog.MaterialRule  `json:"material_rules"`
	ThresholdSets  []catalog.ThresholdSet  `json:"threshold_sets"`
	DryingPrograms []catalog.DryingProgram `json:"drying_programs"`
	Qualifications []catalog.Qualification `json:"qualifications"`
}

// CreateRevision registers a versioned rule-catalog revision. It refuses a
// revision whose effective time precedes the current latest revision, keeping
// the "latest" pointer monotonic so stale summaries cannot be locked later.
func (s *Service) CreateRevision(ctx context.Context, opID string, req CreateRevisionRequest) (catalog.CatalogRevision, error) {
	rev := catalog.CatalogRevision{
		ID:             catalog.RevisionID(req.ID),
		DesignID:       req.DesignID,
		DesignVersion:  req.DesignVersion,
		ProcessID:      req.ProcessID,
		ProcessVersion: req.ProcessVersion,
		EffectiveTime:  req.EffectiveTime,
		MaterialRules:  req.MaterialRules,
		ThresholdSets:  req.ThresholdSets,
		DryingPrograms: req.DryingPrograms,
		Qualifications: req.Qualifications,
	}
	body, err := s.idempotent(ctx, opID, canonicalJSON(req), func(tx store.Store) (any, error) {
		if latest, err := tx.LatestRevision(ctx); err == nil {
			latest, err = s.store.LatestRevision(ctx)
			if err != nil {
				return nil, err
			}
			if rev.EffectiveTime < latest.EffectiveTime {
				return nil, domain.NewError(domain.CodeStaleRevision, "service.catalog", rev.EffectiveTime,
					"revision effective time precedes latest revision")
			}
		}
		if err := tx.SaveRevision(ctx, rev); err != nil {
			return nil, err
		}
		return rev, nil
	})
	if err != nil {
		return catalog.CatalogRevision{}, err
	}
	var out catalog.CatalogRevision
	if err := json.Unmarshal(body, &out); err != nil {
		return catalog.CatalogRevision{}, err
	}
	return out, nil
}

// LatestRevision returns the current effective catalog revision.
func (s *Service) LatestRevision(ctx context.Context) (catalog.CatalogRevision, error) {
	return s.store.LatestRevision(ctx)
}

// Catalog returns the revision with the given ID.
func (s *Service) Catalog(ctx context.Context, id string) (catalog.CatalogRevision, error) {
	return s.store.Revision(ctx, catalog.RevisionID(id))
}
