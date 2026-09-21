package store

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS meta(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  frame TEXT NOT NULL DEFAULT 'spec',   -- active analysis frame
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS imports(
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  batch_label TEXT NOT NULL,
  imported_at TEXT NOT NULL DEFAULT (datetime('now')),
  item_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS specimens(
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  name TEXT NOT NULL,
  azimuth REAL NOT NULL,
  plunge REAL NOT NULL,
  roll REAL NOT NULL,
  strike REAL,
  dip REAL,
  UNIQUE(project_id, code)
);

CREATE TABLE IF NOT EXISTS steps(
  id INTEGER PRIMARY KEY,
  specimen_id INTEGER NOT NULL REFERENCES specimens(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  kind TEXT NOT NULL,
  level REAL NOT NULL,
  x REAL NOT NULL, y REAL NOT NULL, z REAL NOT NULL,
  cov_xx REAL NOT NULL, cov_xy REAL NOT NULL, cov_xz REAL NOT NULL DEFAULT 0,
  cov_yy REAL NOT NULL, cov_yz REAL NOT NULL DEFAULT 0, cov_zz REAL NOT NULL,
  azimuth REAL NOT NULL, plunge REAL NOT NULL, roll REAL NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  UNIQUE(specimen_id, seq)
);

-- Candidate windows are retained side by side.  Parameters are versioned:
-- changing the window/constraint/weighting inserts a new row instead of
-- overwriting, so no single screening is treated as the unique answer.
CREATE TABLE IF NOT EXISTS candidates(
  id INTEGER PRIMARY KEY,
  specimen_id INTEGER NOT NULL REFERENCES specimens(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  frame TEXT NOT NULL,
  from_seq INTEGER NOT NULL,
  to_seq INTEGER NOT NULL,
  origin TEXT NOT NULL,
  weighting TEXT NOT NULL,
  manual_exclude TEXT NOT NULL DEFAULT '[]',
  bootstrap_seed INTEGER NOT NULL,
  bootstrap_repeats INTEGER NOT NULL DEFAULT 2000,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Immutable computed analysis layers.  Each parameter revision produces a new
-- version row; the full result, diagnostics and transform chain are stored.
CREATE TABLE IF NOT EXISTS analyses(
  id INTEGER PRIMARY KEY,
  candidate_id INTEGER NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  spec_json TEXT NOT NULL,
  result_json TEXT NOT NULL,
  chain_json TEXT NOT NULL,
  computed_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(candidate_id, version)
);

-- Manual adjudication is a separate layer on top of computed results.
CREATE TABLE IF NOT EXISTS decisions(
  id INTEGER PRIMARY KEY,
  candidate_id INTEGER NOT NULL UNIQUE REFERENCES candidates(id) ON DELETE CASCADE,
  accepted INTEGER NOT NULL DEFAULT 0,
  rationale TEXT NOT NULL DEFAULT '',
  reviewer TEXT NOT NULL DEFAULT '',
  decided_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS run_log(
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL DEFAULT (datetime('now')),
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_steps_spec ON steps(specimen_id, seq);
CREATE INDEX IF NOT EXISTS idx_cand_spec ON candidates(specimen_id);
`
