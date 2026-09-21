package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpsertIdempotentAndCandidateLifecycle(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(context.Background(), filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	pid, err := st.CreateProject(ctx, "p", "spec")
	if err != nil {
		t.Fatal(err)
	}
	sp := Specimen{ProjectID: pid, Code: "S1", Name: "n", Azimuth: 1, Plunge: 2, Roll: 3}
	steps := []Step{
		{Seq: 1, Kind: "AF", Level: 0, X: 1, Cov: [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, Azimuth: 1, Plunge: 2, Roll: 3},
		{Seq: 2, Kind: "AF", Level: 10, X: 0.5, Cov: [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, Azimuth: 1, Plunge: 2, Roll: 3},
	}
	id1, n1, err := st.UpsertSpecimen(ctx, pid, sp, steps, "b1")
	if err != nil || n1 != 2 {
		t.Fatalf("first upsert n=%d err=%v", n1, err)
	}
	id2, n2, err := st.UpsertSpecimen(ctx, pid, sp, steps, "b2")
	if err != nil || n2 != 2 || id1 != id2 {
		t.Fatalf("upsert not idempotent: ids %d/%d n=%d", id1, id2, n2)
	}
	got, err := st.ListSteps(ctx, id1)
	if err != nil || len(got) != 2 {
		t.Fatalf("steps len %d err %v", len(got), err)
	}
	if got[0].Cov[1][0] != 0 || got[0].Cov[2][0] != 0 {
		t.Fatalf("covariance symmetry not reconstructed: %+v", got[0].Cov)
	}

	cid, err := st.CreateCandidate(ctx, Candidate{
		SpecimenID: id1, Label: "c", Frame: "geo", FromSeq: 1, ToSeq: 2,
		Origin: "free", Weighting: "none", BootstrapRepeats: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := st.AddAnalysis(ctx, cid, []byte(`{}`), []byte(`{}`), []byte(`{}`))
	if err != nil || v1 != 1 {
		t.Fatalf("v1=%d err=%v", v1, err)
	}
	v2, err := st.AddAnalysis(ctx, cid, []byte(`{}`), []byte(`{}`), []byte(`{}`))
	if err != nil || v2 != 2 {
		t.Fatalf("v2=%d err=%v", v2, err)
	}
	if err := st.SetDecision(ctx, cid, true, "ok", "rev"); err != nil {
		t.Fatal(err)
	}
	cands, err := st.ListCandidates(ctx, id1)
	if err != nil || len(cands) != 1 || !cands[0].Accepted || cands[0].LatestVersion != 2 {
		t.Fatalf("candidate state wrong: %+v err %v", cands, err)
	}

	if err := st.WipeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if specs, _ := st.ListSpecimens(ctx, pid); len(specs) != 0 {
		t.Fatalf("wipe left specimens: %d", len(specs))
	}
	if log, _ := st.RunLog(ctx, 10); len(log) == 0 || log[0].Action != "wipe" {
		t.Fatalf("run log after wipe wrong: %+v", log)
	}
}
