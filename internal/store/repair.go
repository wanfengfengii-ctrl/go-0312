package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// CreateDefect inserts a defect record.
func (s *SQL) CreateDefect(ctx context.Context, d repair.Defect) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO defects(id, task_id, defect_json) VALUES(?,?,?)`, d.ID, d.TaskID, string(data))
	return err
}

// Defect loads a defect by ID.
func (s *SQL) Defect(ctx context.Context, id string) (repair.Defect, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT defect_json FROM defects WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return repair.Defect{}, ErrNotFound
	}
	if err != nil {
		return repair.Defect{}, err
	}
	var d repair.Defect
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return repair.Defect{}, err
	}
	return d, nil
}

// CreateRepair inserts a repair generation.
func (s *SQL) CreateRepair(ctx context.Context, r repair.RepairGeneration) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO repair_generations(id, task_id, number, repair_json) VALUES(?,?,?,?)`,
		r.ID, r.TaskID, r.Number, string(data))
	return err
}

// CreateGouging inserts a gouging record retaining the removed defect volume.
func (s *SQL) CreateGouging(ctx context.Context, g repair.GougingRecord) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO gouging_records(id, defect_id, repair_id, volume) VALUES(?,?,?,?)`,
		g.ID, g.DefectID, g.RepairID, int64(g.Volume))
	return err
}

// CreateRetest inserts a retest result.
func (s *SQL) CreateRetest(ctx context.Context, r repair.RetestResult) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO retest_results(id, repair_id, retest_json) VALUES(?,?,?)`, r.ID, r.RepairID, string(data))
	return err
}

// Repairs lists a task's repair generations ordered by number.
func (s *SQL) Repairs(ctx context.Context, taskID task.TaskID) ([]repair.RepairGeneration, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT repair_json FROM repair_generations WHERE task_id=? ORDER BY number`, string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repair.RepairGeneration
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r repair.RepairGeneration
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RepairCount returns the number of repair generations for a task.
func (s *SQL) RepairCount(ctx context.Context, taskID task.TaskID) (int64, error) {
	var n int64
	err := s.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM repair_generations WHERE task_id=?`, string(taskID)).Scan(&n)
	return n, err
}

// Retests lists retest results for a repair generation ordered by created time.
func (s *SQL) Retests(ctx context.Context, repairID string) ([]repair.RetestResult, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT retest_json FROM retest_results WHERE repair_id=? ORDER BY id`, repairID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repair.RetestResult
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r repair.RetestResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
