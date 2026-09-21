package geomag

import "math"

// Vec3 is a 3-vector expressed in a right-handed orthonormal basis.
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Mat3 is a 3x3 rotation matrix stored row-major.  When the matrix maps a
// vector from basis A into basis B the convention is v_B = M * v_A.
type Mat3 struct {
	// R00 R01 R02 ; R10 R11 R12 ; R20 R21 R22
	R00, R01, R02 float64
	R10, R11, R12 float64
	R20, R21, R22 float64
}

func (v Vec3) Add(o Vec3) Vec3      { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3      { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Scale(s float64) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }
func (v Vec3) Dot(o Vec3) float64   { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) Cross(o Vec3) Vec3 {
	return Vec3{
		v.Y*o.Z - v.Z*o.Y,
		v.Z*o.X - v.X*o.Z,
		v.X*o.Y - v.Y*o.X,
	}
}
func (v Vec3) Norm() float64 { return math.Sqrt(v.Dot(v)) }

func (v Vec3) Normalize() Vec3 {
	n := v.Norm()
	if n == 0 {
		return Vec3{}
	}
	return v.Scale(1 / n)
}

// Mul applies the matrix to a column vector.
func (m Mat3) Mul(v Vec3) Vec3 {
	return Vec3{
		m.R00*v.X + m.R01*v.Y + m.R02*v.Z,
		m.R10*v.X + m.R11*v.Y + m.R12*v.Z,
		m.R20*v.X + m.R21*v.Y + m.R22*v.Z,
	}
}

// MulMat multiplies two matrices: a applied first, then m (i.e. m * a).
func (m Mat3) MulMat(a Mat3) Mat3 {
	return Mat3{
		m.R00*a.R00 + m.R01*a.R10 + m.R02*a.R20,
		m.R00*a.R01 + m.R01*a.R11 + m.R02*a.R21,
		m.R00*a.R02 + m.R01*a.R12 + m.R02*a.R22,
		m.R10*a.R00 + m.R11*a.R10 + m.R12*a.R20,
		m.R10*a.R01 + m.R11*a.R11 + m.R12*a.R21,
		m.R10*a.R02 + m.R11*a.R12 + m.R12*a.R22,
		m.R20*a.R00 + m.R21*a.R10 + m.R22*a.R20,
		m.R20*a.R01 + m.R21*a.R11 + m.R22*a.R21,
		m.R20*a.R02 + m.R21*a.R12 + m.R22*a.R22,
	}
}

func (m Mat3) Transpose() Mat3 {
	return Mat3{
		m.R00, m.R10, m.R20,
		m.R01, m.R11, m.R21,
		m.R02, m.R12, m.R22,
	}
}

// Identity rotation.
func Identity() Mat3 {
	return Mat3{1, 0, 0, 0, 1, 0, 0, 0, 1}
}

// RotZ returns a rotation about the Z axis.  Angles are measured in radians
// and the positive sense is counter-clockwise when looking down the axis
// toward the origin (right-hand rule): a point on +X moves toward +Y.
func RotZ(rad float64) Mat3 {
	c, s := math.Cos(rad), math.Sin(rad)
	return Mat3{c, -s, 0, s, c, 0, 0, 0, 1}
}

// RotY rotates about the Y axis; positive angle takes +Z toward +X.
func RotY(rad float64) Mat3 {
	c, s := math.Cos(rad), math.Sin(rad)
	return Mat3{c, 0, s, 0, 1, 0, -s, 0, c}
}

// RotX rotates about the X axis; positive angle takes +Y toward +Z.
func RotX(rad float64) Mat3 {
	c, s := math.Cos(rad), math.Sin(rad)
	return Mat3{1, 0, 0, 0, c, -s, 0, s, c}
}

// Deg converts degrees to radians.
func Deg(d float64) float64 { return d * math.Pi / 180 }

// Rad converts radians to degrees.
func Rad(r float64) float64 { return r * 180 / math.Pi }
