// Package ingest wires raw imported levels through the documented rotation
// chain into analysis-frame points and runs reproducible PCA fits.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"polaritytrace/internal/fixture"
	"polaritytrace/internal/geom"
	"polaritytrace/internal/store"
)

type Service struct{ st *store.Store }

func New(st *store.Store) *Service     { return &Service{st: st} }
func (s *Service) Store() *store.Store { return s.st }

// SeedFixtures imports (or re-imports) the fixed acceptance specimens.
func (s *Service) SeedFixtures(ctx context.Context) (int, error) {
	specs, err := fixture.LoadAll()
	if err != nil {
		return 0, err
	}
	for _, sp := range specs {
		dbSpec := store.Specimen{
			ID: sp.ID, Name: sp.Name, Description: sp.Description,
			MountTrend: sp.Mount.TrendDeg, MountPlunge: sp.Mount.PlungeDeg,
			BedDipAz: sp.Bedding.DipAzimuthDeg, BedDip: sp.Bedding.DipAngleDeg,
		}
		var steps []store.Step
		for _, st := range sp.Steps {
			steps = append(steps, store.Step{
				StepKey: st.Key, Treatment: st.Treatment, Level: st.Level, Rep: st.Rep,
				X: st.X, Y: st.Y, Z: st.Z,
				CovXX: st.Cov[0], CovYY: st.Cov[1], CovZZ: st.Cov[2],
				CovXY: st.Cov[3], CovYZ: st.Cov[4], CovXZ: st.Cov[5],
			})
		}
		if err := s.st.UpsertSpecimen(ctx, dbSpec, steps); err != nil {
			return 0, err
		}
	}
	return len(specs), nil
}

// TransformedStep is one level with its vector expressed in the active frame
// plus the exact rotation chain producing it.
type TransformedStep struct {
	Key          string         `json:"key"`
	Treatment    string         `json:"treatment"`
	Level        float64        `json:"level"`
	Rep          int            `json:"rep"`
	Frame        string         `json:"frame"`
	V            geom.Vec3      `json:"v"`
	Norm         float64        `json:"norm_ma_m"`
	Cov          geom.Sym3      `json:"cov"`
	Chain        []geom.RotStep `json:"chain"`
	RoundTripErr float64        `json:"roundtrip_max_abs_err"`
}

type Excluded struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

type WindowSpec struct {
	Treatment string   `json:"treatment"`
	LevelFrom *float64 `json:"level_from"`
	LevelTo   *float64 `json:"level_to"`
	Keys      []string `json:"keys"`
}

type CreateAnalysisRequest struct {
	SpecimenID     string     `json:"specimen_id"`
	Frame          string     `json:"frame"`
	Window         WindowSpec `json:"window"`
	OriginAnchored bool       `json:"origin_anchored"`
	Weighting      string     `json:"weighting"`
	BootstrapSeed  int64      `json:"bootstrap_seed"`
	Note           string     `json:"note"`
	ParentID       string     `json:"parent_id"`
}

type AnalysisView struct {
	store.Analysis
	ParentIDV  *string         `json:"parent_id"`
	LevelFromV *float64        `json:"level_from"`
	LevelToV   *float64        `json:"level_to"`
	Result     geom.FitResult  `json:"result"`
	Excluded   []Excluded      `json:"excluded"`
	Selected   []string        `json:"selected"`
	Decision   *store.Decision `json:"decision,omitempty"`
	Lineage    []LineageNode   `json:"lineage"`
	ReqHash    string          `json:"request_hash"`
}

type LineageNode struct {
	AnalysisID string `json:"analysis_id"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id,omitempty"`
	Frame      string `json:"frame"`
	Note       string `json:"note"`
	CreatedAt  string `json:"created_at"`
}

// LoadSteps transforms all raw levels of a specimen into the target frame.
func (s *Service) LoadSteps(ctx context.Context, specimenID, frame string) ([]TransformedStep, geom.Mount, geom.Bedding, error) {
	sp, err := s.st.GetSpecimen(ctx, specimenID)
	if err != nil {
		return nil, geom.Mount{}, geom.Bedding{}, err
	}
	mount := geom.Mount{TrendDeg: sp.MountTrend, PlungeDeg: sp.MountPlunge}
	bedding := geom.Bedding{DipAzimuthDeg: sp.BedDipAz, DipAngleDeg: sp.BedDip}
	chain, R, err := geom.Chain(mount, bedding, frame)
	if err != nil {
		return nil, mount, bedding, err
	}
	raw, err := s.st.ListSteps(ctx, specimenID)
	if err != nil {
		return nil, mount, bedding, err
	}
	out := make([]TransformedStep, 0, len(raw))
	for _, st := range raw {
		vM := geom.Vec3{X: st.X, Y: st.Y, Z: st.Z}
		cM := geom.Sym3{XX: st.CovXX, YY: st.CovYY, ZZ: st.CovZZ,
			XY: st.CovXY, YZ: st.CovYZ, XZ: st.CovXZ}
		vF := R.MulV(vM)
		cF := geom.RotateSym(R, cM)
		_, rt := geom.RoundTripError(R, vM)
		out = append(out, TransformedStep{
			Key: st.StepKey, Treatment: st.Treatment, Level: st.Level, Rep: st.Rep,
			Frame: frame, V: vF, Norm: vF.Norm(), Cov: cF, Chain: cloneChain(chain),
			RoundTripErr: rt,
		})
	}
	return out, mount, bedding, nil
}

func cloneChain(c []geom.RotStep) []geom.RotStep {
	if c == nil {
		return []geom.RotStep{}
	}
	return append([]geom.RotStep{}, c...)
}

// SelectWindow partitions levels into included ordered points and explainable
// exclusions. Replicate rows are never silently merged: every key appears in
// exactly one list.
func SelectWindow(levels []TransformedStep, w WindowSpec) ([]geom.Point, []Excluded) {
	want := map[string]bool{}
	for _, k := range w.Keys {
		want[k] = true
	}
	explicit := len(want) > 0

	var pts []geom.Point
	var excluded []Excluded
	addExcluded := func(l TransformedStep, reason string) {
		excluded = append(excluded, Excluded{Key: l.Key, Reason: reason})
	}

	for _, l := range levels {
		if explicit {
			if want[l.Key] {
				pts = append(pts, toPoint(l))
			} else {
				addExcluded(l, "outside explicit key selection")
			}
			continue
		}
		if l.Treatment != w.Treatment {
			addExcluded(l, fmt.Sprintf("treatment %q outside window %q", l.Treatment, w.Treatment))
			continue
		}
		if w.LevelFrom != nil && l.Level < *w.LevelFrom {
			addExcluded(l, fmt.Sprintf("level %g below window start %g", l.Level, *w.LevelFrom))
			continue
		}
		if w.LevelTo != nil && l.Level > *w.LevelTo {
			addExcluded(l, fmt.Sprintf("level %g above window end %g", l.Level, *w.LevelTo))
			continue
		}
		pts = append(pts, toPoint(l))
	}
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].Level != pts[j].Level {
			return pts[i].Level < pts[j].Level
		}
		return pts[i].Rep < pts[j].Rep
	})
	return pts, excluded
}

func toPoint(l TransformedStep) geom.Point {
	return geom.Point{
		StepKey: l.Key, Treatment: l.Treatment, Level: l.Level,
		Rep: l.Rep, V: l.V, Cov: l.Cov,
	}
}

// RequestHash canonicalizes the fit request so identical inputs can be traced.
func RequestHash(req CreateAnalysisRequest) string {
	b, _ := json.Marshal(struct {
		SpecimenID string     `json:"specimen_id"`
		Frame      string     `json:"frame"`
		Window     WindowSpec `json:"window"`
		Anchored   bool       `json:"anchored"`
		Weighting  string     `json:"weighting"`
		Seed       int64      `json:"seed"`
	}{req.SpecimenID, req.Frame, req.Window, req.OriginAnchored, req.Weighting, req.BootstrapSeed})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12])
}
