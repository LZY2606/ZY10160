package geomag

import (
	"math"
	"testing"
)

func approx(t *testing.T, got, want float64, tol float64, msg string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.12f want %.12f", msg, got, want)
	}
}

func TestRotationRoundTrip(t *testing.T) {
	att := Attitude{Azimuth: 137.5, Plunge: 24.2, Roll: -18.8}
	r := SpecToGeo(att)
	rt := r.Transpose()
	got := rt.MulMat(r)
	want := Identity()
	const eps = 1e-12
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if math.Abs(got.component(i, j)-want.component(i, j)) > eps {
				t.Fatalf("R^T R not identity at (%d,%d): %.3e", i, j, got.component(i, j)-want.component(i, j))
			}
		}
	}

	v := Vec3{12.3, -4.7, 8.9}
	back := rt.Mul(r.Mul(v))
	d := back.Sub(v).Norm()
	if d > eps {
		t.Fatalf("vector round trip error %.3e exceeds %.0e", d, eps)
	}
}

func (m Mat3) component(i, j int) float64 {
	return [9]float64{m.R00, m.R01, m.R02, m.R10, m.R11, m.R12, m.R20, m.R21, m.R22}[i*3+j]
}

func TestRightHandedAndAngleSign(t *testing.T) {
	// +90 deg about Z takes +X to +Y (right-hand rule, CCW looking down -Z).
	r := RotZ(math.Pi / 2)
	p := r.Mul(Vec3{1, 0, 0})
	approx(t, p.X, 0, 1e-12, "RotZ x")
	approx(t, p.Y, 1, 1e-12, "RotZ y")
	// +90 about Y takes +Z to +X.
	ry := RotY(math.Pi / 2)
	q := ry.Mul(Vec3{0, 0, 1})
	approx(t, q.X, 1, 1e-12, "RotY z->x")
	// Basis orthonormality.
	r3 := SpecToGeo(Attitude{Azimuth: 33, Plunge: 41, Roll: 7})
	cols := []Vec3{column(r3, 0), column(r3, 1), column(r3, 2)}
	for i := range cols {
		approx(t, cols[i].Norm(), 1, 1e-12, "unit column")
	}
	cross := cols[0].Cross(cols[1])
	if cross.Sub(cols[2]).Norm() > 1e-12 {
		t.Fatalf("basis not right-handed: %v vs %v", cross, cols[2])
	}
}

func TestTiltCorrection(t *testing.T) {
	// Horizontal bedding must be the identity.
	r := GeoToTilt(Bedding{Strike: 45, Dip: 0})
	if r.MulMat(Identity()).Transpose().MulMat(r).R00 < 0 {
		t.Fatal("bad tilt")
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if math.Abs(r.component(i, j)-Identity().component(i, j)) > 1e-12 {
				t.Fatalf("zero-dip untilt not identity at %d,%d = %v", i, j, r)
			}
		}
	}
	// A 30-degree dip plane normal should become vertical after untilt.
	b := Bedding{Strike: 0, Dip: 30} // strikes north, dips east
	rt := GeoToTilt(b)
	// Plane normal in geo frame: points east/down by dip.
	normal := Vec3{0, -math.Sin(Deg(30)), math.Cos(Deg(30))}
	nt := rt.Mul(normal)
	approx(t, nt.X, 0, 1e-12, "normal x")
	approx(t, nt.Y, 0, 1e-12, "normal y")
	approx(t, nt.Z, 1, 1e-12, "normal z after untilt")

	// Chain round trip with full attitude + tilt.
	att := Attitude{Azimuth: 210, Plunge: 12, Roll: 5}
	ch := BuildChain(att, &b, FrameTilt)
	v := Vec3{3.1, 7.2, -1.4}
	fwd := ch.Combined.Mul(v)
	back := ch.Combined.Transpose().Mul(fwd)
	if d := back.Sub(v).Norm(); d > 1e-12 {
		t.Fatalf("full chain round trip error %.3e", d)
	}
}

func TestEigenKnown(t *testing.T) {
	// Diagonal matrix already eigen-aligned.
	a := [3][3]float64{{5, 0, 0}, {0, 2, 0}, {0, 0, 1}}
	vals, vecs := SymEigen(a)
	want := []float64{5, 2, 1}
	for i := range want {
		approx(t, vals[i], want[i], 1e-12, "eigenvalue")
	}
	if math.Abs(column(vecs, 0).X-1) > 1e-12 {
		t.Fatalf("expected first eigenvector +X, got %v", column(vecs, 0))
	}
	// Rank-one matrix u u^T has one nonzero eigenvalue.
	u := Vec3{1, 2, 3}.Normalize()
	var m [3][3]float64
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			m[r][c] = 10 * u.Component(r) * u.Component(c)
		}
	}
	vals2, vecs2 := SymEigen(m)
	approx(t, vals2[0], 10, 1e-10, "rank-one eigenvalue")
	approx(t, vals2[1], 0, 1e-10, "rank-one zero eig 1")
	approx(t, vals2[2], 0, 1e-10, "rank-one zero eig 2")
	e := column(vecs2, 0)
	if math.Abs(math.Abs(e.Dot(u))-1) > 1e-10 {
		t.Fatalf("rank-one eigenvector mismatch: %v vs %v", e, u)
	}
}
