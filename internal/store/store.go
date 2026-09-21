package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps the application database.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the SQLite database and applies the schema.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	s := &Store{DB: db}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// Project row.
type Project struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Frame     string `json:"frame"`
	CreatedAt string `json:"created_at"`
}

// Specimen row.
type Specimen struct {
	ID        int64    `json:"id"`
	ProjectID int64    `json:"project_id"`
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Azimuth   float64  `json:"azimuth"`
	Plunge    float64  `json:"plunge"`
	Roll      float64  `json:"roll"`
	Strike    *float64 `json:"strike"`
	Dip       *float64 `json:"dip"`
}

// Step row matching geomag.Step plus covariance fields.
type Step struct {
	ID         int64   `json:"id"`
	SpecimenID int64   `json:"specimen_id"`
	Seq        int     `json:"seq"`
	Kind       string  `json:"kind"`
	Level      float64 `json:"level"`
	X, Y, Z    float64
	Cov        [3][3]float64
	Azimuth    float64
	Plunge     float64
	Roll       float64
	Note       string
}

// Candidate row.
type Candidate struct {
	ID               int64  `json:"id"`
	SpecimenID       int64  `json:"specimen_id"`
	Label            string `json:"label"`
	Frame            string `json:"frame"`
	FromSeq          int    `json:"from_seq"`
	ToSeq            int    `json:"to_seq"`
	Origin           string `json:"origin"`
	Weighting        string `json:"weighting"`
	ManualExclude    []int  `json:"manual_exclude"`
	BootstrapSeed    uint64 `json:"bootstrap_seed"`
	BootstrapRepeats int    `json:"bootstrap_repeats"`
	CreatedAt        string `json:"created_at"`
	Accepted         bool   `json:"accepted"`
	Rationale        string `json:"rationale"`
	LatestVersion    int    `json:"latest_version"`
}

// RunLogEntry is one audit record.
type RunLogEntry struct {
	ID     int64  `json:"id"`
	TS     string `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// GetProject returns the single demo project, creating nothing implicitly.
func (s *Store) GetProject(ctx context.Context, id int64) (*Project, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,name,frame,created_at FROM projects WHERE id=?`, id)
	var p Project
	if err := row.Scan(&p.ID, &p.Name, &p.Frame, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// ProjectByName returns a project with the given name or nil.
func (s *Store) ProjectByName(ctx context.Context, name string) (*Project, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,name,frame,created_at FROM projects WHERE name=?`, name)
	var p Project
	if err := row.Scan(&p.ID, &p.Name, &p.Frame, &p.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// CreateProject inserts a project and returns its id.
func (s *Store) CreateProject(ctx context.Context, name, frame string) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects(name,frame) VALUES(?,?)`, name, frame)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetProjectFrame updates the active analysis frame.
func (s *Store) SetProjectFrame(ctx context.Context, id int64, frame string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE projects SET frame=? WHERE id=?`, frame, id)
	return err
}

// Log records an audit entry.
func (s *Store) Log(ctx context.Context, actor, action, detail string) {
	_, _ = s.DB.ExecContext(ctx,
		`INSERT INTO run_log(actor,action,detail) VALUES(?,?,?)`, actor, action, detail)
}

// RunLog returns audit entries newest-first.
func (s *Store) RunLog(ctx context.Context, limit int) ([]RunLogEntry, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,ts,actor,action,detail FROM run_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunLogEntry
	for rows.Next() {
		var e RunLogEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
