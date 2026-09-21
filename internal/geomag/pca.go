package geomag

import (
	"fmt"
	"math"
	"sort"
)

// Diagnostic severity levels.
const (
	SevInfo  = "info"
	SevWarn  = "warning"
	SevError = "error"
)

// Diagnostic is an interpretable, machine-readable finding about a window.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	StepSeq  int    `json:"step_seq,omitempty"`
	Level    string `json:"level,omitempty"`
}

// FitResult is the versioned eigenanalysis output for one candidate window.
type FitResult struct {
	Frame       string     `json:"frame"`
	Origin      Origin     `json:"origin"`
	Weighting   Weighting  `json:"weighting"`
	N           int        `json:"n"`
	Direction   Vec3       `json:"direction"`   // unit vector in Frame
	Azimuth     float64    `json:"azimuth"`     // degrees clockwise from north
	Inclination float64    `json:"inclination"` // degrees positive down
	MAD         float64    `json:"mad"`         // maximum angular deviation, degrees
	Eigenvalues [3]float64 `json:"eigenvalues"`
	// Centroid of the fitted points (zero when anchored).
	Centroid Vec3 `json:"centroid"`
	// EndpointResidual is the orthogonal distance of the first and last
	// window point from the fitted line (same units as input moment).
	EndpointResidual [2]float64 `json:"endpoint_residual"`
	// EndpointIndex records the seq of the endpoints used above.
	EndpointIndex [2]int `json:"endpoint_index"`
	// AngularGap95 is a bootstrap half-angle (degrees); zero when unavailable.
	AngularGap95 float64      `json:"angular_gap95"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	// IncludedSeq / ExcludedSeq describe the window membership.
	IncludedSeq []int          `json:"included_seq"`
	ExcludedSeq []ExcludedStep `json:"excluded_seq"`
}

// ExcludedStep records a level that exists for the specimen but is not in the
// window, with an interpretable reason.
type ExcludedStep struct {
	Seq    int    `json:"seq"`
	Level  string `json:"level"`
	Reason string `json:"reason"`
}

// WindowSpec is the user-controlled processing window.
type WindowSpec struct {
	Frame     string    `json:"frame"`
	FromSeq   int       `json:"from_seq"`
	ToSeq     int       `json:"to_seq"`
	Origin    Origin    `json:"origin"`
	Weighting Weighting `json:"weighting"`
	// ManualExclude removes specific seqs from inside [FromSeq,ToSeq].
	ManualExclude []int `json:"manual_exclude"`
	// BootstrapSeed makes the confidence interval replayable.
	BootstrapSeed uint64 `json:"bootstrap_seed"`
	// BootstrapRepeats defaults to 2000.
	BootstrapRepeats int `json:"bootstrap_repeats"`
}

// Fit runs eigenanalysis over the requested window.
func Fit(views []StepView, spec WindowSpec) FitResult {
	res := FitResult{
		Frame:       spec.Frame,
		Origin:      spec.Origin,
		Weighting:   spec.Weighting,
		Diagnostics: []Diagnostic{},
		IncludedSeq: []int{},
		ExcludedSeq: []ExcludedStep{},
	}
	if res.Frame == "" {
		res.Frame = FrameSpec
	}
	if res.Origin == "" {
		res.Origin = OriginFree
	}
	if res.Weighting == "" {
		res.Weighting = WeightNone
	}

	manual := map[int]bool{}
	for _, s := range spec.ManualExclude {
		manual[s] = true
	}

	// Partition into included and excluded, preserving seq order.
	var pts []StepView
	allBySeq := map[int]StepView{}
	var ordered []StepView
	ordered = append(ordered, views...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	for _, sv := range ordered {
		allBySeq[sv.Seq] = sv
	}
	markExcluded := func(seq int, reason string) {
		sv := allBySeq[seq]
		res.ExcludedSeq = append(res.ExcludedSeq, ExcludedStep{
			Seq: seq, Level: sv.TreatmentLabel, Reason: reason,
		})
	}
	for _, sv := range ordered {
		inWindow := sv.Seq >= spec.FromSeq && sv.Seq <= spec.ToSeq
		switch {
		case !inWindow:
			markExcluded(sv.Seq, "outside processing window")
		case manual[sv.Seq]:
			markExcluded(sv.Seq, "manual exclusion")
		default:
			pts = append(pts, sv)
			res.IncludedSeq = append(res.IncludedSeq, sv.Seq)
		}
	}

	attachDuplicateDiagnostics(&res, pts)
	attachNearZeroDiagnostics(&res, pts)

	n := len(pts)
	res.N = n
	if n < 2 {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: "WINDOW_TOO_SHORT", Severity: SevError,
			Message: fmt.Sprintf("window contains %d level(s); eigenanalysis needs at least 2", n),
		})
		return res
	}
	if res.Origin == OriginAnchored && n < 2 {
		// anchored with one point is degenerate too.
	}

	weights := make([]float64, n)
	for i := range weights {
		weights[i] = 1
	}
	switch res.Weighting {
	case WeightIsotropic:
		for i, p := range pts {
			tr := p.Cov[0][0] + p.Cov[1][1] + p.Cov[2][2]
			if tr <= 0 || math.IsNaN(tr) {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{
					Code: "VARIANCE_DEGENERATE", Severity: SevWarn,
					Message: fmt.Sprintf("seq %d has non-positive trace covariance; weight floored", p.Seq),
					StepSeq: p.Seq, Level: p.TreatmentLabel,
				})
				weights[i] = 1
				continue
			}
			weights[i] = 3 / tr
		}
	case WeightMahalanobis:
		// Two-pass: seed with isotropic weights, solve, then reweight by
		// directional variance along the fitted axis.
		for i, p := range pts {
			tr := p.Cov[0][0] + p.Cov[1][1] + p.Cov[2][2]
			if tr <= 0 || math.IsNaN(tr) {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{
					Code: "VARIANCE_DEGENERATE", Severity: SevWarn,
					Message: fmt.Sprintf("seq %d has non-positive trace covariance; weight floored", p.Seq),
					StepSeq: p.Seq, Level: p.TreatmentLabel,
				})
				weights[i] = 1
				continue
			}
			weights[i] = 3 / tr
		}
	}

	dir, centroid, eig, ok := solveLine(pts, weights, res.Origin)
	if !ok {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: "VARIANCE_DEGENERATE", Severity: SevError,
			Message: "scatter matrix has no dominant eigenvalue; direction is undefined",
		})
		return res
	}

	if res.Weighting == WeightMahalanobis {
		for iter := 0; iter < 4; iter++ {
			for i, p := range pts {
				su := directionalVariance(p.Cov, dir)
				if su <= 0 || math.IsNaN(su) {
					res.Diagnostics = append(res.Diagnostics, Diagnostic{
						Code: "VARIANCE_DEGENERATE", Severity: SevWarn,
						Message: fmt.Sprintf("seq %d directional variance is non-positive; weight floored", p.Seq),
						StepSeq: p.Seq, Level: p.TreatmentLabel,
					})
					weights[i] = 1
					continue
				}
				weights[i] = 1 / su
			}
			ndir, nc, neig, nok := solveLine(pts, weights, res.Origin)
			if !nok {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{
					Code: "VARIANCE_DEGENERATE", Severity: SevError,
					Message: "Mahalanobis reweighting produced a degenerate scatter matrix",
				})
				return res
			}
			if math.Acos(clamp(ndir.Dot(dir), -1, 1)) < 1e-10 {
				dir, centroid, eig = ndir, nc, neig
				break
			}
			dir, centroid, eig = ndir, nc, neig
		}
	}

	res.Direction = orientDirection(dir, pts, weights, res.Origin)
	res.Centroid = centroid
	res.Eigenvalues = eig
	az, inc := ToAzInc(res.Direction)
	res.Azimuth, res.Inclination = az, inc

	// MAD per Kirschvink 1980.
	mad := math.Sqrt((eig[1] + eig[2]) / eig[0])
	if math.IsNaN(mad) || math.IsInf(mad, 0) {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: "VARIANCE_DEGENERATE", Severity: SevError,
			Message: "largest eigenvalue is zero relative to transverse scatter; MAD undefined",
		})
		res.MAD = 0
	} else {
		res.MAD = Rad(mad)
	}

	// Endpoint orthogonal residuals.
	first, last := pts[0], pts[len(pts)-1]
	res.EndpointIndex = [2]int{first.Seq, last.Seq}
	res.EndpointResidual = [2]float64{
		lineDistance(first.V, centroid, res.Direction),
		lineDistance(last.V, centroid, res.Direction),
	}

	// Polarity diagnostics.
	attachPolarityDiagnostics(&res, pts, weights, centroid)

	// Bootstrap angular confidence half-angle.
	repeats := spec.BootstrapRepeats
	if repeats <= 0 {
		repeats = 2000
	}
	res.AngularGap95 = bootstrapGap(pts, weights, res.Origin, res.Direction,
		spec.BootstrapSeed, repeats, &res.Diagnostics)
	return res
}

// solveLine builds the weighted scatter matrix and returns the principal
// eigenvector, centroid and ordered eigenvalues.
func solveLine(pts []StepView, weights []float64, origin Origin) (Vec3, Vec3, [3]float64, bool) {
	var centroid Vec3
	var wsum float64
	for i, p := range pts {
		centroid = centroid.Add(p.V.Scale(weights[i]))
		wsum += weights[i]
	}
	if wsum <= 0 {
		return Vec3{}, Vec3{}, [3]float64{}, false
	}
	centroid = centroid.Scale(1 / wsum)
	anchor := centroid
	if origin == OriginAnchored {
		anchor = Vec3{}
	}
	var s [3][3]float64
	for i, p := range pts {
		d := p.V.Sub(anchor)
		w := weights[i]
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				s[r][c] += w * d.Component(r) * d.Component(c)
			}
		}
	}
	vals, vecs := SymEigen(s)
	if vals[0] <= 0 {
		return Vec3{}, anchor, vals, false
	}
	// Degeneracy: largest two eigenvalues indistinguishable.
	scale := vals[0]
	if scale == 0 {
		return Vec3{}, anchor, vals, false
	}
	if vals[1]/scale > 1-1e-12 && (vals[0]-vals[1]) < 1e-9*math.Max(1, scale) {
		return Vec3{}, anchor, vals, false
	}
	return column(vecs, 0), anchor, vals, true
}

// orientDirection flips the principal eigenvector so that it points from the
// last (most demagnetized) point toward the first point, i.e. along the
// stable component carried by the NRM.  For anchored fits it points toward the
// high-moment (first) endpoint as well.
func orientDirection(u Vec3, pts []StepView, weights []float64, origin Origin) Vec3 {
	var fromLast Vec3
	if origin == OriginAnchored {
		// Direction from origin toward the high-moment end.
		var hi Vec3
		var wsum float64
		// weighted centroid already biased; use first-half mean minus origin.
		for i, p := range pts {
			hi = hi.Add(p.V.Scale(weights[i]))
			wsum += weights[i]
		}
		fromLast = hi.Scale(1 / wsum)
	} else {
		last := pts[len(pts)-1].V
		var firstAgg Vec3
		var wsum float64
		for i, p := range pts {
			firstAgg = firstAgg.Add(p.V.Sub(last).Scale(weights[i]))
			wsum += weights[i]
		}
		fromLast = firstAgg.Scale(1 / wsum)
	}
	if fromLast.Norm() == 0 {
		return u
	}
	if u.Dot(fromLast) < 0 {
		return u.Scale(-1)
	}
	return u
}

// ToAzInc converts an NED vector to azimuth (0..360 from north) and
// inclination (-90..90, positive down).
func ToAzInc(v Vec3) (az, inc float64) {
	n := v.Norm()
	if n == 0 {
		return 0, 0
	}
	inc = Rad(math.Asin(clamp(v.Z/n, -1, 1)))
	az = Rad(math.Atan2(v.Y, v.X))
	if az < 0 {
		az += 360
	}
	return az, inc
}

func lineDistance(p, c, u Vec3) float64 {
	d := p.Sub(c)
	return d.Sub(u.Scale(d.Dot(u))).Norm()
}

func directionalVariance(cov [3][3]float64, u Vec3) float64 {
	var s float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			s += u.Component(r) * cov[r][c] * u.Component(c)
		}
	}
	return s
}

func (v Vec3) Component(i int) float64 {
	switch i {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
