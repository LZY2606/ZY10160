package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
)

func sqlNullFloat(v float64, valid bool) sql.NullFloat64 {
	return sql.NullFloat64{Float64: v, Valid: valid}
}
func sqlNullString(v string, valid bool) sql.NullString {
	return sql.NullString{String: v, Valid: valid}
}
func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

func (s *Service) CountSpecimens(ctx context.Context) (int, error) {
	sps, err := s.st.ListSpecimens(ctx)
	return len(sps), err
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}
