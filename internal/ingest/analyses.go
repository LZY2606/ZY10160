package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"polaritytrace/internal/geom"
	"polaritytrace/internal/store"
)

func canonicalFrame(f string) string {
	switch f {
	case "M", "G", "T":
		return f
	case "":
		return "G"
	default:
		return "G"
	}
}

// CreateAnalysis computes a fit and appends it as a new immutable version.
func (s *Service) CreateAnalysis(ctx context.Context, req CreateAnalysisRequest) (AnalysisView, []TransformedStep, error) {
	req.Frame = canonicalFrame(req.Frame)
	if req.Weighting == "" {
		req.Weighting = string(geom.WeightUniform)
	}
	switch geom.Weighting(req.Weighting) {
	case geom.WeightUniform, geom.WeightInverseTrace:
	default:
		return AnalysisView{}, nil, fmt.Errorf("unknown weighting %q", req.Weighting)
	}

	levels, _, _, err := s.LoadSteps(ctx, req.SpecimenID, req.Frame)
	if err != nil {
		return AnalysisView{}, nil, err
	}
	pts, excluded := SelectWindow(levels, req.Window)

	fit := geom.Fit(pts, geom.FitConfig{
		OriginAnchored: req.OriginAnchored,
		Weighting:      geom.Weighting(req.Weighting),
		BootstrapSeed:  req.BootstrapSeed,
	})

	version, err := s.st.NextAnalysisVersion(ctx, req.SpecimenID)
	if err != nil {
		return AnalysisView{}, nil, err
	}
	id := fmt.Sprintf("an-%s-v%d", shortHash(req.SpecimenID), version)
	selected := make([]string, 0, len(pts))
	for _, p := range pts {
		selected = append(selected, p.StepKey)
	}
	selJSON, _ := json.Marshal(selected)
	excJSON, _ := json.Marshal(excluded)
	resJSON, err := json.Marshal(fit)
	if err != nil {
		return AnalysisView{}, nil, err
	}

	// Range bounds are persisted from the actual selected points so that a
	// reopened project reconstructs the same continuous window even when the
	// request expressed only an open bound or an explicit key list.
	var from, to *float64
	if len(pts) > 0 {
		lo, hi := pts[0].Level, pts[len(pts)-1].Level
		from, to = &lo, &hi
	}
	rec := store.Analysis{
		ID: id, SpecimenID: req.SpecimenID, Version: version,
		Frame: req.Frame, Treatment: req.Window.Treatment,
		Anchored: req.OriginAnchored, Weighting: req.Weighting,
		BootstrapSeed: req.BootstrapSeed,
		SelectedKeys:  string(selJSON), ExcludedKeys: string(excJSON),
		ResultJSON: string(resJSON), Note: req.Note,
		CreatedBy: "researcher",
	}
	if from != nil {
		rec.LevelFrom = sqlNullFloat(*from, true)
	}
	if to != nil {
		rec.LevelTo = sqlNullFloat(*to, true)
	}
	if req.ParentID != "" {
		rec.ParentID = sqlNullString(req.ParentID, true)
	}
	if err := s.st.InsertAnalysis(ctx, rec); err != nil {
		return AnalysisView{}, nil, err
	}

	view, err := s.buildView(ctx, rec)
	if err != nil {
		return AnalysisView{}, nil, err
	}
	return view, levels, nil
}

func (s *Service) buildView(ctx context.Context, rec store.Analysis) (AnalysisView, error) {
	var fit geom.FitResult
	if err := json.Unmarshal([]byte(rec.ResultJSON), &fit); err != nil {
		return AnalysisView{}, err
	}
	var selected []string
	if err := json.Unmarshal([]byte(rec.SelectedKeys), &selected); err != nil {
		return AnalysisView{}, err
	}
	var excluded []Excluded
	if err := json.Unmarshal([]byte(rec.ExcludedKeys), &excluded); err != nil {
		return AnalysisView{}, err
	}
	decisions, err := s.st.ListDecisions(ctx, rec.SpecimenID)
	if err != nil {
		return AnalysisView{}, err
	}
	view := AnalysisView{
		Analysis: rec, Result: fit, Excluded: excluded, Selected: selected,
		ParentIDV:  nullStringPtr(rec.ParentID),
		LevelFromV: nullFloatPtr(rec.LevelFrom), LevelToV: nullFloatPtr(rec.LevelTo),
	}
	for i := range decisions {
		if decisions[i].AnalysisID == rec.ID {
			d := decisions[i]
			view.Decision = &d
		}
	}
	lineage, err := s.lineage(ctx, rec.SpecimenID)
	if err != nil {
		return AnalysisView{}, err
	}
	view.Lineage = lineage
	view.ReqHash = RequestHash(CreateAnalysisRequest{
		SpecimenID: rec.SpecimenID, Frame: rec.Frame,
		Window: WindowSpec{Treatment: rec.Treatment,
			LevelFrom: nullFloatPtr(rec.LevelFrom), LevelTo: nullFloatPtr(rec.LevelTo)},
		OriginAnchored: rec.Anchored, Weighting: rec.Weighting,
		BootstrapSeed: rec.BootstrapSeed,
	})
	return view, nil
}

func (s *Service) lineage(ctx context.Context, specimenID string) ([]LineageNode, error) {
	all, err := s.st.ListAnalyses(ctx, specimenID)
	if err != nil {
		return nil, err
	}
	out := make([]LineageNode, 0, len(all))
	for _, a := range all {
		n := LineageNode{AnalysisID: a.ID, Version: a.Version,
			Frame: a.Frame, Note: a.Note, CreatedAt: a.CreatedAt}
		if a.ParentID.Valid {
			n.ParentID = a.ParentID.String
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) GetAnalysis(ctx context.Context, id string) (AnalysisView, error) {
	rec, err := s.st.GetAnalysis(ctx, id)
	if err != nil {
		return AnalysisView{}, err
	}
	return s.buildView(ctx, rec)
}

func (s *Service) ListAnalyses(ctx context.Context, specimenID string) ([]AnalysisView, error) {
	recs, err := s.st.ListAnalyses(ctx, specimenID)
	if err != nil {
		return nil, err
	}
	out := make([]AnalysisView, 0, len(recs))
	for _, rec := range recs {
		v, err := s.buildView(ctx, rec)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// SetDecision attaches/updates the human layer for a specimen. Every decision
// is itself versioned independently of the computation layer.
func (s *Service) SetDecision(ctx context.Context, specimenID, analysisID, action, rationale string) (store.Decision, error) {
	if _, err := s.st.GetAnalysis(ctx, analysisID); err != nil {
		return store.Decision{}, err
	}
	switch action {
	case "candidate", "accepted", "rejected":
	default:
		return store.Decision{}, fmt.Errorf("unknown decision action %q", action)
	}
	v, err := s.st.NextDecisionVersion(ctx, specimenID)
	if err != nil {
		return store.Decision{}, err
	}
	d := store.Decision{
		ID:         fmt.Sprintf("dec-%s-v%d", shortHash(specimenID), v),
		SpecimenID: specimenID, AnalysisID: analysisID, Version: v,
		Action: action, Rationale: rationale, Author: "researcher",
	}
	if err := s.st.UpsertDecision(ctx, d); err != nil {
		return store.Decision{}, err
	}
	return d, nil
}

// RotationExport documents every link of the M->frame chain as matrices.
type RotationExport struct {
	SpecimenID  string         `json:"specimen_id"`
	Frame       string         `json:"frame"`
	GeneratedAt string         `json:"generated_at"`
	Convention  string         `json:"convention"`
	Steps       []geom.RotStep `json:"steps"`
	Composed    geom.Mat3      `json:"composed"`
}

func (s *Service) RotationExport(ctx context.Context, specimenID, frame string) (RotationExport, error) {
	sp, err := s.st.GetSpecimen(ctx, specimenID)
	if err != nil {
		return RotationExport{}, err
	}
	mount := geom.Mount{TrendDeg: sp.MountTrend, PlungeDeg: sp.MountPlunge}
	bedding := geom.Bedding{DipAzimuthDeg: sp.BedDipAz, DipAngleDeg: sp.BedDip}
	steps, composed, err := geom.Chain(mount, bedding, canonicalFrame(frame))
	if err != nil {
		return RotationExport{}, err
	}
	if steps == nil {
		steps = []geom.RotStep{{Name: "identity", FrameIn: "M", FrameOut: "M", Matrix: geom.Identity()}}
	}
	return RotationExport{
		SpecimenID: specimenID, Frame: canonicalFrame(frame),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Convention:  "right-handed NED; positive angles follow the right-hand rule about the named axis",
		Steps:       steps, Composed: composed,
	}, nil
}
