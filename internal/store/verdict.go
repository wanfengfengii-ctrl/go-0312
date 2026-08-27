package store

import (
	"context"
	"encoding/json"
	"errors"

	"truss-thickplate-weld-restraint-release/internal/repair"
	"truss-thickplate-weld-restraint-release/internal/task"
)

// CreateReview inserts a review by a qualified person.
func (s *SQL) CreateReview(ctx context.Context, r repair.Review) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx,
		`INSERT INTO reviews(id, task_id, review_json) VALUES(?,?,?)`, r.ID, r.TaskID, string(data))
	return err
}

// Reviews lists a task's reviews ordered by created time.
func (s *SQL) Reviews(ctx context.Context, taskID task.TaskID) ([]repair.Review, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT review_json FROM reviews WHERE task_id=? ORDER BY id`, string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repair.Review
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r repair.Review
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Verdict loads a task's terminal verdict, if any.
func (s *SQL) Verdict(ctx context.Context, taskID task.TaskID) (repair.TerminalVerdict, error) {
	var v repair.TerminalVerdict
	var typ string
	err := s.q.QueryRowContext(ctx,
		`SELECT task_id, type, credential, version FROM terminal_verdicts WHERE task_id=?`,
		string(taskID)).Scan(&v.TaskID, &typ, &v.Credential, &v.Version)
	if errors.Is(err, sqlErrNoRows()) {
		return repair.TerminalVerdict{}, ErrNotFound
	}
	if err != nil {
		return repair.TerminalVerdict{}, err
	}
	v.Type = repair.VerdictType(typ)
	return v, nil
}

// SaveVerdict inserts a terminal verdict. The task_id primary key is the
// single-writer barrier: a second insert for the same task violates uniqueness
// and is reported as a conflict by the caller via ErrVerdictConflict.
func (s *SQL) SaveVerdict(ctx context.Context, v repair.TerminalVerdict) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO terminal_verdicts(task_id, type, credential, version) VALUES(?,?,?,?)`,
		v.TaskID, string(v.Type), v.Credential, v.Version)
	if isUniqueViolation(err) {
		return ErrVerdictConflict
	}
	return err
}

// Idempotency loads an idempotency record by operation ID.
func (s *SQL) Idempotency(ctx context.Context, operationID string) (IdempotencyRecord, error) {
	var rec IdempotencyRecord
	var resp string
	err := s.q.QueryRowContext(ctx,
		`SELECT operation_id, content_hash, response FROM idempotency_records WHERE operation_id=?`,
		operationID).Scan(&rec.OperationID, &rec.ContentHash, &resp)
	if errors.Is(err, sqlErrNoRows()) {
		return IdempotencyRecord{}, ErrNotFound
	}
	if err != nil {
		return IdempotencyRecord{}, err
	}
	rec.Response = []byte(resp)
	return rec, nil
}

// SaveIdempotency inserts an idempotency record keyed by operation ID.
func (s *SQL) SaveIdempotency(ctx context.Context, rec IdempotencyRecord) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO idempotency_records(operation_id, content_hash, response) VALUES(?,?,?)`,
		rec.OperationID, rec.ContentHash, string(rec.Response))
	return err
}
