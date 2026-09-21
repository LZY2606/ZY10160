package store

import (
	"context"
	"database/sql"
)

// ListSpecimens returns specimens for a project.
func (s *Store) ListSpecimens(ctx context.Context, projectID int64) ([]Specimen, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id,project_id,code,name,azimuth,plunge,roll,strike,dip
		FROM specimens WHERE project_id=? ORDER BY code`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Specimen
	for rows.Next() {
		sp, err := scanSpecimen(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// GetSpecimen loads one specimen.
func (s *Store) GetSpecimen(ctx context.Context, id int64) (*Specimen, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id,project_id,code,name,azimuth,plunge,roll,strike,dip
		FROM specimens WHERE id=?`, id)
	sp, err := scanSpecimen(row)
	if err != nil {
		return nil, err
	}
	return &sp, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSpecimen(r rowScanner) (Specimen, error) {
	var sp Specimen
	if err := r.Scan(&sp.ID, &sp.ProjectID, &sp.Code, &sp.Name,
		&sp.Azimuth, &sp.Plunge, &sp.Roll, &sp.Strike, &sp.Dip); err != nil {
		return Specimen{}, err
	}
	return sp, nil
}

// ListSteps loads a specimen's steps in measurement order.
func (s *Store) ListSteps(ctx context.Context, specimenID int64) ([]Step, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id,specimen_id,seq,kind,level,
		  x,y,z, cov_xx,cov_xy,cov_xz,cov_yy,cov_yz,cov_zz,
		  azimuth,plunge,roll,note
		FROM steps WHERE specimen_id=? ORDER BY seq`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Step
	for rows.Next() {
		var st Step
		if err := rows.Scan(&st.ID, &st.SpecimenID, &st.Seq, &st.Kind, &st.Level,
			&st.X, &st.Y, &st.Z,
			&st.Cov[0][0], &st.Cov[0][1], &st.Cov[0][2],
			&st.Cov[1][1], &st.Cov[1][2], &st.Cov[2][2],
			&st.Azimuth, &st.Plunge, &st.Roll, &st.Note); err != nil {
			return nil, err
		}
		st.Cov[1][0] = st.Cov[0][1]
		st.Cov[2][0] = st.Cov[0][2]
		st.Cov[2][1] = st.Cov[1][2]
		out = append(out, st)
	}
	return out, rows.Err()
}

// UpsertSpecimen inserts a specimen and replaces all its steps in one
// transaction.  Re-importing the same specimen code is idempotent and returns
// the specimen id.
func (s *Store) UpsertSpecimen(ctx context.Context, projectID int64,
	sp Specimen, steps []Step, importLabel string) (int64, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO specimens(project_id,code,name,azimuth,plunge,roll,strike,dip)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id,code) DO UPDATE SET
		  name=excluded.name, azimuth=excluded.azimuth, plunge=excluded.plunge,
		  roll=excluded.roll, strike=excluded.strike, dip=excluded.dip`,
		projectID, sp.Code, sp.Name, sp.Azimuth, sp.Plunge, sp.Roll, sp.Strike, sp.Dip)
	if err != nil {
		return 0, 0, err
	}
	var specID int64
	if id, err := res.LastInsertId(); err == nil && id > 0 {
		specID = id
	} else {
		row := tx.QueryRowContext(ctx,
			`SELECT id FROM specimens WHERE project_id=? AND code=?`, projectID, sp.Code)
		if err := row.Scan(&specID); err != nil {
			return 0, 0, err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM steps WHERE specimen_id=?`, specID); err != nil {
		return 0, 0, err
	}
	// Candidates from a previous import of this same code are dropped so the
	// window/seq references remain consistent with the reimported levels.
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE specimen_id=?`, specID); err != nil {
		return 0, 0, err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO steps(specimen_id,seq,kind,level,x,y,z,
		  cov_xx,cov_xy,cov_xz,cov_yy,cov_yz,cov_zz,azimuth,plunge,roll,note)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()
	for _, st := range steps {
		if _, err := stmt.ExecContext(ctx, specID, st.Seq, st.Kind, st.Level,
			st.X, st.Y, st.Z,
			st.Cov[0][0], st.Cov[0][1], st.Cov[0][2],
			st.Cov[1][1], st.Cov[1][2], st.Cov[2][2],
			st.Azimuth, st.Plunge, st.Roll, st.Note); err != nil {
			return 0, 0, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO imports(project_id,batch_label,item_count) VALUES(?,?,?)`,
		projectID, importLabel, len(steps)); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return specID, len(steps), nil
}

// WipeAll clears every imported row but keeps the schema.  Used by the
// re-import verification flow.
func (s *Store) WipeAll(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM decisions`, `DELETE FROM analyses`, `DELETE FROM candidates`,
		`DELETE FROM steps`, `DELETE FROM specimens`, `DELETE FROM imports`,
		`DELETE FROM projects`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO run_log(actor,action,detail) VALUES(?,?,?)`,
		"system", "wipe", "all data cleared for re-import verification")
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ = sql.ErrNoRows
