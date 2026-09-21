package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"paleobench/internal/geomag"
	"paleobench/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := NewService(st)
	n, err := svc.ImportFixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 32 {
		t.Fatalf("expected 32 fixture levels, got %d", n)
	}
	sub, err := fs.Sub(Assets, "webroot")
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Svc: svc, Web: sub}
	return srv, st, srv.NewMux()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Code >= 400 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if len(rec.Body.Bytes()) > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func raw(t *testing.T, h http.Handler, method, path string) (int, []byte, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), rec.Header().Get("Content-Type")
}

func specimenIDByCode(t *testing.T, h http.Handler, code string) int {
	_, proj, _ := raw(t, h, "GET", "/api/project")
	var p struct {
		Specimens []struct {
			ID   int    `json:"id"`
			Code string `json:"code"`
		} `json:"specimens"`
	}
	if err := json.Unmarshal(proj, &p); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Specimens {
		if s.Code == code {
			return s.ID
		}
	}
	t.Fatalf("specimen %s not found", code)
	return 0
}

type candResp struct {
	Candidate struct {
		ID     int    `json:"id"`
		Frame  string `json:"frame"`
		From   int    `json:"from_seq"`
		To     int    `json:"to_seq"`
		Manual []int  `json:"manual_exclude"`
	} `json:"candidate"`
	Result  geomag.FitResult `json:"result"`
	Version int              `json:"version"`
}

func createCandidate(t *testing.T, h http.Handler, specID int, body map[string]any) candResp {
	t.Helper()
	_, out := doJSON(t, h, "POST",
		fmt.Sprintf("/api/specimen/%d/candidates", specID), body)
	b, _ := json.Marshal(out)
	var cr candResp
	if err := json.Unmarshal(b, &cr); err != nil {
		t.Fatalf("candidate decode: %v (%s)", err, string(b))
	}
	return cr
}

func hasDiag(res geomag.FitResult, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestAcceptanceStableSingleComponent(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "stable", "frame": "spec",
		"from_seq": 2, "to_seq": 11, "origin": "free",
		"weighting": "isotropic", "bootstrap_seed": 1164,
	})
	if cr.Result.MAD > 4.0 {
		t.Fatalf("stable trajectory MAD too large: %.2f", cr.Result.MAD)
	}
	if cr.Result.AngularGap95 <= 0 || cr.Result.AngularGap95 > 5 {
		t.Fatalf("unexpected gap95: %.2f", cr.Result.AngularGap95)
	}
	if cr.Result.N != 10 {
		t.Fatalf("expected 10 included levels, got %d", cr.Result.N)
	}
	if len(cr.Result.ExcludedSeq) != 1 || cr.Result.ExcludedSeq[0].Seq != 1 {
		t.Fatalf("seq 1 should be excluded as outside window: %+v", cr.Result.ExcludedSeq)
	}
}

func TestAcceptanceOriginApproach(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "ORIGIN-1")
	anchored := createCandidate(t, h, id, map[string]any{
		"label": "anchored", "frame": "spec",
		"from_seq": 4, "to_seq": 11, "origin": "anchored",
		"weighting": "none", "bootstrap_seed": 7,
	})
	free := createCandidate(t, h, id, map[string]any{
		"label": "free", "frame": "spec",
		"from_seq": 4, "to_seq": 11, "origin": "free",
		"weighting": "none", "bootstrap_seed": 7,
	})
	if anchored.Result.MAD >= free.Result.MAD {
		t.Fatalf("anchored fit (%.3f) should be tighter than free (%.3f) on origin-approaching tail",
			anchored.Result.MAD, free.Result.MAD)
	}
	if anchored.Candidate.ID == free.Candidate.ID {
		t.Fatal("two candidates must be retained side by side with distinct ids")
	}
}

func TestAcceptanceDuplicateAnomalousLoad(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "DUP-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "dup", "frame": "spec",
		"from_seq": 1, "to_seq": 9, "origin": "free",
		"weighting": "mahalanobis", "bootstrap_seed": 9,
	})
	if !hasDiag(cr.Result, "DUPLICATE_LEVEL") {
		t.Fatalf("expected duplicate-level diagnostic: %+v", cr.Result.Diagnostics)
	}
	if !hasDiag(cr.Result, "DUPLICATE_ANOMALOUS_LOAD") {
		t.Fatalf("expected anomalous-load diagnostic: %+v", cr.Result.Diagnostics)
	}
	if cr.Result.N != 9 {
		t.Fatalf("repeated 40 mT runs must stay independent (n=9), got %d", cr.Result.N)
	}
}

func TestManualExcludeRecorded(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "excl", "frame": "spec",
		"from_seq": 1, "to_seq": 11, "origin": "free",
		"weighting": "none", "manual_exclude": []int{3, 7},
	})
	if cr.Result.N != 9 {
		t.Fatalf("expected 9 included after 2 manual excludes, got %d", cr.Result.N)
	}
	got := map[int]string{}
	for _, e := range cr.Result.ExcludedSeq {
		got[e.Seq] = e.Reason
	}
	if got[3] != "manual exclusion" || got[7] != "manual exclusion" {
		t.Fatalf("manual exclusions not recorded: %+v", got)
	}
}

func TestVersioningAppendsImmutableRows(t *testing.T) {
	srv, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "v", "frame": "spec",
		"from_seq": 2, "to_seq": 10, "origin": "free", "weighting": "none",
	})
	if cr.Version != 1 {
		t.Fatalf("first analysis should be version 1, got %d", cr.Version)
	}
	cid := cr.Candidate.ID
	if code, _ := doJSON(t, h, "POST",
		fmt.Sprintf("/api/candidate/%d/refit", cid), nil); code != 200 {
		t.Fatalf("refit status %d", code)
	}
	recs, err := srv.Svc.Store.ListAnalyses(context.Background(), int64(cid))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Version != 2 || recs[1].Version != 1 {
		t.Fatalf("expected versions 2 then 1 (newest first), got %+v", recs)
	}
}

func TestFrameSwitchRestoresCandidateExactly(t *testing.T) {
	_, st, h := newTestServer(t)
	ctx := context.Background()
	id := specimenIDByCode(t, h, "STABLE-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "geo-window", "frame": "geo",
		"from_seq": 3, "to_seq": 10, "origin": "free",
		"weighting": "isotropic", "manual_exclude": []int{5},
		"bootstrap_seed": 42,
	})
	if code, out := doJSON(t, h, "POST", fmt.Sprintf("/api/candidate/%d/decision", cr.Candidate.ID), map[string]any{
		"accepted": true, "rationale": "稳定", "reviewer": "rev",
	}); code != 200 {
		t.Fatalf("decision: %d %v", code, out)
	}
	// Switch the *project* frame away from the candidate frame.
	if code, out := doJSON(t, h, "POST", "/api/frame", map[string]any{"frame": "tilt"}); code != 200 {
		t.Fatalf("frame switch: %d %v", code, out)
	}

	// Reload from the same database (simulates reopening the project).
	svc2 := NewService(st)
	sub, _ := fs.Sub(Assets, "webroot")
	srv2 := &Server{Svc: svc2, Web: sub}
	h2 := srv2.NewMux()
	code, body, _ := raw(t, h2, "GET", fmt.Sprintf("/api/specimen/%d", id))
	if code != 200 {
		t.Fatalf("reload specimen %d", code)
	}
	var data SpecimenData
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	if data.Frame != "tilt" {
		t.Fatalf("project frame should persist as tilt, got %s", data.Frame)
	}
	var cv *CandidateView
	for i := range data.Candidates {
		if data.Candidates[i].Candidate.ID == int64(cr.Candidate.ID) {
			cv = &data.Candidates[i]
		}
	}
	if cv == nil {
		t.Fatal("candidate disappeared after reopen")
	}
	if cv.Candidate.Frame != "geo" || cv.Candidate.FromSeq != 3 ||
		cv.Candidate.ToSeq != 10 || !cv.Candidate.Accepted || cv.Candidate.Rationale != "稳定" {
		t.Fatalf("candidate params not restored: %+v", cv.Candidate)
	}
	if len(cv.Candidate.ManualExclude) != 1 || cv.Candidate.ManualExclude[0] != 5 {
		t.Fatalf("rejected level not restored: %v", cv.Candidate.ManualExclude)
	}
	r2 := cv.Result
	if r2 == nil {
		t.Fatal("computed result missing after reopen")
	}
	if !fuzzyVec(r2.Direction, cr.Result.Direction, 1e-12) {
		t.Fatalf("direction changed after reopen: %v vs %v", r2.Direction, cr.Result.Direction)
	}
	if diff := r2.MAD - cr.Result.MAD; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("MAD changed after reopen: %.15f vs %.15f", r2.MAD, cr.Result.MAD)
	}
	if r2.AngularGap95 != cr.Result.AngularGap95 {
		t.Fatalf("confidence interval changed after reopen: %v vs %v",
			r2.AngularGap95, cr.Result.AngularGap95)
	}
	_ = ctx
}

func fuzzyVec(a, b geomag.Vec3, eps float64) bool {
	d := a.Sub(b).Norm()
	return d <= eps
}

func TestRotationMatrixExportAndRoundTrip(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	cr := createCandidate(t, h, id, map[string]any{
		"label": "chain", "frame": "geo",
		"from_seq": 1, "to_seq": 6, "origin": "free", "weighting": "none",
	})
	code, body, ctype := raw(t, h, "GET",
		fmt.Sprintf("/api/candidate/%d/chain?download=1", cr.Candidate.ID))
	if code != 200 || ctype != "application/json; charset=utf-8" {
		t.Fatalf("chain export %d %s", code, ctype)
	}
	var chains map[string]struct {
		Steps []struct {
			FromFrame string      `json:"from_frame"`
			ToFrame   string      `json:"to_frame"`
			About     string      `json:"about"`
			Matrix    geomag.Mat3 `json:"matrix"`
		} `json:"steps"`
		Combined geomag.Mat3 `json:"combined"`
	}
	if err := json.Unmarshal(body, &chains); err != nil {
		t.Fatal(err)
	}
	ch, ok := chains["1"]
	if !ok || len(ch.Steps) != 3 {
		t.Fatalf("expected 3 chain steps for seq 1, got %+v", ch)
	}
	if ch.Steps[0].FromFrame != "spec" || ch.Steps[2].ToFrame != "geo" {
		t.Fatalf("chain frames wrong: %+v", ch.Steps)
	}
	// Round-trip error on a measured vector must be below input precision.
	v := geomag.Vec3{X: 9.8123, Y: -4.0071, Z: 8.1204}
	back := ch.Combined.Transpose().Mul(ch.Combined.Mul(v))
	if d := back.Sub(v).Norm(); d > 1e-12 {
		t.Fatalf("exported matrix round trip error %.3e exceeds 1e-12", d)
	}
}

func TestWipeAndReimportReplays(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	before := specimenVectorSignature(t, h, id)
	if code, out := doJSON(t, h, "POST", "/api/reset", nil); code != 200 {
		t.Fatalf("reset %d %v", code, out)
	}
	if code, out := doJSON(t, h, "POST", "/api/import", nil); code != 200 {
		t.Fatalf("import %d %v", code, out)
	}
	id2 := specimenIDByCode(t, h, "STABLE-1")
	after := specimenVectorSignature(t, h, id2)
	if before != after {
		t.Fatalf("vectors differ after wipe + reimport")
	}
	// Run log is exportable and records the wipe.
	code, body, _ := raw(t, h, "GET", "/api/runlog")
	if code != 200 {
		t.Fatalf("runlog %d", code)
	}
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil || len(entries) == 0 {
		t.Fatalf("run log not exportable: %v", err)
	}
	foundWipe := false
	for _, e := range entries {
		if e["action"] == "wipe" {
			foundWipe = true
		}
	}
	if !foundWipe {
		t.Fatal("run log missing wipe entry")
	}
}

func specimenVectorSignature(t *testing.T, h http.Handler, id int) string {
	t.Helper()
	code, body, _ := raw(t, h, "GET", fmt.Sprintf("/api/specimen/%d", id))
	if code != 200 {
		t.Fatalf("specimen %d", code)
	}
	var data SpecimenData
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Seq     int
		Kind    string
		Level   float64
		X, Y, Z float64
		Cov     [3][3]float64
	}
	var rows []row
	for _, v := range data.Views {
		rows = append(rows, row{v.Seq, v.Treatment.Kind, v.Treatment.Level,
			v.V.X, v.V.Y, v.V.Z, v.Cov})
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

func TestSVGRenders(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "DUP-1")
	for _, p := range []string{
		fmt.Sprintf("/api/specimen/%d/plot/zijderveld.svg", id),
		fmt.Sprintf("/api/specimen/%d/plot/stereonet.svg", id),
	} {
		code, body, ctype := raw(t, h, "GET", p)
		if code != 200 || ctype != "image/svg+xml; charset=utf-8" {
			t.Fatalf("%s -> %d %s", p, code, ctype)
		}
		if !bytes.Contains(body, []byte("<svg")) {
			t.Fatalf("%s did not render svg", p)
		}
	}
}

func TestIndexServesTitle(t *testing.T) {
	_, _, h := newTestServer(t)
	code, body, _ := raw(t, h, "GET", "/")
	if code != 200 {
		t.Fatalf("index %d", code)
	}
	if !bytes.Contains(body, []byte("极性轨迹")) {
		t.Fatalf("index page missing title marker")
	}
}

func TestInvalidFrameAndWeight(t *testing.T) {
	_, _, h := newTestServer(t)
	id := specimenIDByCode(t, h, "STABLE-1")
	if code, _ := doJSON(t, h, "POST", "/api/frame", map[string]any{"frame": "moon"}); code != 400 {
		t.Fatalf("expected 400 for bad frame, got %d", code)
	}
	if code, _ := doJSON(t, h, "POST",
		fmt.Sprintf("/api/specimen/%d/candidates", id),
		map[string]any{"from_seq": 1, "to_seq": 3, "weighting": "magic"}); code != 400 {
		t.Fatalf("expected 400 for bad weighting, got %d", code)
	}
}
