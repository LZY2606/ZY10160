package geomag

// Treatment describes how a single demagnetization level was produced.
type Treatment struct {
	// Kind is "AF" (alternating field, mT) or "TH" (thermal, degrees C).
	Kind string `json:"kind"`
	// Level is the peak field (mT) for AF or temperature (C) for TH.
	Level float64 `json:"level"`
}

// Key returns the comparable treatment identity.  Repeated measurements at
// the same Kind/Level are intentionally NOT collapsed: they are independent
// remeasurements and are reported as duplicates.
func (t Treatment) Key() string {
	return t.Kind
}

// Step is one imported demagnetization level for one specimen.
type Step struct {
	ID         int64     `json:"id"`
	SpecimenID int64     `json:"specimen_id"`
	Seq        int       `json:"seq"` // measurement order, 1-based
	Treatment  Treatment `json:"treatment"`
	// Vector is the measured 3-component moment in the specimen frame.
	Vector Vec3 `json:"vector"`
	// Cov is the measurement covariance (3x3, symmetric, specimen frame).
	Cov [3][3]float64 `json:"cov"`
	// Attitude is the mounting orientation used for this measurement.
	Attitude Attitude `json:"attitude"`
	// Note carries optional operator annotation (e.g. "repeat run").
	Note string `json:"note,omitempty"`
}

// StepView is a step after transformation into the requested analysis frame.
type StepView struct {
	Step
	TreatmentLabel string  `json:"treatment_label"`
	Frame          string  `json:"frame"`
	V              Vec3    `json:"v"`      // vector in analysis frame
	Moment         float64 `json:"moment"` // |V|
	Chain          TransformChain
}

// Weighting selects how measurement covariance enters the eigenproblem.
type Weighting string

const (
	// WeightNone uses every point equally.
	WeightNone Weighting = "none"
	// WeightIsotropic uses w_i = 3 / trace(Cov_i).
	WeightIsotropic Weighting = "isotropic"
	// WeightMahalanobis uses w_i = 1 / u^T Cov_i u iteratively.
	WeightMahalanobis Weighting = "mahalanobis"
)

// Origin selects the line-fit constraint.
type Origin string

const (
	// OriginFree fits an unconstrained line through the weighted centroid.
	OriginFree Origin = "free"
	// OriginAnchored forces the line through the geographic origin.
	OriginAnchored Origin = "anchored"
)
