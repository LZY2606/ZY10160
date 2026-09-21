package store

const schemaSQL = `
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS schema_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
  id           INTEGER PRIMARY KEY CHECK (id = 1),
  name         TEXT NOT NULL,
  frame        TEXT NOT NULL DEFAULT 'G',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS specimens (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  mount_trend  REAL NOT NULL,
  mount_plunge REAL NOT NULL,
  bed_dip_az   REAL NOT NULL,
  bed_dip      REAL NOT NULL,
  imported_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS steps (
  specimen_id TEXT NOT NULL REFERENCES specimens(id) ON DELETE CASCADE,
  step_key    TEXT NOT NULL,
  treatment   TEXT NOT NULL,
  level       REAL NOT NULL,
  rep         INTEGER NOT NULL DEFAULT 1,
  x           REAL NOT NULL,
  y           REAL NOT NULL,
  z           REAL NOT NULL,
  cov_xx      REAL NOT NULL,
  cov_yy      REAL NOT NULL,
  cov_zz      REAL NOT NULL,
  cov_xy      REAL NOT NULL DEFAULT 0,
  cov_yz      REAL NOT NULL DEFAULT 0,
  cov_xz      REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (specimen_id, step_key)
);
CREATE INDEX IF NOT EXISTS idx_steps_spec ON steps(specimen_id, treatment, level, rep);

-- Layered, append-only versioning: each recomputation inserts a new row.
CREATE TABLE IF NOT EXISTS analyses (
  id             TEXT PRIMARY KEY,
  specimen_id    TEXT NOT NULL REFERENCES specimens(id) ON DELETE CASCADE,
  version        INTEGER NOT NULL,
  parent_id      TEXT,
  frame          TEXT NOT NULL,
  treatment      TEXT NOT NULL,
  level_from     REAL,
  level_to       REAL,
  anchored       INTEGER NOT NULL,
  weighting      TEXT NOT NULL,
  bootstrap_seed INTEGER NOT NULL,
  selected_keys  TEXT NOT NULL,   -- JSON array of ordered step keys used
  excluded_keys  TEXT NOT NULL,   -- JSON array of {key,reason}
  result_json    TEXT NOT NULL,   -- full geom.FitResult
  note           TEXT NOT NULL DEFAULT '',
  created_by     TEXT NOT NULL DEFAULT 'researcher',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (specimen_id, version)
);
CREATE INDEX IF NOT EXISTS idx_analyses_spec ON analyses(specimen_id, created_at);

-- Human decisions form a separate layer from computed results.
CREATE TABLE IF NOT EXISTS decisions (
  id          TEXT PRIMARY KEY,
  specimen_id TEXT NOT NULL REFERENCES specimens(id) ON DELETE CASCADE,
  analysis_id TEXT NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
  version     INTEGER NOT NULL,
  action      TEXT NOT NULL CHECK (action IN ('candidate','accepted','rejected')),
  rationale   TEXT NOT NULL DEFAULT '',
  author      TEXT NOT NULL DEFAULT 'researcher',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (specimen_id, version)
);

-- Operation journal enabling run-record export and replay review.
CREATE TABLE IF NOT EXISTS run_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ts          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  actor       TEXT NOT NULL DEFAULT 'researcher',
  method      TEXT NOT NULL,
  path        TEXT NOT NULL,
  status      INTEGER NOT NULL,
  request_hash TEXT,
  summary     TEXT NOT NULL DEFAULT ''
);
`
