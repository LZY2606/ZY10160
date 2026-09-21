package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Analysis is one persisted, versioned computation layer.
type Analysis struct {
	ID            string          `json:"id"`
	SpecimenID    string          `json:"specimen_id"`
	Version       int             `json:"version"`
	ParentID      sql.NullString  `json:"parent_id"`
	Frame         string          `json:"frame"`
	Treatment     string          `json:"treatment"`
	LevelFrom     sql.NullFloat64 `json:"level_from"`
	LevelTo       sql.NullFloat64 `json:"level_to"`
	Anchored      bool            `json:"anchored"`
	Weighting     string          `json:"weighting"`
	BootstrapSeed int64           `json:"bootstrap_seed"`
	SelectedKeys  string          `json:"-"`
	ExcludedKeys  string          `json:"-"`
	ResultJSON    string          `json:"-"`
	Note          string          `json:"note"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     string          `json:"created_at"`
}

// Decision is the separate human layer attached to a computation version.
type Decision struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	AnalysisID string `json:"analysis_id"`
	Version    int    `json:"version"`
	Action     string `json:"action"`
	Rationale  string `json:"rationale"`
	Author     string `json:"author"`
	CreatedAt  string `json:"created_at"`
}

// NextAnalysisVersion returns specimen version + 1 under a transaction lock.
func (s *Store) NextAnalysisVersion(ctx context.Context, specimenID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var v sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT max(version) FROM analyses WHERE specimen_id=?`, specimenID).Scan(&v)
	if err != nil {
		return 0, err
	}
	next := 1
	if v.Valid {
		next = int(v.Int64) + 1
	}
	tx.Commit()
	return next, nil
}

func (s *Store) InsertAnalysis(ctx context.Context, a Analysis) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analyses(id,specimen_id,version,parent_id,frame,treatment,
		  level_from,level_to,anchored,weighting,bootstrap_seed,
		  selected_keys,excluded_keys,result_json,note,created_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.SpecimenID, a.Version, a.ParentID, a.Frame, a.Treatment,
		a.LevelFrom, a.LevelTo, a.Anchored, a.Weighting, a.BootstrapSeed,
		a.SelectedKeys, a.ExcludedKeys, a.ResultJSON, a.Note, a.CreatedBy)
	return err
}

func scanAnalysis(row interface {
	Scan(...any) error
}) (Analysis, error) {
	var a Analysis
	err := row.Scan(&a.ID, &a.SpecimenID, &a.Version, &a.ParentID, &a.Frame,
		&a.Treatment, &a.LevelFrom, &a.LevelTo, &a.Anchored, &a.Weighting,
		&a.BootstrapSeed, &a.SelectedKeys, &a.ExcludedKeys, &a.ResultJSON,
		&a.Note, &a.CreatedBy, &a.CreatedAt)
	return a, err
}

const analysisCols = `id,specimen_id,version,parent_id,frame,treatment,
  level_from,level_to,anchored,weighting,bootstrap_seed,
  selected_keys,excluded_keys,result_json,note,created_by,created_at`

func (s *Store) GetAnalysis(ctx context.Context, id string) (Analysis, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+analysisCols+` FROM analyses WHERE id=?`, id)
	return scanAnalysis(row)
}

func (s *Store) ListAnalyses(ctx context.Context, specimenID string) ([]Analysis, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+analysisCols+` FROM analyses WHERE specimen_id=? ORDER BY version`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Analysis
	for rows.Next() {
		a, err := scanAnalysis(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAnalysis removes a candidate but keeps the version number consumed
// (append-only history). Candidates may coexist; this only supports explicit
// discards recorded in the run log.
func (s *Store) DeleteAnalysis(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM analyses WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("analysis not found")
	}
	return nil
}

func (s *Store) NextDecisionVersion(ctx context.Context, specimenID string) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT max(version) FROM decisions WHERE specimen_id=?`, specimenID).Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 1, nil
	}
	return int(v.Int64) + 1, nil
}

func (s *Store) UpsertDecision(ctx context.Context, d Decision) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO decisions(id,specimen_id,analysis_id,version,action,rationale,author)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(specimen_id, version) DO UPDATE SET
		  analysis_id=excluded.analysis_id, action=excluded.action,
		  rationale=excluded.rationale, author=excluded.author`,
		d.ID, d.SpecimenID, d.AnalysisID, d.Version, d.Action, d.Rationale, d.Author)
	return err
}

func (s *Store) ListDecisions(ctx context.Context, specimenID string) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,specimen_id,analysis_id,version,action,rationale,author,created_at
		FROM decisions WHERE specimen_id=? ORDER BY version`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.SpecimenID, &d.AnalysisID, &d.Version,
			&d.Action, &d.Rationale, &d.Author, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type RunEntry struct {
	ID          int64          `json:"id"`
	TS          string         `json:"ts"`
	Actor       string         `json:"actor"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Status      int            `json:"status"`
	RequestHash sql.NullString `json:"request_hash"`
	Summary     string         `json:"summary"`
}

func (s *Store) LogRun(ctx context.Context, e RunEntry) {
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO run_log(actor,method,path,status,request_hash,summary)
		VALUES(?,?,?,?,?,?)`,
		e.Actor, e.Method, e.Path, e.Status, e.RequestHash, e.Summary)
}

func (s *Store) ListRunLog(ctx context.Context, limit int) ([]RunEntry, error) {
	if limit <= 0 {
		limit = 100000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,ts,actor,method,path,status,request_hash,summary
		FROM run_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunEntry
	for rows.Next() {
		var e RunEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Method, &e.Path,
			&e.Status, &e.RequestHash, &e.Summary); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ResetAll empties operational data while preserving schema, so the fixed
// fixtures can be re-imported for clean-room replay.
func (s *Store) ResetAll(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"run_log", "decisions", "analyses", "steps", "specimens"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,frame) VALUES (1,'离线退磁工作台','G')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET frame='G' WHERE id=1`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.LogRun(ctx, RunEntry{Actor: "system", Method: "POST", Path: "/api/admin/reset",
		Status: 200, Summary: "database emptied at " + time.Now().UTC().Format(time.RFC3339)})
	return nil
}
