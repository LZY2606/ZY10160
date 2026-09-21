package geomag

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// NearZeroMoment is the threshold (in the fixture moment units) under which a
// level is flagged as effectively zero moment.
const NearZeroMoment = 1e-3

func treatmentLabel(t Treatment) string {
	switch t.Kind {
	case "AF":
		return fmt.Sprintf("AF %.0f mT", t.Level)
	case "TH":
		return fmt.Sprintf("TH %.0f°C", t.Level)
	default:
		return fmt.Sprintf("%s %.1f", t.Kind, t.Level)
	}
}

// attachDuplicateDiagnostics flags repeated treatment levels.  Duplicates are
// never silently merged: every repeat stays an independent point, and large
// spread among repeats is reported as an anomalous-load condition.
func attachDuplicateDiagnostics(res *FitResult, pts []StepView) {
	type rep struct {
		key   string
		label string
		seqs  []int
		vecs  []Vec3
	}
	byKey := map[string]*rep{}
	var order []string
	for _, p := range pts {
		key := p.Treatment.Kind + ":" + fmt.Sprintf("%g", p.Treatment.Level)
		r, ok := byKey[key]
		if !ok {
			r = &rep{key: key, label: p.TreatmentLabel}
			byKey[key] = r
			order = append(order, key)
		}
		r.seqs = append(r.seqs, p.Seq)
		r.vecs = append(r.vecs, p.V)
	}
	for _, k := range order {
		r := byKey[k]
		if len(r.seqs) < 2 {
			continue
		}
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: "DUPLICATE_LEVEL", Severity: SevInfo,
			Message: fmt.Sprintf("%s has %d independent remeasurements (not merged)", r.label, len(r.seqs)),
			Level:   r.label,
		})
		// Anomalous load: largest pairwise separation compared with the
		// mean moment at this level.
		var maxSep, meanMom float64
		for i := range r.vecs {
			meanMom += r.vecs[i].Norm()
			for j := i + 1; j < len(r.vecs); j++ {
				if d := r.vecs[i].Sub(r.vecs[j]).Norm(); d > maxSep {
					maxSep = d
				}
			}
		}
		meanMom /= float64(len(r.vecs))
		if meanMom > 0 && maxSep/meanMom > 0.2 {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: "DUPLICATE_ANOMALOUS_LOAD", Severity: SevWarn,
				Message: fmt.Sprintf("%s repeat vectors differ by %.1f%% of mean moment; check loading/measurement",
					r.label, 100*maxSep/meanMom),
				Level: r.label,
			})
		}
	}
}

func attachNearZeroDiagnostics(res *FitResult, pts []StepView) {
	for _, p := range pts {
		if p.Moment >= 0 && p.Moment < NearZeroMoment {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: "NEAR_ZERO_MOMENT", Severity: SevWarn,
				Message: fmt.Sprintf("seq %d (%s) moment %.2e is below %g; direction unreliable",
					p.Seq, p.TreatmentLabel, p.Moment, NearZeroMoment),
				StepSeq: p.Seq, Level: p.TreatmentLabel,
			})
		}
	}
}

// attachPolarityDiagnostics detects a reversal inside the window: once
// projected onto the fitted direction, consecutive points change sign with a
// magnitude large enough to be meaningful.
func attachPolarityDiagnostics(res *FitResult, pts []StepView, weights []float64, centroid Vec3) {
	if len(pts) < 3 {
		return
	}
	u := res.Direction
	type sp struct {
		seq   int
		label string
		s     float64
		m     float64
	}
	var ps []sp
	for _, p := range pts {
		t := p.V.Sub(centroid).Dot(u)
		ps = append(ps, sp{p.Seq, p.TreatmentLabel, t, p.Moment})
	}
	// Reference scale from median moment.
	moms := make([]float64, len(ps))
	for i, p := range ps {
		moms[i] = p.m
	}
	sort.Float64s(moms)
	scale := moms[len(moms)/2]
	if scale < NearZeroMoment {
		return
	}
	for i := 1; i < len(ps); i++ {
		if ps[i-1].s*ps[i].s < 0 && math.Min(math.Abs(ps[i-1].s), math.Abs(ps[i].s)) > 0.15*scale {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: "POLARITY_FLIP", Severity: SevWarn,
				Message: fmt.Sprintf("sign reverses between seq %d (%s) and seq %d (%s); two polarity zones in one window",
					ps[i-1].seq, ps[i-1].label, ps[i].seq, ps[i].label),
				StepSeq: ps[i].seq, Level: ps[i].label,
			})
		}
	}
}

// bootstrapGap resamples points with replacement, refits the line, and returns
// the 95th percentile angular deviation (degrees) from the point estimate.
// The RNG is seeded so results replay byte-for-byte.
func bootstrapGap(pts []StepView, weights []float64, origin Origin, dir Vec3,
	seed uint64, repeats int, diags *[]Diagnostic) float64 {
	n := len(pts)
	if n < 3 {
		*diags = append(*diags, Diagnostic{
			Code: "CI_INSUFFICIENT", Severity: SevInfo,
			Message: "fewer than 3 levels; angular 95% interval not bootstrapped",
		})
		return 0
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	devs := make([]float64, 0, repeats)
	for k := 0; k < repeats; k++ {
		sample := make([]StepView, n)
		sw := make([]float64, n)
		for i := range sample {
			j := rng.IntN(n)
			sample[i] = pts[j]
			sw[i] = weights[j]
		}
		u, _, _, ok := solveLine(sample, sw, origin)
		if !ok || u.Norm() == 0 {
			continue
		}
		if u.Dot(dir) < 0 {
			u = u.Scale(-1)
		}
		cosang := clamp(u.Dot(dir), -1, 1)
		devs = append(devs, Rad(math.Acos(cosang)))
	}
	if len(devs) < repeats/2 {
		*diags = append(*diags, Diagnostic{
			Code: "CI_UNSTABLE", Severity: SevWarn,
			Message: "majority of bootstrap fits were degenerate; interval unreliable",
		})
		return 0
	}
	sort.Float64s(devs)
	idx := int(math.Ceil(0.95*float64(len(devs)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(devs) {
		idx = len(devs) - 1
	}
	return devs[idx]
}
