package geomag

import "math"

// TransformSteps converts specimen-frame measurements into the requested
// analysis frame.  Each returned view retains its full transformation chain
// so every direction is traceable to the raw level.
func TransformSteps(steps []Step, bedding *Bedding, frame string) []StepView {
	out := make([]StepView, 0, len(steps))
	for _, st := range steps {
		chain := BuildChain(st.Attitude, bedding, frame)
		v := ApplyChain(chain, st.Vector)
		out = append(out, StepView{
			Step:           st,
			TreatmentLabel: treatmentLabel(st.Treatment),
			Frame:          frame,
			V:              v,
			Moment:         v.Norm(),
			Chain:          chain,
		})
	}
	return out
}

// ZijdPoint is a 2-D point on a Zijderveld diagram.
type ZijdPoint struct {
	Seq   int     `json:"seq"`
	HX    float64 `json:"hx"`    // horizontal axis: N or X
	HY    float64 `json:"hy"`    // vertical vs. orthogonal horizontal
	Plane string  `json:"plane"` // "horizontal" or "vertical"
}

// Zijderveld builds the standard orthogonal projection in NED coordinates:
// horizontal plane plots North vs East; vertical plane plots North vs Down
// (filled) — convention: N toward the origin during demagnetization.
func Zijderveld(views []StepView) (horiz, vert []ZijdPoint) {
	for _, v := range views {
		horiz = append(horiz, ZijdPoint{Seq: v.Seq, HX: v.V.X, HY: v.V.Y, Plane: "horizontal"})
		vert = append(vert, ZijdPoint{Seq: v.Seq, HX: v.V.X, HY: v.V.Z, Plane: "vertical"})
	}
	return horiz, vert
}

// StereoPoint is a point on an equal-angle (Wulff) stereonet.
type StereoPoint struct {
	Seq     int     `json:"seq"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Upper   bool    `json:"upper"` // true -> upper hemisphere (open symbol)
	Azimuth float64 `json:"azimuth"`
	Incl    float64 `json:"incl"`
}

// Stereonet projects step vectors onto an equal-angle net centered on the
// vertical axis.  Down-pointing (lower hemisphere) vectors use the standard
// lower-hemisphere intersection; up-pointing ones are flagged Upper.
func Stereonet(views []StepView) []StereoPoint {
	pts := make([]StereoPoint, 0, len(views))
	for _, v := range views {
		n := v.V.Norm()
		if n == 0 {
			continue
		}
		az, inc := ToAzInc(v.V)
		upper := inc < 0
		// Polar angle from down axis.
		theta := math.Pi/2 - math.Abs(Deg(inc)) // 0 vertical down, 90 horizontal
		r := math.Tan(theta / 2)                // equal-angle, radius 0..1
		// Plot north up, east right.
		a := Deg(az)
		x := r * math.Sin(a)
		y := -r * math.Cos(a)
		pts = append(pts, StereoPoint{Seq: v.Seq, X: x, Y: y, Upper: upper, Azimuth: az, Incl: inc})
	}
	return pts
}
