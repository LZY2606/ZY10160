package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// AnalysisRecord is one immutable computed version for a candidate.
type AnalysisRecord struct {
	ID          int64           `json:"id"`
	CandidateID int64           `json:"candidate_id"`
	Version     int             `json:"version"`
	Spec        json.RawMessage `json:"spec_json"`
	Result      json.RawMessage `json:"result_json"`
	Chain       json.RawMessage `json:"chain_json"`
	ComputedAt  string          `json:"computed_at"`
}

// CreateCandidate inserts a new candidate window (candidate windows are kept
// side by side; this never updates an existing one).
func (s *Store) CreateCandidate(ctx context.Context, c Candidate) (int64, error) {
	excl, _ := json.Marshal(c.ManualExclude)
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO candidates(specimen_id,label,frame,from_seq,to_seq,origin,
		  weighting,manual_exclude,bootstrap_seed,bootstrap_repeats)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.SpecimenID, c.Label, c.Frame, c.FromSeq, c.ToSeq, c.Origin, c.Weighting,
		string(excl), c.BootstrapSeed, c.BootstrapRepeats)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	_, _ = s.DB.ExecContext(ctx,
		`INSERT INTO decisions(candidate_id,accepted,rationale,reviewer)
		 VALUES(?,0,'','')`, id)
	return id, nil
}

// ListCandidates returns every candidate for a specimen with its decision and
// latest analysis version.
func (s *Store) ListCandidates(ctx context.Context, specimenID int64) ([]Candidate, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id,c.specimen_id,c.label,c.frame,c.from_seq,c.to_seq,c.origin,
		  c.weighting,c.manual_exclude,c.bootstrap_seed,c.bootstrap_repeats,c.created_at,
		  COALESCE(d.accepted,0), COALESCE(d.rationale,''),
		  COALESCE((SELECT MAX(version) FROM analyses a WHERE a.candidate_id=c.id),0)
		FROM candidates c LEFT JOIN decisions d ON d.candidate_id=c.id
		WHERE c.specimen_id=? ORDER BY c.id`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var excl string
		var accepted int
		if err := rows.Scan(&c.ID, &c.SpecimenID, &c.Label, &c.Frame,
			&c.FromSeq, &c.ToSeq, &c.Origin, &c.Weighting, &excl,
			&c.BootstrapSeed, &c.BootstrapRepeats, &c.CreatedAt,
			&accepted, &c.Rationale, &c.LatestVersion); err != nil {
			return nil, err
		}
		c.Accepted = accepted == 1
		_ = json.Unmarshal([]byte(excl), &c.ManualExclude)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCandidate loads one candidate.
func (s *Store) GetCandidate(ctx context.Context, id int64) (*Candidate, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id,specimen_id,label,frame,from_seq,to_seq,origin,weighting,
		  manual_exclude,bootstrap_seed,bootstrap_repeats,created_at
		FROM candidates WHERE id=?`, id)
	var c Candidate
	var excl string
	if err := row.Scan(&c.ID, &c.SpecimenID, &c.Label, &c.Frame,
		&c.FromSeq, &c.ToSeq, &c.Origin, &c.Weighting, &excl,
		&c.BootstrapSeed, &c.BootstrapRepeats, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(excl), &c.ManualExclude)
	return &c, nil
}

// AddAnalysis appends a new immutable computed version for a candidate and
// returns the version number.
func (s *Store) AddAnalysis(ctx context.Context, candidateID int64,
	specJSON, resultJSON, chainJSON []byte) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM analyses WHERE candidate_id=?`,
		candidateID).Scan(&next); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO analyses(candidate_id,version,spec_json,result_json,chain_json)
		VALUES(?,?,?,?,?)`, candidateID, next, string(specJSON), string(resultJSON),
		string(chainJSON)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// ListAnalyses returns stored versions for a candidate, newest first.
func (s *Store) ListAnalyses(ctx context.Context, candidateID int64) ([]AnalysisRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id,candidate_id,version,spec_json,result_json,chain_json,computed_at
		FROM analyses WHERE candidate_id=? ORDER BY version DESC`, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalysisRecord
	for rows.Next() {
		var a AnalysisRecord
		var spec, res, ch string
		if err := rows.Scan(&a.ID, &a.CandidateID, &a.Version, &spec, &res, &ch, &a.ComputedAt); err != nil {
			return nil, err
		}
		a.Spec = json.RawMessage(spec)
		a.Result = json.RawMessage(res)
		a.Chain = json.RawMessage(ch)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetDecision writes the manual adjudication layer.
func (s *Store) SetDecision(ctx context.Context, candidateID int64, accepted bool,
	rationale, reviewer string) error {
	v := 0
	if accepted {
		v = 1
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE decisions SET accepted=?, rationale=?, reviewer=?,
		  decided_at=datetime('now') WHERE candidate_id=?`,
		v, rationale, reviewer, candidateID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("candidate not found")
	}
	return nil
}
