package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/task"
)

// SaveTask inserts or updates a node task aggregate. It stores the full locked
// snapshot as JSON plus queryable generation and status columns.
func (s *SQL) SaveTask(ctx context.Context, t task.NodeTask) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO node_tasks(id, generation, status, task_json) VALUES(?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET generation=excluded.generation, status=excluded.status, task_json=excluded.task_json`,
		string(t.ID), int64(t.Generation), string(t.Status), string(data))
	return err
}

// Task loads a node task aggregate by ID.
func (s *SQL) Task(ctx context.Context, id task.TaskID) (task.NodeTask, error) {
	var raw string
	err := s.q.QueryRowContext(ctx,
		`SELECT task_json FROM node_tasks WHERE id=?`, string(id)).Scan(&raw)
	if errors.Is(err, sqlErrNoRows()) {
		return task.NodeTask{}, ErrNotFound
	}
	if err != nil {
		return task.NodeTask{}, err
	}
	var t task.NodeTask
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return task.NodeTask{}, err
	}
	return t, nil
}

// ListTasks returns every task ordered by ID for the frontend overview.
func (s *SQL) ListTasks(ctx context.Context) ([]task.NodeTask, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT task_json FROM node_tasks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]task.NodeTask, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var t task.NodeTask
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
