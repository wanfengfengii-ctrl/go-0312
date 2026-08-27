package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/catalog"
)

// SaveRevision inserts or updates a catalog revision keyed by ID.
func (s *SQL) SaveRevision(ctx context.Context, rev catalog.CatalogRevision) error {
	data, err := json.Marshal(rev)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO catalog_revisions(id, effective_time, revision_json) VALUES(?,?,?)
		 ON CONFLICT(id) DO UPDATE SET effective_time=excluded.effective_time, revision_json=excluded.revision_json`,
		string(rev.ID), int64(rev.EffectiveTime), string(data))
	return err
}

// Revision loads a catalog revision by ID.
func (s *SQL) Revision(ctx context.Context, id catalog.RevisionID) (catalog.CatalogRevision, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT revision_json FROM catalog_revisions WHERE id=?`, string(id)).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return catalog.CatalogRevision{}, ErrNotFound
	}
	if err != nil {
		return catalog.CatalogRevision{}, err
	}
	return decodeRevision(raw)
}

// LatestRevision returns the revision with the greatest effective time.
func (s *SQL) LatestRevision(ctx context.Context) (catalog.CatalogRevision, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT revision_json FROM catalog_revisions ORDER BY effective_time DESC, id DESC LIMIT 1`).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return catalog.CatalogRevision{}, ErrNotFound
	}
	if err != nil {
		return catalog.CatalogRevision{}, err
	}
	return decodeRevision(raw)
}

func decodeRevision(raw string) (catalog.CatalogRevision, error) {
	var rev catalog.CatalogRevision
	if err := json.Unmarshal([]byte(raw), &rev); err != nil {
		return catalog.CatalogRevision{}, err
	}
	return rev, nil
}
