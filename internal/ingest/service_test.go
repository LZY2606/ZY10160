package ingest

import (
	"context"
	"testing"

	"polaritytrace/internal/fixture"
	"polaritytrace/internal/geom"
	"polaritytrace/internal/store"
)

func testService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	st, err := store.Open(context.Background(), "file:pctest_"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)
	n, err := svc.SeedFixtures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("seeded %d specimens, want 3", n)
	}
	return svc, context.Background()
}

func floatAt(v float64) *float64 { return &v }

func TestFixtureRoundTripPrecision(t *testing.T) {
	specs, err := fixture.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		_, R, err := geom.Chain(
			geom.Mount{TrendDeg: s.Mount.TrendDeg, PlungeDeg: s.Mount.PlungeDeg},
			s.Bedding, "T")
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range s.Steps {
			_, e := geom.RoundTripError(R, st.Vec())
			if e > 1e-12 {
				t.Fatalf("%s/%s roundtrip error %.3e exceeds input precision", s.ID, st.Key, e)
			}
		}
	}
}

func TestS1StableRecoversAuthoredDirection(t *testing.T) {
	svc, ctx := testService(t)
	// Authored in G at D=10, I=45.
	view, _, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S1-STABLE", Frame: "G",
		Window:    WindowSpec{Treatment: "AF", LevelFrom: floatAt(10), LevelTo: floatAt(60)},
		Weighting: "uniform", BootstrapSeed: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Result.MAD > 1.0 {
		t.Fatalf("S1 MAD %.3f deg too high", view.Result.MAD)
	}
	authored := geom.FromDir(10, 45)
	if ang := geom.Angle(view.Result.Direction, authored); ang > 1.0 {
		t.Fatalf("S1 direction %.1f/%.1f is %.2f deg from authored",
			view.Result.Declination, view.Result.Inclination, ang)
	}
	// Levels outside the range must be reported, never silently dropped.
	if len(view.Excluded) != 3 {
		t.Fatalf("expected 3 excluded (0,80 + none?) got %d: %+v", len(view.Excluded), view.Excluded)
	}
}

func TestS2CollapsesToOrigin(t *testing.T) {
	svc, ctx := testService(t)
	view, _, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S2-ORIGIN", Frame: "G",
		Window:    WindowSpec{Treatment: "TH", LevelFrom: floatAt(350), LevelTo: floatAt(600)},
		Weighting: "inverse_variance", BootstrapSeed: 22,
	})
	if err != nil {
		t.Fatal(err)
	}
	authored := geom.FromDir(200, -10)
	if ang := geom.Angle(view.Result.Direction, authored); ang > 2.0 {
		t.Fatalf("S2 ChRM direction %.1f/%.1f %.2f deg from authored, MAD %.3f",
			view.Result.Declination, view.Result.Inclination, ang, view.Result.MAD)
	}
	var nearZero bool
	for _, d := range view.Result.Diagnostics {
		if d.Code == geom.DiagNearZeroMoment {
			nearZero = true
		}
	}
	if !nearZero {
		t.Fatalf("S2 600C step should flag near-zero moment: %+v", view.Result.Diagnostics)
	}
}

func TestS3ReplicatesKeptAndFlagged(t *testing.T) {
	svc, ctx := testService(t)
	levels, _, _, err := svc.LoadSteps(ctx, "S3-REPLICA", "G")
	if err != nil {
		t.Fatal(err)
	}
	count30 := 0
	for _, l := range levels {
		if l.Treatment == "AF" && l.Level == 30 {
			count30++
		}
	}
	if count30 != 2 {
		t.Fatalf("30 mT replicates = %d, want 2 (must not merge)", count30)
	}
	view, _, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S3-REPLICA", Frame: "G",
		Window:    WindowSpec{Treatment: "AF", LevelFrom: floatAt(0), LevelTo: floatAt(60)},
		Weighting: "inverse_variance", BootstrapSeed: 33,
	})
	if err != nil {
		t.Fatal(err)
	}
	var mismatch bool
	for _, d := range view.Result.Diagnostics {
		if d.Code == geom.DiagRepeatedLoadMismatch {
			mismatch = true
		}
	}
	if !mismatch {
		t.Fatalf("S3 must flag discordant replicate loading: %+v", view.Result.Diagnostics)
	}
	if view.Result.N != 7 {
		t.Fatalf("S3 fit used %d points, want 7 (replicates retained)", view.Result.N)
	}
}

func TestCandidatesCoexistAndVersioned(t *testing.T) {
	svc, ctx := testService(t)
	v1, _, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S1-STABLE", Frame: "G",
		Window:    WindowSpec{Treatment: "AF", LevelFrom: floatAt(0), LevelTo: floatAt(40)},
		Weighting: "uniform", Note: "free window A",
	})
	if err != nil {
		t.Fatal(err)
	}
	v2, _, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S1-STABLE", Frame: "G",
		Window:         WindowSpec{Treatment: "AF", LevelFrom: floatAt(20), LevelTo: floatAt(80)},
		OriginAnchored: true, Weighting: "inverse_variance", ParentID: v1.ID, Note: "anchored window B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || v2.Version != 2 {
		t.Fatalf("versions %d,%d", v1.Version, v2.Version)
	}
	if v2.ParentID.String != v1.ID {
		t.Fatalf("lineage parent not stored: %+v", v2.ParentID)
	}
	all, err := svc.ListAnalyses(ctx, "S1-STABLE")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("candidates must coexist, got %d", len(all))
	}
	if _, err := svc.SetDecision(ctx, "S1-STABLE", v2.ID, "accepted", "tighter origin trend"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetAnalysis(ctx, v2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision == nil || got.Decision.Action != "accepted" {
		t.Fatalf("human decision layer missing: %+v", got.Decision)
	}
}

func TestProvenanceChainsBackToRaw(t *testing.T) {
	svc, ctx := testService(t)
	view, levels, err := svc.CreateAnalysis(ctx, CreateAnalysisRequest{
		SpecimenID: "S1-STABLE", Frame: "T",
		Window:    WindowSpec{Treatment: "AF", LevelFrom: floatAt(20), LevelTo: floatAt(60)},
		Weighting: "uniform", BootstrapSeed: 44,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Every selected key resolves to a real raw level carrying a chain.
	index := map[string]TransformedStep{}
	for _, l := range levels {
		index[l.Key] = l
	}
	for _, key := range view.Selected {
		l, ok := index[key]
		if !ok {
			t.Fatalf("selected key %s has no raw level", key)
		}
		if len(l.Chain) == 0 {
			t.Fatalf("level %s has no rotation chain in frame %s", key, l.Frame)
		}
		if l.RoundTripErr > 1e-12 {
			t.Fatalf("level %s roundtrip %.3e", key, l.RoundTripErr)
		}
	}
	exp, err := svc.RotationExport(ctx, "S1-STABLE", "T")
	if err != nil {
		t.Fatal(err)
	}
	if len(exp.Steps) != 3 {
		t.Fatalf("T frame chain should document 3 elementary steps, got %d", len(exp.Steps))
	}
}
