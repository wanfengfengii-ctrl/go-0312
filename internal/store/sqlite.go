package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// queryer is the minimal SQL surface shared by *sql.DB and *sql.Tx so that a
// single method set backs both the root store and transaction-bound stores.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SQL is the SQLite-backed Store. It is safe for concurrent use: each method
// uses the connection pool's own handle, and WithTx runs fn inside a single
// *sql.Tx so multi-write operations commit atomically.
type SQL struct {
	q  queryer
	db *sql.DB // nil for transaction-bound stores
}

// Open opens (or creates) the embedded database at path and applies the schema.
// A path of ":memory:" yields an in-memory database used by tests and ad-hoc
// processes; a file path yields durable, restartable persistence.
func Open(path string) (*SQL, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is effectively single-writer; a single connection avoids
	// busy/deadlock surprises and keeps transaction handling simple.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &SQL{q: db, db: db}, nil
}

// Close releases the database handle.
func (s *SQL) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithTx runs fn inside a single transaction. A returned error rolls back.
func (s *SQL) WithTx(ctx context.Context, fn func(Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txs := &SQL{q: tx}
	if err := fn(txs); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func sqlErrNoRows() error { return sql.ErrNoRows }
