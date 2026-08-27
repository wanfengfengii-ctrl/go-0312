package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/material"
)

// SavePackage inserts a consumable package.
func (s *SQL) SavePackage(ctx context.Context, p material.ConsumablePackage) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO material_packages(id, batch_id, pkg_json) VALUES(?,?,?)`,
		string(p.ID), p.BatchID, string(data))
	return err
}

// Package loads a consumable package by ID.
func (s *SQL) Package(ctx context.Context, id material.PackageID) (material.ConsumablePackage, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT pkg_json FROM material_packages WHERE id=?`, string(id)).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return material.ConsumablePackage{}, ErrNotFound
	}
	if err != nil {
		return material.ConsumablePackage{}, err
	}
	var p material.ConsumablePackage
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return material.ConsumablePackage{}, err
	}
	return p, nil
}

// AppendLedgerEntry appends one integer-gram movement to a package ledger.
func (s *SQL) AppendLedgerEntry(ctx context.Context, pkg material.PackageID, e material.MaterialLedgerEntry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO material_ledger(package_id, entry_json) VALUES(?,?)`,
		string(pkg), string(data))
	return err
}

// Ledger loads a package's ledger entries ordered by append sequence.
func (s *SQL) Ledger(ctx context.Context, pkg material.PackageID) (material.Ledger, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT entry_json FROM material_ledger WHERE package_id=? ORDER BY seq`, string(pkg))
	if err != nil {
		return material.Ledger{}, err
	}
	defer rows.Close()
	ledger := material.Ledger{PackageID: pkg}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return material.Ledger{}, err
		}
		var e material.MaterialLedgerEntry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return material.Ledger{}, err
		}
		ledger.Entries = append(ledger.Entries, e)
	}
	return ledger, rows.Err()
}

// SaveHoldingGeneration inserts a drying/holding generation.
func (s *SQL) SaveHoldingGeneration(ctx context.Context, g material.HoldingGeneration) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO holding_generations(id, package_id, oven_id, started_at) VALUES(?,?,?,?)`,
		g.ID, string(g.PackageID), g.OvenID, int64(g.StartedAt))
	return err
}

// ContainerOccupancy loads the current occupant of a container, if any.
func (s *SQL) ContainerOccupancy(ctx context.Context, containerID string) (material.ContainerOccupancy, error) {
	var o material.ContainerOccupancy
	err := s.q.QueryRowContext(ctx,
		`SELECT container_id, package_id, batch_id FROM container_occupancy WHERE container_id=?`,
		containerID).Scan(&o.ContainerID, &o.PackageID, &o.BatchID)
	if errors.Is(err, sqlErrNoRows()) {
		return material.ContainerOccupancy{}, ErrNotFound
	}
	if err != nil {
		return material.ContainerOccupancy{}, err
	}
	return o, nil
}

// SetContainerOccupancy assigns a container to a package/batch, enforcing the
// single-occupancy invariant via the primary key.
func (s *SQL) SetContainerOccupancy(ctx context.Context, o material.ContainerOccupancy) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO container_occupancy(container_id, package_id, batch_id) VALUES(?,?,?)`,
		o.ContainerID, string(o.PackageID), o.BatchID)
	return err
}
