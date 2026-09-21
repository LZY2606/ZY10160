package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is the application's SQLite persistence layer.
type Store struct{ db *sql.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	s := &Store{db: db}
	if err := s.initProject(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initProject(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id, name, frame) VALUES (1, '离线退磁工作台', 'G')`)
	return err
}

type Project struct {
	Name  string `json:"name"`
	Frame string `json:"frame"`
}

func (s *Store) GetProject(ctx context.Context) (Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, `SELECT name, frame FROM projects WHERE id = 1`).Scan(&p.Name, &p.Frame)
	return p, err
}

func (s *Store) SetFrame(ctx context.Context, frame string) (Project, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET frame = ? WHERE id = 1`, frame); err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx)
}
