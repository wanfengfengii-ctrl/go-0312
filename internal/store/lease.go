package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/domain"
	"truss-thickplate-weld-restraint-release/internal/material"
)

// SaveResource inserts a leasable resource.
func (s *SQL) SaveResource(ctx context.Context, r material.Resource) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO resources(id, type) VALUES(?,?)`, r.ID, string(r.Type))
	return err
}

// Lease loads a single lease by ID.
func (s *SQL) Lease(ctx context.Context, id string) (material.Lease, error) {
	var l material.Lease
	err := s.q.QueryRowContext(ctx,
		`SELECT id, resource_id, operation, start_ts, end_ts, version FROM leases WHERE id=?`, id).
		Scan(&l.ID, &l.ResourceID, &l.Operation, &l.Start, &l.End, &l.Version)
	if errors.Is(err, sqlErrNoRows()) {
		return material.Lease{}, ErrNotFound
	}
	if err != nil {
		return material.Lease{}, err
	}
	return l, nil
}

// Leases loads every lease on a resource ordered by start time.
func (s *SQL) Leases(ctx context.Context, resourceID string) ([]material.Lease, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, resource_id, operation, start_ts, end_ts, version FROM leases WHERE resource_id=? ORDER BY start_ts`,
		resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []material.Lease
	for rows.Next() {
		var l material.Lease
		if err := rows.Scan(&l.ID, &l.ResourceID, &l.Operation, &l.Start, &l.End, &l.Version); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SaveLease inserts a new lease; the primary key on ID enforces uniqueness.
func (s *SQL) SaveLease(ctx context.Context, l material.Lease) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO leases(id, resource_id, operation, start_ts, end_ts, version) VALUES(?,?,?,?,?,?)`,
		l.ID, l.ResourceID, l.Operation, int64(l.Start), int64(l.End), l.Version)
	return err
}

// UpdateLeaseEnd performs a compare-and-swap renewal: it only extends the end
// time when the stored version matches, otherwise the caller observes no rows
// affected and must treat the lease as stale.
func (s *SQL) UpdateLeaseEnd(ctx context.Context, id string, newEnd domain.Milliseconds, version int64) error {
	res, err := s.q.ExecContext(ctx,
		`UPDATE leases SET end_ts=? WHERE id=? AND version=?`, int64(newEnd), id, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveDeviceCall inserts or updates a scripted device call record.
func (s *SQL) SaveDeviceCall(ctx context.Context, c material.DeviceCall) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO device_calls(id, resource_id, call_json) VALUES(?,?,?)
		 ON CONFLICT(id) DO UPDATE SET resource_id=excluded.resource_id, call_json=excluded.call_json`,
		c.ID, c.ResourceID, string(data))
	return err
}

// DeviceCall loads a device call by ID.
func (s *SQL) DeviceCall(ctx context.Context, id string) (material.DeviceCall, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT call_json FROM device_calls WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return material.DeviceCall{}, ErrNotFound
	}
	if err != nil {
		return material.DeviceCall{}, err
	}
	var c material.DeviceCall
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return material.DeviceCall{}, err
	}
	return c, nil
}
