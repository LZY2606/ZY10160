package store

import (
	"context"
	"database/sql"
)

type Specimen struct {
	ID          string
	Name        string
	Description string
	MountTrend  float64
	MountPlunge float64
	BedDipAz    float64
	BedDip      float64
}

type Step struct {
	SpecimenID string
	StepKey    string
	Treatment  string
	Level      float64
	Rep        int
	X, Y, Z    float64
	CovXX      float64
	CovYY      float64
	CovZZ      float64
	CovXY      float64
	CovYZ      float64
	CovXZ      float64
}

// UpsertSpecimen is idempotent: re-importing the same specimen replaces its
// raw rows but never merges duplicate treatment levels with each other.
func (s *Store) UpsertSpecimen(ctx context.Context, sp Specimen, steps []Step) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO specimens(id,name,description,mount_trend,mount_plunge,bed_dip_az,bed_dip)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name=excluded.name, description=excluded.description,
		  mount_trend=excluded.mount_trend, mount_plunge=excluded.mount_plunge,
		  bed_dip_az=excluded.bed_dip_az, bed_dip=excluded.bed_dip,
		  imported_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		sp.ID, sp.Name, sp.Description, sp.MountTrend, sp.MountPlunge,
		sp.BedDipAz, sp.BedDip); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM steps WHERE specimen_id=?`, sp.ID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO steps(specimen_id,step_key,treatment,level,rep,x,y,z,
		  cov_xx,cov_yy,cov_zz,cov_xy,cov_yz,cov_xz)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, st := range steps {
		if st.SpecimenID == "" {
			st.SpecimenID = sp.ID
		}
		if _, err := stmt.ExecContext(ctx, st.SpecimenID, st.StepKey, st.Treatment,
			st.Level, st.Rep, st.X, st.Y, st.Z,
			st.CovXX, st.CovYY, st.CovZZ, st.CovXY, st.CovYZ, st.CovXZ); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSpecimens(ctx context.Context) ([]Specimen, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,name,description,mount_trend,mount_plunge,bed_dip_az,bed_dip
		FROM specimens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Specimen
	for rows.Next() {
		var sp Specimen
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Description, &sp.MountTrend,
			&sp.MountPlunge, &sp.BedDipAz, &sp.BedDip); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Store) GetSpecimen(ctx context.Context, id string) (Specimen, error) {
	var sp Specimen
	err := s.db.QueryRowContext(ctx, `
		SELECT id,name,description,mount_trend,mount_plunge,bed_dip_az,bed_dip
		FROM specimens WHERE id=?`, id).Scan(
		&sp.ID, &sp.Name, &sp.Description, &sp.MountTrend,
		&sp.MountPlunge, &sp.BedDipAz, &sp.BedDip)
	return sp, err
}

func (s *Store) ListSteps(ctx context.Context, specimenID string) ([]Step, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT specimen_id,step_key,treatment,level,rep,x,y,z,
		       cov_xx,cov_yy,cov_zz,cov_xy,cov_yz,cov_xz
		FROM steps WHERE specimen_id=?
		ORDER BY treatment, level, rep`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Step
	for rows.Next() {
		var st Step
		if err := rows.Scan(&st.SpecimenID, &st.StepKey, &st.Treatment, &st.Level,
			&st.Rep, &st.X, &st.Y, &st.Z,
			&st.CovXX, &st.CovYY, &st.CovZZ, &st.CovXY, &st.CovYZ, &st.CovXZ); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) CountSteps(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM steps`).Scan(&n)
	return n, err
}

var _ = sql.ErrNoRows
