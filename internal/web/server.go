package web

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"paleobench/internal/geomag"
)

// Server holds HTTP dependencies.
type Server struct {
	Svc *Service
	Web fs.FS
}

// NewMux wires every route.
func (s *Server) NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/project", s.handleProject)
	mux.HandleFunc("POST /api/frame", s.handleSetFrame)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("POST /api/reset", s.handleReset)
	mux.HandleFunc("GET /api/specimen/{id}", s.handleSpecimen)
	mux.HandleFunc("POST /api/specimen/{id}/candidates", s.handleCreateCandidate)
	mux.HandleFunc("POST /api/candidate/{id}/refit", s.handleRefit)
	mux.HandleFunc("POST /api/candidate/{id}/decision", s.handleDecision)
	mux.HandleFunc("GET /api/candidate/{id}/chain", s.handleChain)
	mux.HandleFunc("GET /api/candidate/{id}/versions", s.handleVersions)
	mux.HandleFunc("GET /api/runlog", s.handleRunLog)
	mux.HandleFunc("GET /api/specimen/{id}/plot/zijderveld.svg", s.handleZijdSVG)
	mux.HandleFunc("GET /api/specimen/{id}/plot/stereonet.svg", s.handleStereoSVG)
	mux.HandleFunc("GET /api/candidate/{id}/plot/zijderveld.svg", s.handleCandidateZijdSVG)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.Web, "index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.Svc.EnsureProject(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	specs, err := s.Svc.Store.ListSpecimens(ctx, p.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"project": p, "specimens": specs})
}

func (s *Server) handleSetFrame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Frame string `json:"frame"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Frame != geomag.FrameSpec && req.Frame != geomag.FrameGeo && req.Frame != geomag.FrameTilt {
		writeErr(w, 400, "frame must be spec|geo|tilt")
		return
	}
	p, err := s.Svc.EnsureProject(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := s.Svc.Store.SetProjectFrame(r.Context(), p.ID, req.Frame); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.Svc.Store.Log(r.Context(), "researcher", "frame.set", req.Frame)
	writeJSON(w, map[string]string{"frame": req.Frame})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	n, err := s.Svc.ImportFixtures(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"imported_levels": n})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.Store.WipeAll(r.Context()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "wiped"})
}

func (s *Server) handleSpecimen(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	data, err := s.Svc.LoadSpecimen(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, data)
}

type createCandidateReq struct {
	Label            string `json:"label"`
	Frame            string `json:"frame"`
	FromSeq          int    `json:"from_seq"`
	ToSeq            int    `json:"to_seq"`
	Origin           string `json:"origin"`
	Weighting        string `json:"weighting"`
	ManualExclude    []int  `json:"manual_exclude"`
	BootstrapSeed    uint64 `json:"bootstrap_seed"`
	BootstrapRepeats int    `json:"bootstrap_repeats"`
}

func (s *Server) handleCreateCandidate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req createCandidateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Origin == "" {
		req.Origin = string(geomag.OriginFree)
	}
	if req.Weighting == "" {
		req.Weighting = string(geomag.WeightNone)
	}
	if req.Origin != string(geomag.OriginFree) && req.Origin != string(geomag.OriginAnchored) {
		writeErr(w, 400, "origin must be free|anchored")
		return
	}
	if req.Weighting != string(geomag.WeightNone) &&
		req.Weighting != string(geomag.WeightIsotropic) &&
		req.Weighting != string(geomag.WeightMahalanobis) {
		writeErr(w, 400, "weighting must be none|isotropic|mahalanobis")
		return
	}
	if req.BootstrapSeed == 0 {
		req.BootstrapSeed = 1164
	}
	spec := geomag.WindowSpec{
		Frame: req.Frame, FromSeq: req.FromSeq, ToSeq: req.ToSeq,
		Origin: geomag.Origin(req.Origin), Weighting: geomag.Weighting(req.Weighting),
		ManualExclude:    req.ManualExclude,
		BootstrapSeed:    req.BootstrapSeed,
		BootstrapRepeats: req.BootstrapRepeats,
	}
	cv, err := s.Svc.CreateAndFitCandidate(r.Context(), id, spec, req.Label)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, cv)
}

func (s *Server) handleRefit(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.Svc.Refit(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"refitted": id})
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Accepted  bool   `json:"accepted"`
		Rationale string `json:"rationale"`
		Reviewer  string `json:"reviewer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.Svc.Store.SetDecision(r.Context(), id, req.Accepted, req.Rationale, req.Reviewer); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	s.Svc.Store.Log(r.Context(), req.Reviewer, "decision",
		fmt.Sprintf("candidate=%d accepted=%v", id, req.Accepted))
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	ch, err := s.Svc.CandidateChain(r.Context(), id, version)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="candidate-%d-rotation-matrices.json"`, id))
	}
	_, _ = w.Write(ch)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	recs, err := s.Svc.Store.ListAnalyses(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, recs)
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Svc.Store.RunLog(r.Context(), 2000)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="run-log.json"`)
	}
	writeJSON(w, entries)
}

// svgEscape is a tiny helper for element text.
func svgEscape(in string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(in))
	return b.String()
}
