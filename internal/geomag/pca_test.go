package geomag

import "testing"

func fitViews() []StepView {
	steps := []Step{}
	_ = steps
	return nil
}

func TestMADAndLineFit(t *testing.T) {
	// Perfectly collinear points along a known direction: MAD must be ~0.
	u := Vec3{0.6, -0.3, 0.7416198487}
	u = u.Normalize()
	var views []StepView
	mags := []float64{10, 8, 6, 4, 2}
	for i, m := range mags {
		v := u.Scale(m)
		views = append(views, StepView{
			Step: Step{Seq: i + 1, Treatment: Treatment{Kind: "AF", Level: float64(i * 10)}},
			V:    v, Moment: m,
			Frame: FrameSpec,
		})
	}
	res := Fit(views, WindowSpec{Frame: FrameSpec, FromSeq: 1, ToSeq: 5,
		Origin: OriginFree, Weighting: WeightNone, BootstrapSeed: 1, BootstrapRepeats: 500})
	if res.MAD > 1e-9 {
		t.Fatalf("collinear MAD should be 0, got %v", res.MAD)
	}
	if mathishAbs(res.Direction.Dot(u)) < 1-1e-9 {
		t.Fatalf("direction %v not aligned with %v", res.Direction, u)
	}
	if res.AngularGap95 > 1e-9 {
		t.Fatalf("collinear gap95 should be 0, got %v", res.AngularGap95)
	}
	if res.EndpointResidual[0] > 1e-9 || res.EndpointResidual[1] > 1e-9 {
		t.Fatalf("endpoint residuals should be 0: %v", res.EndpointResidual)
	}
}

func TestWindowTooShort(t *testing.T) {
	views := []StepView{{Step: Step{Seq: 1}, V: Vec3{1, 2, 3}}}
	res := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 1})
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "WINDOW_TOO_SHORT" && d.Severity == SevError {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected WINDOW_TOO_SHORT error, got %+v", res.Diagnostics)
	}
}

func TestDuplicateNotMerged(t *testing.T) {
	mk := func(seq int, level float64, v Vec3) StepView {
		return StepView{
			Step: Step{Seq: seq, Treatment: Treatment{Kind: "AF", Level: level}},
			V:    v, Moment: v.Norm(), Frame: FrameSpec,
			TreatmentLabel: "AF 40 mT",
		}
	}
	views := []StepView{
		mk(1, 40, Vec3{1, 0, 0}),
		mk(2, 40, Vec3{1, 0, 0}),
		mk(3, 40, Vec3{9, 0, 0}), // wildly different repeat -> anomalous
		mk(4, 50, Vec3{0.9, 0, 0}),
	}
	res := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 4})
	codes := map[string]string{}
	for _, d := range res.Diagnostics {
		codes[d.Code] = d.Severity
	}
	if codes["DUPLICATE_LEVEL"] == "" {
		t.Fatalf("expected DUPLICATE_LEVEL info: %+v", res.Diagnostics)
	}
	if codes["DUPLICATE_ANOMALOUS_LOAD"] != SevWarn {
		t.Fatalf("expected anomalous-load warning: %+v", res.Diagnostics)
	}
	if res.N != 4 {
		t.Fatalf("duplicates must not be merged: n=%d", res.N)
	}
}

func TestNearZeroAndPolarity(t *testing.T) {
	views := []StepView{
		{Step: Step{Seq: 1, Treatment: Treatment{Kind: "TH", Level: 100}},
			V: Vec3{5, 0, 0}, Moment: 5, TreatmentLabel: "TH 100°C"},
		{Step: Step{Seq: 2, Treatment: Treatment{Kind: "TH", Level: 200}},
			V: Vec3{4, 0, 0}, Moment: 4, TreatmentLabel: "TH 200°C"},
		{Step: Step{Seq: 3, Treatment: Treatment{Kind: "TH", Level: 300}},
			V: Vec3{0.0005, 0, 0}, Moment: 0.0005, TreatmentLabel: "TH 300°C"},
		{Step: Step{Seq: 4, Treatment: Treatment{Kind: "TH", Level: 400}},
			V: Vec3{-3, 0, 0}, Moment: 3, TreatmentLabel: "TH 400°C"},
	}
	res := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 4, BootstrapRepeats: 100})
	codes := map[string]bool{}
	for _, d := range res.Diagnostics {
		codes[d.Code] = true
	}
	if !codes["NEAR_ZERO_MOMENT"] {
		t.Fatalf("expected NEAR_ZERO_MOMENT: %+v", res.Diagnostics)
	}
	if !codes["POLARITY_FLIP"] {
		t.Fatalf("expected POLARITY_FLIP: %+v", res.Diagnostics)
	}
}

func TestWeightingVariants(t *testing.T) {
	views := []StepView{
		{Step: Step{Seq: 1, Cov: [3][3]float64{{0.01, 0, 0}, {0, 0.01, 0}, {0, 0, 0.01}}}, V: Vec3{3, 0.1, 0}, Moment: 3},
		{Step: Step{Seq: 2, Cov: [3][3]float64{{0.04, 0, 0}, {0, 0.04, 0}, {0, 0, 0.04}}}, V: Vec3{2, -0.1, 0}, Moment: 2},
		{Step: Step{Seq: 3, Cov: [3][3]float64{{0.09, 0, 0}, {0, 0.09, 0}, {0, 0, 0.09}}}, V: Vec3{1, 0.05, 0}, Moment: 1},
	}
	for _, w := range []Weighting{WeightNone, WeightIsotropic, WeightMahalanobis} {
		res := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 3, Weighting: w, BootstrapRepeats: 100})
		if len(res.Diagnostics) > 0 && res.Diagnostics[0].Severity == SevError {
			t.Fatalf("weighting %s produced error: %+v", w, res.Diagnostics)
		}
		if mathishAbs(res.Direction.X) < 0.9 {
			t.Fatalf("weighting %s lost X alignment: %v", w, res.Direction)
		}
	}
}

func TestBootstrapDeterministic(t *testing.T) {
	views := []StepView{}
	for i := 0; i < 6; i++ {
		jitter := []float64{0.02, -0.03, 0.01, -0.02, 0.03, -0.01}[i]
		v := Vec3{float64(6 - i), jitter, jitter * 0.5}
		views = append(views, StepView{Step: Step{Seq: i + 1}, V: v, Moment: v.Norm()})
	}
	a := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 6, BootstrapSeed: 99, BootstrapRepeats: 1000})
	b := Fit(views, WindowSpec{FromSeq: 1, ToSeq: 6, BootstrapSeed: 99, BootstrapRepeats: 1000})
	if a.AngularGap95 != b.AngularGap95 {
		t.Fatalf("bootstrap not deterministic: %v vs %v", a.AngularGap95, b.AngularGap95)
	}
}

func mathishAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
