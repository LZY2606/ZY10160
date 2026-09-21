package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"polaritytrace/internal/ingest"
	"polaritytrace/internal/store"
)

func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := ingest.New(st)
	if _, err := svc.SeedFixtures(context.Background()); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>极性轨迹</h1>")},
	}
	srv := httptest.NewServer(New(svc, static).Routes())
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	return srv, dbPath
}

func doJSON(t *testing.T, method, url string, body any) (int, any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rdr)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)

	var out any
	_ = json.Unmarshal(data, &out)
	return res.StatusCode, out
}

func TestIndexServesTitle(t *testing.T) {
	srv, _ := testServer(t)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !bytes.Contains(body, []byte("极性轨迹")) {
		t.Fatalf("index missing title: %s", body)
	}
}

func TestFullWorkflowAndPersistence(t *testing.T) {
	srv, dbPath := testServer(t)
	base := srv.URL

	// Create two candidate windows in G.
	for _, win := range []map[string]any{
		{"treatment": "AF", "level_from": 0, "level_to": 40},
		{"treatment": "AF", "level_from": 20, "level_to": 80},
	} {
		status, out := doJSON(t, "POST", base+"/api/analyses", map[string]any{
			"specimen_id": "S1-STABLE", "frame": "G", "window": win,
			"weighting": "uniform", "bootstrap_seed": 5,
		})
		if status != 201 {
			t.Fatalf("create status %d: %v", status, out)
		}
	}
	status, list := doJSON(t, "GET", base+"/api/analyses?specimen_id=S1-STABLE", nil)
	if status != 200 {
		t.Fatal(status)
	}
	arr := list.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	analysisID := first["id"].(string)

	// Reject one specific level via explicit keys on S3, then verify excluded.
	s3keys := []string{"AF0", "AF10", "AF20", "AF30-R1", "AF40", "AF60"}
	status, s3an := doJSON(t, "POST", base+"/api/analyses", map[string]any{
		"specimen_id": "S3-REPLICA", "frame": "G",
		"window":         map[string]any{"keys": s3keys},
		"weighting":      "inverse_variance",
		"bootstrap_seed": 9,
	})
	if status != 201 {
		t.Fatalf("s3 create %d %v", status, s3an)
	}
	s3obj := s3an.(map[string]any)
	excluded := s3obj["excluded"].([]any)
	if len(excluded) != 1 || excluded[0].(map[string]any)["key"] != "AF30-R2" {
		t.Fatalf("explicit exclusion wrong: %v", excluded)
	}
	result := s3obj["result"].(map[string]any)
	if result["n"].(float64) != 6 {
		t.Fatalf("explicit window n = %v", result["n"])
	}

	// Human decision attached to candidate v1.
	status, dec := doJSON(t, "PUT", base+"/api/decisions", map[string]any{
		"specimen_id": "S1-STABLE", "analysis_id": analysisID,
		"action": "accepted", "rationale": "linear and low MAD",
	})
	if status != 200 {
		t.Fatalf("decision %d %v", status, dec)
	}

	// Switch coordinate frame and persist.
	status, proj := doJSON(t, "PUT", base+"/api/project/frame", map[string]any{"frame": "M"})
	if status != 200 || proj.(map[string]any)["frame"] != "M" {
		t.Fatalf("frame switch: %d %v", status, proj)
	}

	// Rotation matrix export for every step link.
	res, err := http.Get(base + "/api/specimens/S1-STABLE/rotations?frame=T")
	if err != nil {
		t.Fatal(err)
	}
	rot, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Contains(rot, []byte(`"composed"`)) || !bytes.Contains(rot, []byte("right-handed")) {
		t.Fatalf("rotation export incomplete: %s", rot)
	}

	// Reopen the "project": fresh process, same database file.
	st2, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	svc2 := ingest.New(st2)
	srv2 := httptest.NewServer(New(svc2, fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>极性轨迹</h1>")},
	}).Routes())
	defer srv2.Close()

	status, proj2 := doJSON(t, "GET", srv2.URL+"/api/project", nil)
	if proj2.(map[string]any)["frame"] != "M" {
		t.Fatalf("frame not restored after reopen: %v", proj2)
	}
	status, restored := doJSON(t, "GET", srv2.URL+"/api/analyses?specimen_id=S1-STABLE", nil)
	rarr := restored.([]any)
	if len(rarr) != 2 {
		t.Fatalf("candidates lost after reopen: %d", len(rarr))
	}
	r0 := rarr[0].(map[string]any)
	rres := r0["result"].(map[string]any)
	if _, ok := rres["alpha95_deg"]; !ok {
		t.Fatalf("confidence interval not restored: %v", rres)
	}
	if r0["decision"] == nil {
		t.Fatalf("human decision not restored")
	}
	// S3 explicit exclusion must be restored verbatim.
	status, s3rest := doJSON(t, "GET", srv2.URL+"/api/analyses?specimen_id=S3-REPLICA", nil)
	if status != 200 {
		t.Fatal(status)
	}
	s3arr := s3rest.([]any)
	if len(s3arr) != 1 {
		t.Fatalf("S3 analysis lost after reopen: %d", len(s3arr))
	}
	reExc := s3arr[0].(map[string]any)["excluded"].([]any)
	if len(reExc) != 1 || reExc[0].(map[string]any)["key"] != "AF30-R2" {
		t.Fatalf("rejected level not restored: %v", reExc)
	}

	// Reset and re-import for clean replay.
	status, reset := doJSON(t, "POST", srv2.URL+"/api/admin/reset", nil)
	if status != 200 {
		t.Fatalf("reset %d %v", status, reset)
	}
	status, again := doJSON(t, "POST", srv2.URL+"/api/admin/reseed", nil)
	if status != 200 || again.(map[string]any)["imported"].(float64) != 3 {
		t.Fatalf("reseed %d %v", status, again)
	}
	status, specsAny := doJSON(t, "GET", srv2.URL+"/api/specimens", nil)
	specs := specsAny.([]any)
	if status != 200 || len(specs) != 3 {
		t.Fatalf("specimens after replay: %v", specs)
	}

	// Run log is exportable.
	rl, err := http.Get(srv2.URL + "/api/runlog")
	if err != nil {
		t.Fatal(err)
	}
	rldata, _ := io.ReadAll(rl.Body)
	rl.Body.Close()
	if !bytes.Contains(rldata, []byte("/api/admin/reset")) {
		t.Fatalf("run log missing reset entry: %s", rldata)
	}
}
