package api

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"polaritytrace/internal/store"
)

func (s *Server) activeFrame(r *http.Request) string {
	if f := strings.ToUpper(r.URL.Query().Get("frame")); f == "M" || f == "G" || f == "T" {
		return f
	}
	if p, err := s.st.GetProject(r.Context()); err == nil && p.Frame != "" {
		return p.Frame
	}
	return "G"
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: 200}
		var hash sql.NullString
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Body != nil && r.Method != "GET" {
			b, err := io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(b))
				if len(b) > 0 {
					h := sha256.Sum256(b)
					hash = sql.NullString{String: hex.EncodeToString(h[:12]), Valid: true}
				}
			}
		}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.st.LogRun(r.Context(), store.RunEntry{
				Method: r.Method, Path: r.URL.RequestURI(), Status: sw.status,
				RequestHash: hash,
			})
		}
	})
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(s.static, ".")
	if err != nil {
		panic(err)
	}
	fileSrv := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			f, err := sub.Open("index.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", info.ModTime(), f.(io.ReadSeeker))
			return
		}
		fileSrv.ServeHTTP(w, r)
	})
}
