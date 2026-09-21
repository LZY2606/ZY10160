// Package geom implements the paleomagnetic geometry core:
// right-handed coordinate frames, rotation chains with explicit angle
// sign conventions, covariance propagation and PCA on demagnetization paths.
//
// Coordinate convention (documented and enforced everywhere):
//
//   - Every frame is right-handed with axes X = north, Y = east, Z = down.
//   - Declination D = atan2(Y, X), clockwise from north, in degrees [0,360).
//   - Inclination I = atan2(Z, sqrt(X^2+Y^2)); positive = downward.
//   - Positive rotation angles follow the right-hand rule about the named
//     axis.  Because Z points down, a positive Rz angle is clockwise when
//     the scene is viewed from above.
//
// Three frames are supported:
//
//	M  measurement/sample frame of the imported raw vectors
//	G  geographic frame
//	T  tilt-corrected (stratigraphic) frame
package geom

import (
	"errors"
	"fmt"
	"math"
)

// Vec3 is a magnetic moment vector in mA/m.
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Mat3 is a row-major 3x3 rotation matrix.
type Mat3 struct {
	A [9]float64
}

func (v Vec3) Add(o Vec3) Vec3 { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3 { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Scale(s float64) Vec3 {
	return Vec3{v.X * s, v.Y * s, v.Z * s}
}
func (v Vec3) Dot(o Vec3) float64 { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) Cross(o Vec3) Vec3 {
	return Vec3{
		X: v.Y*o.Z - v.Z*o.Y,
		Y: v.Z*o.X - v.X*o.Z,
		Z: v.X*o.Y - v.Y*o.X,
	}
}
func (v Vec3) Norm() float64 { return math.Sqrt(v.Dot(v)) }

func (v Vec3) Normalized() (Vec3, error) {
	n := v.Norm()
	if n == 0 || math.IsNaN(n) {
		return Vec3{}, errors.New("geom: cannot normalize zero vector")
	}
	return v.Scale(1 / n), nil
}

// Dst is the storage for symmetric 3x3 matrices (covariance, scatter).
// Entries are ordered: XX, YY, ZZ, XY, YZ, XZ.
type Sym3 struct {
	XX, YY, ZZ, XY, YZ, XZ float64
}

func (m Mat3) At(r, c int) float64 { return m.A[r*3+c] }

func Mat(rows ...float64) Mat3 {
	if len(rows) != 9 {
		panic("geom: Mat needs 9 values")
	}
	return Mat3{A: [9]float64{rows[0], rows[1], rows[2], rows[3], rows[4], rows[5], rows[6], rows[7], rows[8]}}
}

func Identity() Mat3 { return Mat(1, 0, 0, 0, 1, 0, 0, 0, 1) }

func (m Mat3) Mul(o Mat3) Mat3 {
	var r Mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			s := 0.0
			for k := 0; k < 3; k++ {
				s += m.At(i, k) * o.At(k, j)
			}
			r.A[i*3+j] = s
		}
	}
	return r
}

func (m Mat3) MulV(v Vec3) Vec3 {
	return Vec3{
		X: m.At(0, 0)*v.X + m.At(0, 1)*v.Y + m.At(0, 2)*v.Z,
		Y: m.At(1, 0)*v.X + m.At(1, 1)*v.Y + m.At(1, 2)*v.Z,
		Z: m.At(2, 0)*v.X + m.At(2, 1)*v.Y + m.At(2, 2)*v.Z,
	}
}

// T returns the transpose. For rotation matrices the transpose is the inverse.
func (m Mat3) T() Mat3 {
	return Mat(
		m.A[0], m.A[3], m.A[6],
		m.A[1], m.A[4], m.A[7],
		m.A[2], m.A[5], m.A[8],
	)
}

// Rx is a right-hand-rule rotation by angle deg about the X axis.
func Rx(deg float64) Mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return Mat(1, 0, 0, 0, c, -s, 0, s, c)
}

// Ry is a right-hand-rule rotation by angle deg about the Y axis.
func Ry(deg float64) Mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return Mat(c, 0, s, 0, 1, 0, -s, 0, c)
}

// Rz is a right-hand-rule rotation by angle deg about the Z axis.
// In the NED frame a positive angle is clockwise viewed from above.
func Rz(deg float64) Mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return Mat(c, -s, 0, s, c, 0, 0, 0, 1)
}

// AxisRotation builds a right-hand-rule rotation of angle deg about unit axis a
// using the Rodrigues formula.
func AxisRotation(a Vec3, deg float64) (Mat3, error) {
	u, err := a.Normalized()
	if err != nil {
		return Mat3{}, err
	}
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	k := 1 - c
	return Mat(
		c+k*u.X*u.X, k*u.X*u.Y-s*u.Z, k*u.X*u.Z+s*u.Y,
		k*u.Y*u.X+s*u.Z, c+k*u.Y*u.Y, k*u.Y*u.Z-s*u.X,
		k*u.Z*u.X-s*u.Y, k*u.Z*u.Y+s*u.X, c+k*u.Z*u.Z,
	), nil
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }
func deg(rad float64) float64 { return rad * 180 / math.Pi }

// Angle returns the great-circle angle in degrees between two vectors.
func Angle(a, b Vec3) float64 {
	na, nb := a.Norm(), b.Norm()
	if na == 0 || nb == 0 {
		return math.NaN()
	}
	c := a.Dot(b) / (na * nb)
	if c > 1 {
		c = 1
	}
	if c < -1 {
		c = -1
	}
	return deg(math.Acos(c))
}

// Dir converts a vector to declination/inclination in degrees.
func Dir(v Vec3) (d, i float64, ok bool) {
	if v.Norm() == 0 {
		return 0, 0, false
	}
	d = deg(math.Atan2(v.Y, v.X))
	if d < 0 {
		d += 360
	}
	i = deg(math.Atan2(v.Z, math.Sqrt(v.X*v.X+v.Y*v.Y)))
	return d, i, true
}

// FromDir converts declination/inclination (degrees) to a unit vector.
func FromDir(d, i float64) Vec3 {
	cd, sd := math.Cos(rad(d)), math.Sin(rad(d))
	ci, si := math.Cos(rad(i)), math.Sin(rad(i))
	return Vec3{X: ci * cd, Y: ci * sd, Z: si}
}

// RotateSym propagates a covariance matrix through C' = R C R^T.
func RotateSym(R Mat3, c Sym3) Sym3 {
	m := Mat(
		c.XX, c.XY, c.XZ,
		c.XY, c.YY, c.YZ,
		c.XZ, c.YZ, c.ZZ,
	)
	out := R.Mul(m).Mul(R.T())
	g := func(r, cc int) float64 { return out.At(r, cc) }
	return Sym3{
		XX: g(0, 0), YY: g(1, 1), ZZ: g(2, 2),
		XY: g(0, 1), YZ: g(1, 2), XZ: g(0, 2),
	}
}

// RotStep is one documented link of a coordinate transformation chain.
type RotStep struct {
	Name     string             `json:"name"`
	FrameIn  string             `json:"frame_in"`
	FrameOut string             `json:"frame_out"`
	AngleDeg map[string]float64 `json:"angle_deg,omitempty"`
	Matrix   Mat3               `json:"matrix"`
}

// Mount describes the sample mounting orientation.
//
// The sample +X axis is the oriented core axis. It has geographic trend
// (azimuth clockwise from north) and plunge (positive downward).
// The M->G transform is R = Rz(trend) * Ry(-plunge); both factors are
// right-hand-rule rotations in the NED frame, so the handedness and the
// positive-angle direction are fully defined.
type Mount struct {
	TrendDeg  float64 `json:"trend_deg"`
	PlungeDeg float64 `json:"plunge_deg"`
}

// Matrix returns the composed M->G rotation.
func (mt Mount) Matrix() Mat3 {
	return Rz(mt.TrendDeg).Mul(Ry(-mt.PlungeDeg))
}

// Steps returns the elementary, auditable rotation factors.
func (mt Mount) Steps() []RotStep {
	return []RotStep{
		{Name: "mount_ry", FrameIn: "M", FrameOut: "M",
			AngleDeg: map[string]float64{"plunge": -mt.PlungeDeg}, Matrix: Ry(-mt.PlungeDeg)},
		{Name: "mount_rz", FrameIn: "M", FrameOut: "G",
			AngleDeg: map[string]float64{"trend": mt.TrendDeg}, Matrix: Rz(mt.TrendDeg)},
	}
}

// Bedding describes the measured bedding used for tilt correction.
//
// DipAzimuth is the downhill direction (azimuth, degrees clockwise from N)
// and DipAngle is the tilt of the bed (degrees, positive). Untilting applies
// R_axis(strike, -DipAngle), with strike = dipAzimuth - 90 in azimuth and
// angle negative because positive right-hand rotation about strike tilts the
// bed downward; reversing it restores horizontal.
type Bedding struct {
	DipAzimuthDeg float64 `json:"dip_azimuth_deg"`
	DipAngleDeg   float64 `json:"dip_angle_deg"`
}

// Matrix returns the composed G->T untilting rotation.
func (b Bedding) Matrix() Mat3 {
	strikeAz := b.DipAzimuthDeg - 90
	axis := Vec3{X: math.Cos(rad(strikeAz)), Y: math.Sin(rad(strikeAz)), Z: 0}
	R, err := AxisRotation(axis, -b.DipAngleDeg)
	if err != nil {
		panic(fmt.Sprintf("geom: bedding axis: %v", err))
	}
	return R
}

func (b Bedding) Steps() []RotStep {
	return []RotStep{
		{Name: "untilt_strike", FrameIn: "G", FrameOut: "T",
			AngleDeg: map[string]float64{
				"strike_azimuth": b.DipAzimuthDeg - 90,
				"untilt_angle":   -b.DipAngleDeg,
			},
			Matrix: b.Matrix()},
	}
}

// Chain assembles the rotation chain from M to the requested frame.
// Returned steps are in application order (leftmost applied first).
func Chain(mt Mount, bd Bedding, target string) ([]RotStep, Mat3, error) {
	switch target {
	case "M", "":
		return nil, Identity(), nil
	case "G":
		steps := mt.Steps()
		return steps, mt.Matrix(), nil
	case "T":
		steps := append(mt.Steps(), bd.Steps()...)
		return steps, mt.Matrix().Mul(bd.Matrix()), nil
	default:
		return nil, Mat3{}, fmt.Errorf("geom: unknown target frame %q", target)
	}
}

// RoundTripError applies R then R^T to v and reports the maximum absolute
// deviation. tolerance should reflect the input precision (same units as v).
func RoundTripError(R Mat3, v Vec3) (Vec3, float64) {
	back := R.T().MulV(R.MulV(v))
	d := back.Sub(v)
	err := math.Max(math.Abs(d.X), math.Max(math.Abs(d.Y), math.Abs(d.Z)))
	return back, err
}
