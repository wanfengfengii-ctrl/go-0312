package store

// schema is the complete SQLite DDL. Unique constraints enforce the documented
// failure boundaries: one terminal verdict per task, one idempotency record per
// operation, one holding generation per container and one lease identity per
// resource interval (checked in Go for the actual overlap condition).
const schema = `
CREATE TABLE IF NOT EXISTS catalog_revisions (
    id             TEXT PRIMARY KEY,
    effective_time INTEGER NOT NULL,
    revision_json  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS node_tasks (
    id           TEXT PRIMARY KEY,
    generation   INTEGER NOT NULL,
    status       TEXT NOT NULL,
    task_json    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS material_packages (
    id        TEXT PRIMARY KEY,
    batch_id  TEXT NOT NULL,
    pkg_json  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS material_ledger (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id TEXT NOT NULL,
    entry_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS holding_generations (
    id        TEXT PRIMARY KEY,
    package_id TEXT NOT NULL,
    oven_id   TEXT NOT NULL,
    started_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS container_occupancy (
    container_id TEXT PRIMARY KEY,
    package_id   TEXT NOT NULL,
    batch_id     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS resources (
    id   TEXT PRIMARY KEY,
    type TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS leases (
    id          TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL,
    operation   TEXT NOT NULL,
    start_ts    INTEGER NOT NULL,
    end_ts      INTEGER NOT NULL,
    version     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS device_calls (
    id         TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL,
    call_json  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evidence_events (
    id           TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL,
    logical_time INTEGER NOT NULL,
    ev_json      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pass_prefix (
    task_id        TEXT PRIMARY KEY,
    version        INTEGER NOT NULL,
    completed_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS thermal_barrier (
    task_id     TEXT PRIMARY KEY,
    version     INTEGER NOT NULL,
    established INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS defects (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    defect_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repair_generations (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    number      INTEGER NOT NULL,
    repair_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gouging_records (
    id          TEXT PRIMARY KEY,
    defect_id   TEXT NOT NULL,
    repair_id   TEXT NOT NULL,
    volume      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS retest_results (
    id          TEXT PRIMARY KEY,
    repair_id   TEXT NOT NULL,
    retest_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reviews (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    review_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS terminal_verdicts (
    task_id    TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    credential TEXT NOT NULL,
    version    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    operation_id TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    response     TEXT NOT NULL
);
`
