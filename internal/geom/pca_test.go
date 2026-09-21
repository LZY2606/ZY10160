package geom

import (
	"math"
	"testing"
)

func isoCov() Sym3 { return Sym3{XX: 0.0016, YY: 0.0016, ZZ: 0.0016} }

func TestStableLine(t *testing.T) {
	u := FromDir(10, 45)
	var pts []Point
	mags := []float64{12, 10.5, 9, 7.5, 6, 4.5, 3, 1.5}
	for i, m := range mags {
		pts = append(pts, Point{
			StepKey: levelKey(i), Treatment: "AF", Level: float64(i * 10),
			V: u.Scale(m), Cov: isoCov(),
		})
	}
	res := Fit(pts, FitConfig{Weighting: WeightUniform, BootstrapSeed: 7})
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", res.Diagnostics)
	}
	ang := Angle(res.Direction, u)
	if ang > 1e-9 {
		t.Fatalf("direction error %.3e deg, MAD %.4f", ang, res.MAD)
	}
	if res.MAD > 1e-5 {
		t.Fatalf("perfect line MAD %.3e", res.MAD)
	}
	// Free fit of a line through origin has near-zero anchor residual.
	if res.AnchorResidual > 1e-9 {
		t.Fatalf("anchor residual %.3e", res.AnchorResidual)
	}
	if res.Endpoints[0].PerpendicularDistance > 1e-6 {
		t.Fatalf("endpoint residual %.3e", res.Endpoints[0].PerpendicularDistance)
	}
	if res.Alpha95 < 0 || res.Alpha95 > 1e-6 {
		t.Fatalf("alpha95 %.6f", res.Alpha95)
	}
	// Polarity convention aligns with the mean (positive magnetization).
	d, _, _ := Dir(res.Direction)
	if math.Abs(d-10) > 1e-9 {
		t.Fatalf("declination %.6f", d)
	}
}

func TestWindowTooShort(t *testing.T) {
	pts := []Point{
		{StepKey: "a", V: Vec3{X: 1}, Cov: isoCov()},
		{StepKey: "b", V: Vec3{X: 2}, Cov: isoCov()},
	}
	res := Fit(pts, FitConfig{})
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == DiagWindowTooShort && d.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected WINDOW_TOO_SHORT error, got %+v", res.Diagnostics)
	}
}

func TestDegenerateVariance(t *testing.T) {
	pts := make([]Point, 4)
	for i := range pts {
		pts[i] = Point{StepKey: levelKey(i), V: Vec3{X: 1, Y: 2, Z: 3}, Cov: isoCov()}
	}
	res := Fit(pts, FitConfig{})
	codes := map[DiagnosticCode]bool{}
	for _, d := range res.Diagnostics {
		codes[d.Code] = true
	}
	if !codes[DiagDegenerateVariance] {
		t.Fatalf("expected degenerate variance, got %+v", res.Diagnostics)
	}
}

func TestNearZeroAndPolarity(t *testing.T) {
	pts := []Point{
		{StepKey: "a", Treatment: "TH", Level: 100, V: Vec3{X: 5}, Cov: isoCov()},
		{StepKey: "b", Treatment: "TH", Level: 200, V: Vec3{X: 2}, Cov: isoCov()},
		{StepKey: "c", Treatment: "TH", Level: 300, V: Vec3{X: -3}, Cov: isoCov()},
		{StepKey: "d", Treatment: "TH", Level: 400, V: Vec3{X: 0.1}, Cov: isoCov()},
		{StepKey: "e", Treatment: "TH", Level: 500, V: Vec3{X: -2}, Cov: isoCov()},
	}
	res := Fit(pts, FitConfig{BootstrapSeed: 1})
	codes := map[DiagnosticCode]Severity{}
	for _, d := range res.Diagnostics {
		codes[d.Code] = d.Severity
	}
	if codes[DiagNearZeroMoment] == "" {
		t.Fatalf("missing near-zero diagnostic: %+v", res.Diagnostics)
	}
	if codes[DiagPolarityFlipNearby] == "" {
		t.Fatalf("missing flip diagnostic: %+v", res.Diagnostics)
	}
}

func TestReplicatedLoadMismatch(t *testing.T) {
	u := FromDir(300, 20)
	pts := []Point{
		{StepKey: "AF0", Treatment: "AF", Level: 0, V: u.Scale(11), Cov: isoCov()},
		{StepKey: "AF20", Treatment: "AF", Level: 20, V: u.Scale(8), Cov: isoCov()},
		{StepKey: "AF30-R1", Treatment: "AF", Level: 30, Rep: 1, V: u.Scale(6.3), Cov: isoCov()},
		{StepKey: "AF30-R2", Treatment: "AF", Level: 30, Rep: 2,
			V: u.Scale(6.3).Add(Vec3{X: 1.4, Y: -0.6, Z: 0.7}), Cov: isoCov()},
		{StepKey: "AF40", Treatment: "AF", Level: 40, V: u.Scale(4.9), Cov: isoCov()},
	}
	res := Fit(pts, FitConfig{BootstrapSeed: 3})
	var mismatch bool
	for _, d := range res.Diagnostics {
		if d.Code == DiagRepeatedLoadMismatch {
			mismatch = true
			if d.Detail["z"] <= repMismatchSigma {
				t.Fatalf("z not beyond threshold: %v", d.Detail)
			}
		}
	}
	if !mismatch {
		t.Fatalf("expected REPEATED_LOAD_MISMATCH, got %+v", res.Diagnostics)
	}
	if got := len(res.Weights); got != 5 {
		t.Fatalf("replicate was merged: %d weights, want 5", got)
	}
}

func TestAnchoredFit(t *testing.T) {
	u := FromDir(45, 30)
	var pts []Point
	for i, m := range []float64{8, 6, 4, 2} {
		pts = append(pts, Point{StepKey: levelKey(i), V: u.Scale(m).Add(Vec3{X: 1}), Cov: isoCov()})
	}
	free := Fit(pts, FitConfig{BootstrapSeed: 5})
	anchored := Fit(pts, FitConfig{OriginAnchored: true, BootstrapSeed: 5})
	if free.AnchorResidual < 0.5 {
		t.Fatalf("offset line should have anchor residual, got %.3f", free.AnchorResidual)
	}
	if anchored.AnchorResidual != 0 {
		t.Fatalf("anchored line must pass origin, got %.3f", anchored.AnchorResidual)
	}
}

func TestInverseVarianceWeighting(t *testing.T) {
	u := FromDir(0, 0)
	pts := []Point{
		{StepKey: "a", V: u.Scale(10), Cov: Sym3{XX: 0.01, YY: 0.01, ZZ: 0.01}},
		{StepKey: "b", V: u.Scale(5), Cov: Sym3{XX: 0.01, YY: 0.01, ZZ: 0.01}},
		{StepKey: "c", V: u.Scale(0).Add(Vec3{Y: 1}), Cov: Sym3{XX: 100, YY: 100, ZZ: 100}},
	}
	res := Fit(pts, FitConfig{Weighting: WeightInverseTrace, BootstrapSeed: 9})
	if Angle(res.Direction, u) > 2 {
		t.Fatalf("weighted fit should ignore noisy outlier, ang %.2f MAD %.3f", Angle(res.Direction, u), res.MAD)
	}
}

func levelKey(i int) string {
	return string(rune('a' + i))
}
