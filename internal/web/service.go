package web

import (
	"context"
	"encoding/json"
	"fmt"

	"paleobench/internal/fixtures"
	"paleobench/internal/geomag"
	"paleobench/internal/store"
)

// Service orchestrates storage and the geomag analysis kernel.
type Service struct {
	Store *store.Store
}

func NewService(st *store.Store) *Service { return &Service{Store: st} }

const projectName = "极性轨迹"

// EnsureProject returns the demo project, creating it if absent.
func (s *Service) EnsureProject(ctx context.Context) (*store.Project, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	if p != nil {
		return p, nil
	}
	id, err := s.Store.CreateProject(ctx, projectName, geomag.FrameSpec)
	if err != nil {
		return nil, err
	}
	s.Store.Log(ctx, "system", "project.create", projectName)
	return s.Store.GetProject(ctx, id)
}

// ImportFixtures loads (or re-loads) the fixed acceptance data.  Existing
// specimens with the same code are replaced idempotently.
func (s *Service) ImportFixtures(ctx context.Context) (int, error) {
	p, err := s.EnsureProject(ctx)
	if err != nil {
		return 0, err
	}
	docs, err := fixtures.Load()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, f := range docs {
		var bed *geomag.Bedding
		var strike, dip *float64
		if f.Bedding != nil {
			b := geomag.Bedding{Strike: f.Bedding.Strike, Dip: f.Bedding.Dip}
			bed = &b
			st, dp := f.Bedding.Strike, f.Bedding.Dip
			strike, dip = &st, &dp
		}
		sp := store.Specimen{
			ProjectID: p.ID, Code: f.Code, Name: f.Name,
			Azimuth: f.Azimuth, Plunge: f.Plunge, Roll: f.Roll,
			Strike: strike, Dip: dip,
		}
		steps := make([]store.Step, 0, len(f.Steps))
		for _, fs := range f.Steps {
			cov := [3][3]float64{
				{fs.SigmaXX, fs.SigmaXY, 0},
				{fs.SigmaXY, fs.SigmaYY, 0},
				{0, 0, fs.SigmaZZ},
			}
			steps = append(steps, store.Step{
				SpecimenID: 0, Seq: fs.Seq, Kind: fs.Kind, Level: fs.Level,
				X: fs.X, Y: fs.Y, Z: fs.Z, Cov: cov,
				Azimuth: f.Azimuth, Plunge: f.Plunge, Roll: f.Roll, Note: fs.Note,
			})
		}
		_, n, err := s.Store.UpsertSpecimen(ctx, p.ID, sp, steps, "fixtures.json")
		if err != nil {
			return total, fmt.Errorf("import %s: %w", f.Code, err)
		}
		_ = bed
		total += n
	}
	s.Store.Log(ctx, "system", "fixtures.import",
		fmt.Sprintf("%d specimens, %d levels", len(docs), total))
	return total, nil
}

// SpecimenData is the full analysis payload for the UI.
type SpecimenData struct {
	Project    *store.Project       `json:"project"`
	Specimen   store.Specimen       `json:"specimen"`
	Steps      []geomag.Step        `json:"steps"`
	Views      []geomag.StepView    `json:"views"`
	ZijH       []geomag.ZijdPoint   `json:"zij_h"`
	ZijV       []geomag.ZijdPoint   `json:"zij_v"`
	Stereo     []geomag.StereoPoint `json:"stereo"`
	Candidates []CandidateView      `json:"candidates"`
	Bedding    *geomag.Bedding      `json:"bedding"`
	Frame      string               `json:"frame"`
}

// CandidateView bundles the candidate with its latest computed version.
type CandidateView struct {
	Candidate store.Candidate    `json:"candidate"`
	Result    *geomag.FitResult  `json:"result,omitempty"`
	Version   int                `json:"version"`
	Spec      *geomag.WindowSpec `json:"spec,omitempty"`
}

func toGeomagStep(st store.Step) geomag.Step {
	return geomag.Step{
		ID: st.ID, SpecimenID: st.SpecimenID, Seq: st.Seq,
		Treatment: geomag.Treatment{Kind: st.Kind, Level: st.Level},
		Vector:    geomag.Vec3{X: st.X, Y: st.Y, Z: st.Z},
		Cov:       st.Cov,
		Attitude:  geomag.Attitude{Azimuth: st.Azimuth, Plunge: st.Plunge, Roll: st.Roll},
		Note:      st.Note,
	}
}

// LoadSpecimen assembles the specimen payload in the project's active frame.
func (s *Service) LoadSpecimen(ctx context.Context, specimenID int64) (*SpecimenData, error) {
	sp, err := s.Store.GetSpecimen(ctx, specimenID)
	if err != nil || sp == nil {
		return nil, fmt.Errorf("specimen: %w", err)
	}
	p, err := s.Store.GetProject(ctx, sp.ProjectID)
	if err != nil {
		return nil, err
	}
	dbSteps, err := s.Store.ListSteps(ctx, specimenID)
	if err != nil {
		return nil, err
	}
	gsteps := make([]geomag.Step, 0, len(dbSteps))
	for _, st := range dbSteps {
		gsteps = append(gsteps, toGeomagStep(st))
	}
	var bed *geomag.Bedding
	if sp.Strike != nil && sp.Dip != nil {
		b := geomag.Bedding{Strike: *sp.Strike, Dip: *sp.Dip}
		bed = &b
	}
	frame := p.Frame
	if frame == "" {
		frame = geomag.FrameSpec
	}
	views := geomag.TransformSteps(gsteps, bed, frame)
	zh, zv := geomag.Zijderveld(views)
	stereo := geomag.Stereonet(views)

	cands, err := s.Store.ListCandidates(ctx, specimenID)
	if err != nil {
		return nil, err
	}
	cviews := make([]CandidateView, 0, len(cands))
	for _, c := range cands {
		cv := CandidateView{Candidate: c, Version: c.LatestVersion}
		if c.LatestVersion > 0 {
			recs, err := s.Store.ListAnalyses(ctx, c.ID)
			if err == nil && len(recs) > 0 {
				var res geomag.FitResult
				var spec geomag.WindowSpec
				_ = json.Unmarshal(recs[0].Result, &res)
				_ = json.Unmarshal(recs[0].Spec, &spec)
				cv.Result = &res
				cv.Spec = &spec
			}
		}
		cviews = append(cviews, cv)
	}

	return &SpecimenData{
		Project: p, Specimen: *sp, Steps: gsteps, Views: views,
		ZijH: zh, ZijV: zv, Stereo: stereo, Candidates: cviews,
		Bedding: bed, Frame: frame,
	}, nil
}

// CreateAndFitCandidate persists a candidate and computes its first analysis
// version, returning the assembled candidate view.
func (s *Service) CreateAndFitCandidate(ctx context.Context, specimenID int64,
	spec geomag.WindowSpec, label string) (*CandidateView, error) {
	data, err := s.LoadSpecimen(ctx, specimenID)
	if err != nil {
		return nil, err
	}
	if spec.Frame == "" {
		spec.Frame = data.Frame
	}
	if spec.FromSeq == 0 && spec.ToSeq == 0 && len(data.Views) > 0 {
		spec.FromSeq = data.Views[0].Seq
		spec.ToSeq = data.Views[len(data.Views)-1].Seq
	}
	c := store.Candidate{
		SpecimenID: specimenID, Label: label, Frame: spec.Frame,
		FromSeq: spec.FromSeq, ToSeq: spec.ToSeq,
		Origin: string(spec.Origin), Weighting: string(spec.Weighting),
		ManualExclude:    spec.ManualExclude,
		BootstrapSeed:    spec.BootstrapSeed,
		BootstrapRepeats: spec.BootstrapRepeats,
	}
	if c.BootstrapRepeats == 0 {
		c.BootstrapRepeats = 2000
	}
	id, err := s.Store.CreateCandidate(ctx, c)
	if err != nil {
		return nil, err
	}
	s.Store.Log(ctx, "researcher", "candidate.create",
		fmt.Sprintf("specimen=%d candidate=%d label=%q", specimenID, id, label))
	if err := s.Refit(ctx, id); err != nil {
		return nil, err
	}
	fresh, err := s.Store.GetCandidate(ctx, id)
	if err != nil {
		return nil, err
	}
	cv := &CandidateView{Candidate: *fresh}
	recs, err := s.Store.ListAnalyses(ctx, id)
	if err == nil && len(recs) > 0 {
		var res geomag.FitResult
		var ws geomag.WindowSpec
		_ = json.Unmarshal(recs[0].Result, &res)
		_ = json.Unmarshal(recs[0].Spec, &ws)
		cv.Result = &res
		cv.Spec = &ws
		cv.Version = recs[0].Version
	}
	return cv, nil
}

// Refit recomputes the candidate, appending a new immutable analysis version.
func (s *Service) Refit(ctx context.Context, candidateID int64) error {
	c, err := s.Store.GetCandidate(ctx, candidateID)
	if err != nil || c == nil {
		return fmt.Errorf("candidate: %w", err)
	}
	data, err := s.LoadSpecimen(ctx, c.SpecimenID)
	if err != nil {
		return err
	}
	// Candidates were computed in a fixed frame snapshot; recompute in the
	// candidate's own frame so restoring a project reproduces them exactly.
	gsteps := data.Steps
	views := geomag.TransformSteps(gsteps, data.Bedding, c.Frame)
	spec := geomag.WindowSpec{
		Frame: c.Frame, FromSeq: c.FromSeq, ToSeq: c.ToSeq,
		Origin: geomag.Origin(c.Origin), Weighting: geomag.Weighting(c.Weighting),
		ManualExclude:    c.ManualExclude,
		BootstrapSeed:    c.BootstrapSeed,
		BootstrapRepeats: c.BootstrapRepeats,
	}
	result := geomag.Fit(views, spec)

	// Per-step transform chain for traceability.
	chains := map[int]geomag.TransformChain{}
	for _, v := range views {
		chains[v.Seq] = v.Chain
	}
	chainJSON, _ := json.Marshal(chains)
	specJSON, _ := json.Marshal(spec)
	resJSON, _ := json.Marshal(result)
	ver, err := s.Store.AddAnalysis(ctx, candidateID, specJSON, resJSON, chainJSON)
	if err != nil {
		return err
	}
	s.Store.Log(ctx, "system", "analysis.compute",
		fmt.Sprintf("candidate=%d version=%d frame=%s MAD=%.3f",
			candidateID, ver, c.Frame, result.MAD))
	return nil
}

// CandidateChain returns the stored per-seq rotation-chain document.
func (s *Service) CandidateChain(ctx context.Context, candidateID int64, version int) (json.RawMessage, error) {
	recs, err := s.Store.ListAnalyses(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if r.Version == version || version == 0 {
			return r.Chain, nil
		}
	}
	return nil, fmt.Errorf("version %d not found", version)
}
