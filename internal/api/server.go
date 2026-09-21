// Package api exposes the offline workbench over a local HTTP interface.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"polaritytrace/internal/ingest"
	"polaritytrace/internal/store"
)

type Server struct {
	svc    *ingest.Service
	st     *store.Store
	static fs.FS
}

func New(svc *ingest.Service, static fs.FS) *Server {
	return &Server{svc: svc, st: svc.Store(), static: static}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/project", s.getProject)
	mux.HandleFunc("PUT /api/project/frame", s.setFrame)
	mux.HandleFunc("GET /api/specimens", s.listSpecimens)
	mux.HandleFunc("GET /api/specimens/{id}", s.getSpecimen)
	mux.HandleFunc("POST /api/admin/reseed", s.reseed)
	mux.HandleFunc("POST /api/admin/reset", s.resetDB)
	mux.HandleFunc("POST /api/analyses", s.createAnalysis)
	mux.HandleFunc("GET /api/analyses", s.listAnalyses)
	mux.HandleFunc("GET /api/analyses/{id}", s.getAnalysis)
	mux.HandleFunc("DELETE /api/analyses/{id}", s.deleteAnalysis)
	mux.HandleFunc("PUT /api/decisions", s.putDecision)
	mux.HandleFunc("GET /api/specimens/{id}/rotations", s.rotations)
	mux.HandleFunc("GET /api/runlog", s.runlog)
	mux.HandleFunc("GET /api/fixtures", s.exportFixtures)
	mux.Handle("/", s.staticHandler())
	return s.logging(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProject(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) setFrame(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Frame string `json:"frame"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	frame := strings.ToUpper(strings.TrimSpace(body.Frame))
	if frame != "M" && frame != "G" && frame != "T" {
		writeErr(w, 400, "frame must be one of M, G, T")
		return
	}
	p, err := s.st.SetFrame(r.Context(), frame)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) reseed(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.SeedFixtures(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"imported": n})
}

func (s *Server) resetDB(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ResetAll(r.Context()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"reset": true})
}
