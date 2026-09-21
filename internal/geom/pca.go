package geom

import (
	"fmt"
	"math"
	"math/rand"
)

// Point is one demagnetization level expressed in the analysis frame.
type Point struct {
	StepKey   string  `json:"step_key"`
	Treatment string  `json:"treatment"`
	Level     float64 `json:"level"`
	Rep       int     `json:"rep"`
	V         Vec3    `json:"v"`
	Cov       Sym3    `json:"cov"`
}

// Weighting selects how level covariances enter the fit.
type Weighting string

const (
	WeightUniform      Weighting = "uniform"
	WeightInverseTrace Weighting = "inverse_variance"
)

// FitConfig configures a PCA fit.
type FitConfig struct {
	OriginAnchored bool      `json:"origin_anchored"`
	Weighting      Weighting `json:"weighting"`
	BootstrapSeed  int64     `json:"bootstrap_seed"`
}

// DiagnosticCode enumerates explainable fit diagnostics.
type DiagnosticCode string

const (
	DiagWindowTooShort       DiagnosticCode = "WINDOW_TOO_SHORT"
	DiagDegenerateVariance   DiagnosticCode = "DEGENERATE_VARIANCE"
	DiagNearZeroMoment       DiagnosticCode = "NEAR_ZERO_MOMENT"
	DiagPolarityFlipNearby   DiagnosticCode = "POLARITY_FLIP_NEARBY"
	DiagRepeatedLoadMismatch DiagnosticCode = "REPEATED_LOAD_MISMATCH"
	DiagBootstrapUnstable    DiagnosticCode = "BOOTSTRAP_UNSTABLE"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Diagnostic struct {
	Code     DiagnosticCode     `json:"code"`
	Severity Severity           `json:"severity"`
	Message  string             `json:"message"`
	StepKey  string             `json:"step_key,omitempty"`
	Detail   map[string]float64 `json:"detail,omitempty"`
}

// FitResult is the full, auditable output of one PCA calculation.
type FitResult struct {
	N              int                  `json:"n"`
	Centroid       Vec3                 `json:"centroid"`
	Direction      Vec3                 `json:"direction"`
	AlternatePole  Vec3                 `json:"alternate_pole"`
	Declination    float64              `json:"declination_deg"`
	Inclination    float64              `json:"inclination_deg"`
	MAD            float64              `json:"mad_deg"`
	Eigenvalues    [3]float64           `json:"eigenvalues"`
	AnchorResidual float64              `json:"anchor_residual_ma_m"`
	Endpoints      [2]Endpoint          `json:"endpoints"`
	Alpha95        float64              `json:"alpha95_deg"`
	BootstrapCount int                  `json:"bootstrap_count"`
	Diagnostics    []Diagnostic         `json:"diagnostics"`
	Weights        map[string]float64   `json:"weights"`
	Replicates     map[string][]RepStat `json:"replicates,omitempty"`
}

type Endpoint struct {
	StepKey               string  `json:"step_key"`
	PerpendicularDistance float64 `json:"perp_distance_ma_m"`
	AxialCoordinate       float64 `json:"axial_coordinate_ma_m"`
}

type RepStat struct {
	StepKey       string  `json:"step_key"`
	MeanNorm      float64 `json:"mean_norm_ma_m"`
	Spread        float64 `json:"spread_ma_m"`
	PairwiseAngle float64 `json:"pairwise_angle_deg"`
	Count         int     `json:"count"`
}

const (
	minPoints          = 3
	nearZeroThreshold  = 0.5  // mA/m, below this a level is considered near-zero
	varianceRatioFloor = 1e-8 // lambda1 relative scale needed for a line
	repMismatchSigma   = 3.0
)

// weightFor returns the weight of one point.
func weightFor(p Point, w Weighting) float64 {
	trace := p.Cov.XX + p.Cov.YY + p.Cov.ZZ
	if w == WeightInverseTrace && trace > 0 {
		return 1 / trace
	}
	return 1
}

// Fit performs weighted principal-component analysis.
//
// For the free fit the line passes through the weighted centroid; for the
// anchored fit it passes through the origin. The direction is the eigenvector
// of the largest eigenvalue of the weighted scatter matrix. MAD follows the
// Kirschvink convention atan(sqrt((l2+l3)/l1)); anchor residual is the
// perpendicular distance of the free-fit line from the origin.
func Fit(points []Point, cfg FitConfig) FitResult {
	res := FitResult{
		N:           len(points),
		Diagnostics: []Diagnostic{},
		Weights:     map[string]float64{},
	}
	if len(points) < minPoints {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: DiagWindowTooShort, Severity: SeverityError,
			Message: fmt.Sprintf("window contains %d levels, at least %d are required", len(points), minPoints),
			Detail:  map[string]float64{"have": float64(len(points)), "need": minPoints},
		})
		return res
	}

	ws := make([]float64, len(points))
	sumW := 0.0
	for i, p := range points {
		ws[i] = weightFor(p, cfg.Weighting)
		sumW += ws[i]
		res.Weights[p.StepKey] = ws[i]
	}

	centroid := Vec3{}
	if !cfg.OriginAnchored {
		for i, p := range points {
			centroid = centroid.Add(p.V.Scale(ws[i]))
		}
		centroid = centroid.Scale(1 / sumW)
	}

	var S Sym3
	addOuter := func(c Sym3, origin Vec3, w float64, p Point) Sym3 {
		d := p.V.Sub(origin)
		c.XX += w * d.X * d.X
		c.YY += w * d.Y * d.Y
		c.ZZ += w * d.Z * d.Z
		c.XY += w * d.X * d.Y
		c.YZ += w * d.Y * d.Z
		c.XZ += w * d.X * d.Z
		return c
	}
	for i, p := range points {
		S = addOuter(S, centroid, ws[i], p)
	}
	S = scaleSym(S, 1/sumW)

	vals, vecs := Jacobi(S)
	res.Eigenvalues = vals
	dir := Vec3{X: vecs.At(0, 0), Y: vecs.At(1, 0), Z: vecs.At(2, 0)}

	if vals[0] <= 0 || math.IsNaN(vals[0]) || vals[1]+vals[2] <= 0 && vals[0] == 0 {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: DiagDegenerateVariance, Severity: SeverityError,
			Message: "scatter matrix has no variance along the principal axis",
			Detail:  map[string]float64{"lambda1": vals[0], "lambda2": vals[1], "lambda3": vals[2]},
		})
		return res
	}
	if vals[0] < varianceRatioFloor*scatterScale(points, centroid) {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: DiagDegenerateVariance, Severity: SeverityError,
			Message: "variance is degenerate: window is a near-exact point cloud",
			Detail:  map[string]float64{"lambda1": vals[0], "lambda2": vals[1], "lambda3": vals[2]},
		})
		return res
	}

	// Polarity convention: direction points toward the mean remanence so D/I
	// read naturally; the antipodal pole is always retained.
	mean := Vec3{}
	for _, p := range points {
		mean = mean.Add(p.V)
	}
	mean = mean.Scale(1 / float64(len(points)))
	if dir.Dot(mean) < 0 {
		dir = dir.Scale(-1)
	}
	res.Direction = dir
	res.AlternatePole = dir.Scale(-1)
	res.Centroid = centroid
	d, inc, _ := Dir(dir)
	res.Declination, res.Inclination = d, inc

	res.MAD = deg(math.Atan(math.Sqrt((vals[1] + vals[2]) / vals[0])))

	var axisOrigin Vec3
	if cfg.OriginAnchored {
		axisOrigin = Vec3{}
		res.AnchorResidual = 0
	} else {
		axisOrigin = centroid
		res.AnchorResidual = perpendicularDistance(centroid, dir, Vec3{})
	}
	res.Endpoints = endpointResiduals(points, axisOrigin, dir)

	diagnose(points, dir, &res)
	if cfg.BootstrapSeed >= 0 {
		bootstrap(points, cfg, dir, &res)
	}
	return res
}

func scaleSym(s Sym3, k float64) Sym3 {
	return Sym3{s.XX * k, s.YY * k, s.ZZ * k, s.XY * k, s.YZ * k, s.XZ * k}
}

func scatterScale(points []Point, c Vec3) float64 {
	m := 0.0
	for _, p := range points {
		m = math.Max(m, p.V.Sub(c).Norm())
	}
	return m * m
}

// perpendicularDistance of point q from the line origin + t*axis.
func perpendicularDistance(origin, axis, q Vec3) float64 {
	w := q.Sub(origin)
	proj := axis.Scale(w.Dot(axis))
	return w.Sub(proj).Norm()
}

func endpointResiduals(points []Point, origin, dir Vec3) [2]Endpoint {
	first, last := points[0], points[len(points)-1]
	mk := func(p Point) Endpoint {
		w := p.V.Sub(origin)
		return Endpoint{
			StepKey:               p.StepKey,
			PerpendicularDistance: w.Sub(dir.Scale(w.Dot(dir))).Norm(),
			AxialCoordinate:       w.Dot(dir),
		}
	}
	return [2]Endpoint{mk(first), mk(last)}
}

func diagnose(points []Point, dir Vec3, res *FitResult) {
	for _, p := range points {
		if p.V.Norm() < nearZeroThreshold {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: DiagNearZeroMoment, Severity: SeverityWarning, StepKey: p.StepKey,
				Message: fmt.Sprintf("level %s has near-zero moment %.3g mA/m", p.StepKey, p.V.Norm()),
				Detail:  map[string]float64{"norm_ma_m": p.V.Norm(), "threshold_ma_m": nearZeroThreshold},
			})
		}
	}

	// Repeated treatment levels: never merged. Compare independent replicates
	// against their combined 1-sigma uncertainty.
	groups := map[string][]Point{}
	order := []string{}
	for _, p := range points {
		key := fmt.Sprintf("%s:%g", p.Treatment, p.Level)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], p)
	}
	if res.Replicates == nil {
		res.Replicates = map[string][]RepStat{}
	}
	for _, key := range order {
		g := groups[key]
		if len(g) < 2 {
			continue
		}
		var sum Vec3
		for _, p := range g {
			sum = sum.Add(p.V)
		}
		mean := sum.Scale(1 / float64(len(g)))
		maxSpread, maxAngle := 0.0, 0.0
		for _, p := range g {
			maxSpread = math.Max(maxSpread, p.V.Sub(mean).Norm())
			for _, q := range g {
				if p.StepKey < q.StepKey {
					maxAngle = math.Max(maxAngle, Angle(p.V, q.V))
				}
			}
		}
		stat := RepStat{
			StepKey: key, MeanNorm: mean.Norm(), Spread: maxSpread,
			PairwiseAngle: maxAngle, Count: len(g),
		}
		res.Replicates[key] = append(res.Replicates[key], stat)

		sigma := 0.0
		for _, p := range g {
			sigma += (p.Cov.XX + p.Cov.YY + p.Cov.ZZ) / 3
		}
		sigma = math.Sqrt(sigma / float64(len(g)))
		for _, p := range g {
			z := 0.0
			if sigma > 0 {
				z = p.V.Sub(mean).Norm() / sigma
			}
			if z > repMismatchSigma {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{
					Code: DiagRepeatedLoadMismatch, Severity: SeverityWarning, StepKey: p.StepKey,
					Message: fmt.Sprintf("replicate %s deviates %.1f combined-sigma from its sibling", p.StepKey, z),
					Detail: map[string]float64{
						"z": z, "spread_ma_m": p.V.Sub(mean).Norm(), "sigma_ma_m": sigma,
					},
				})
			}
		}
	}

	// Polarity flip nearby: any adjacent ordered levels whose dot product
	// crosses zero within the window.
	ordered := append([]Point(nil), points...)
	for i := 1; i < len(ordered); i++ {
		a, b := ordered[i-1].V, ordered[i].V
		if a.Norm() > nearZeroThreshold && b.Norm() > nearZeroThreshold && a.Dot(b) < 0 {
			ang := Angle(a, b)
			if ang > 120 {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{
					Code: DiagPolarityFlipNearby, Severity: SeverityWarning,
					StepKey: ordered[i].StepKey,
					Message: fmt.Sprintf("adjacent levels %s -> %s differ by %.0f degrees (polarity reversal)",
						ordered[i-1].StepKey, ordered[i].StepKey, ang),
					Detail: map[string]float64{"angle_deg": ang},
				})
			}
		}
	}
}

func bootstrap(points []Point, cfg FitConfig, dir Vec3, res *FitResult) {
	const iterations = 1000
	seed := cfg.BootstrapSeed
	if seed == 0 {
		seed = 1
	}
	rng := rand.New(rand.NewSource(seed))
	angles := make([]float64, 0, iterations)
	unstable := 0
	n := len(points)
	for it := 0; it < iterations; it++ {
		sample := make([]Point, n)
		for k := range sample {
			sample[k] = points[rng.Intn(n)]
		}
		sub := Fit(sample, FitConfig{OriginAnchored: cfg.OriginAnchored, Weighting: cfg.Weighting, BootstrapSeed: -1})
		hardError := false
		for _, dd := range sub.Diagnostics {
			if dd.Severity == SeverityError {
				hardError = true
			}
		}
		if hardError {
			unstable++
			continue
		}
		ang := Angle(dir, sub.Direction)
		if math.IsNaN(ang) {
			unstable++
			continue
		}
		angles = append(angles, ang)
	}
	if len(angles) < iterations/2 {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: DiagBootstrapUnstable, Severity: SeverityWarning,
			Message: fmt.Sprintf("bootstrap failed in %d/%d resamples", unstable, iterations),
			Detail:  map[string]float64{"failed": float64(unstable), "iterations": iterations},
		})
		return
	}
	sortFloat(angles)
	idx := int(math.Ceil(0.95*float64(len(angles)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(angles) {
		idx = len(angles) - 1
	}
	res.Alpha95 = angles[idx]
	res.BootstrapCount = len(angles)
}

func sortFloat(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
