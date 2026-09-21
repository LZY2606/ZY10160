package api

import (
	"encoding/json"
	"net/http"

	"polaritytrace/internal/ingest"
)

type stepDTO struct {
	Key       string     `json:"key"`
	Treatment string     `json:"treatment"`
	Level     float64    `json:"level"`
	Rep       int        `json:"rep"`
	Frame     string     `json:"frame"`
	V         [3]float64 `json:"v"`
	Norm      float64    `json:"norm_ma_m"`
	Cov       [6]float64 `json:"cov"`
	RoundTrip float64    `json:"roundtrip_max_abs_err"`
}

type specimenDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Mount       any       `json:"mount"`
	Bedding     any       `json:"bedding"`
	Steps       []stepDTO `json:"steps"`
}

func toDTO(id, name, desc string, mount, bedding any, levels []ingest.TransformedStep) specimenDTO {
	dto := specimenDTO{ID: id, Name: name, Description: desc, Mount: mount, Bedding: bedding}
	for _, l := range levels {
		dto.Steps = append(dto.Steps, stepDTO{
			Key: l.Key, Treatment: l.Treatment, Level: l.Level, Rep: l.Rep,
			Frame: l.Frame,
			V:     [3]float64{l.V.X, l.V.Y, l.V.Z}, Norm: l.Norm,
			Cov:       [6]float64{l.Cov.XX, l.Cov.YY, l.Cov.ZZ, l.Cov.XY, l.Cov.YZ, l.Cov.XZ},
			RoundTrip: l.RoundTripErr,
		})
	}
	return dto
}

func (s *Server) listSpecimens(w http.ResponseWriter, r *http.Request) {
	sps, err := s.st.ListSpecimens(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(sps))
	for _, sp := range sps {
		n, _ := s.countSteps(r, sp.ID)
		out = append(out, map[string]any{
			"id": sp.ID, "name": sp.Name, "description": sp.Description,
			"mount":   map[string]float64{"trend_deg": sp.MountTrend, "plunge_deg": sp.MountPlunge},
			"bedding": map[string]float64{"dip_azimuth_deg": sp.BedDipAz, "dip_angle_deg": sp.BedDip},
			"steps":   n,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) countSteps(r *http.Request, id string) (int, error) {
	rows, _, _, err := s.svc.LoadSteps(r.Context(), id, s.activeFrame(r))
	return len(rows), err
}

func (s *Server) getSpecimen(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sp, err := s.st.GetSpecimen(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	levels, _, _, err := s.svc.LoadSteps(r.Context(), id, s.activeFrame(r))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	dto := toDTO(sp.ID, sp.Name, sp.Description,
		map[string]float64{"trend_deg": sp.MountTrend, "plunge_deg": sp.MountPlunge},
		map[string]float64{"dip_azimuth_deg": sp.BedDipAz, "dip_angle_deg": sp.BedDip},
		levels)
	writeJSON(w, 200, dto)
}

func (s *Server) createAnalysis(w http.ResponseWriter, r *http.Request) {
	var req ingest.CreateAnalysisRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.SpecimenID == "" {
		writeErr(w, 400, "specimen_id is required")
		return
	}
	view, _, err := s.svc.CreateAnalysis(r.Context(), req)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, view)
}

func (s *Server) listAnalyses(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("specimen_id")
	if id == "" {
		writeErr(w, 400, "specimen_id is required")
		return
	}
	views, err := s.svc.ListAnalyses(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, views)
}

func (s *Server) getAnalysis(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.GetAnalysis(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) deleteAnalysis(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteAnalysis(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

func (s *Server) putDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SpecimenID string `json:"specimen_id"`
		AnalysisID string `json:"analysis_id"`
		Action     string `json:"action"`
		Rationale  string `json:"rationale"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	d, err := s.svc.SetDecision(r.Context(), body.SpecimenID, body.AnalysisID, body.Action, body.Rationale)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) rotations(w http.ResponseWriter, r *http.Request) {
	exp, err := s.svc.RotationExport(r.Context(), r.PathValue("id"), s.activeFrame(r))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, exp)
}

func (s *Server) runlog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.st.ListRunLog(r.Context(), 0)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, entries)
}

func (s *Server) exportFixtures(w http.ResponseWriter, r *http.Request) {
	sps, err := s.st.ListSpecimens(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type exported struct {
		specimenDTO
		Analyses []any `json:"analyses"`
	}
	var out []exported
	for _, sp := range sps {
		levels, _, _, _ := s.svc.LoadSteps(r.Context(), sp.ID, s.activeFrame(r))
		views, _ := s.svc.ListAnalyses(r.Context(), sp.ID)
		out = append(out, exported{
			specimenDTO: toDTO(sp.ID, sp.Name, sp.Description,
				map[string]float64{"trend_deg": sp.MountTrend, "plunge_deg": sp.MountPlunge},
				map[string]float64{"dip_azimuth_deg": sp.BedDipAz, "dip_angle_deg": sp.BedDip},
				levels),
			Analyses: toAny(views),
		})
	}
	writeJSON(w, 200, map[string]any{"frame": s.activeFrame(r), "specimens": out})
}

func toAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i := range xs {
		out[i] = xs[i]
	}
	return out
}

var _ = json.Marshal
