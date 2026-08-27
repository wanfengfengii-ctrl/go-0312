package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/task"
	"truss-thickplate-weld-restraint-release/internal/thermal"
)

// AppendEvidence appends one evidence event to a task's append-only log.
func (s *SQL) AppendEvidence(ctx context.Context, e thermal.EvidenceEvent) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO evidence_events(id, task_id, logical_time, ev_json) VALUES(?,?,?,?)`,
		e.ID, e.TaskID, int64(e.LogicalTime), string(data))
	return err
}

// Evidence loads a task's evidence ordered by logical time.
func (s *SQL) Evidence(ctx context.Context, taskID task.TaskID) ([]thermal.EvidenceEvent, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT ev_json FROM evidence_events WHERE task_id=? ORDER BY logical_time, id`, string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []thermal.EvidenceEvent
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e thermal.EvidenceEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SavePassPrefix upserts the completed-pass projection at a version.
func (s *SQL) SavePassPrefix(ctx context.Context, taskID task.TaskID, completed []string, version int64) error {
	data, err := json.Marshal(completed)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO pass_prefix(task_id, version, completed_json) VALUES(?,?,?)
		 ON CONFLICT(task_id) DO UPDATE SET version=excluded.version, completed_json=excluded.completed_json`,
		string(taskID), version, string(data))
	return err
}

// PassPrefix loads the completed-pass projection.
func (s *SQL) PassPrefix(ctx context.Context, taskID task.TaskID) (thermal.PassPrefixProjection, error) {
	var p thermal.PassPrefixProjection
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT version, completed_json FROM pass_prefix WHERE task_id=?`, string(taskID)).Scan(&p.Version, &raw)
	if errors.Is(err, sqlErrNoRows()) {
		return thermal.PassPrefixProjection{}, ErrNotFound
	}
	if err != nil {
		return thermal.PassPrefixProjection{}, err
	}
	if err := json.Unmarshal([]byte(raw), &p.Completed); err != nil {
		return thermal.PassPrefixProjection{}, err
	}
	return p, nil
}

// SaveThermalBarrier upserts the thermal-barrier projection at a version.
func (s *SQL) SaveThermalBarrier(ctx context.Context, taskID task.TaskID, established bool, version int64) error {
	est := 0
	if established {
		est = 1
	}
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO thermal_barrier(task_id, version, established) VALUES(?,?,?)
		 ON CONFLICT(task_id) DO UPDATE SET version=excluded.version, established=excluded.established`,
		string(taskID), version, est)
	return err
}

// ThermalBarrier loads the thermal-barrier projection.
func (s *SQL) ThermalBarrier(ctx context.Context, taskID task.TaskID) (thermal.ThermalBarrierProjection, error) {
	var p thermal.ThermalBarrierProjection
	var est int
	err := s.q.QueryRowContext(ctx,
		`SELECT version, established FROM thermal_barrier WHERE task_id=?`, string(taskID)).Scan(&p.Version, &est)
	if errors.Is(err, sqlErrNoRows()) {
		return thermal.ThermalBarrierProjection{}, ErrNotFound
	}
	if err != nil {
		return thermal.ThermalBarrierProjection{}, err
	}
	p.Established = est != 0
	return p, nil
}
