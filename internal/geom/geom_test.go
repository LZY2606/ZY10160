package geom

import (
	"math"
	"testing"
)

func approxEq(t *testing.T, got, want, tol float64, name string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %.8f, want %.8f (tol %.0e)", name, got, want, tol)
	}
}

func TestRzHandedness(t *testing.T) {
	// Right-hand rule about Z (NED): north vector rotates toward east for +90.
	v := Rz(90).MulV(Vec3{X: 1})
	approxEq(t, v.X, 0, 1e-12, "x")
	approxEq(t, v.Y, 1, 1e-12, "y")
	// Determinant +1.
	R := Rz(37)
	det := det3(R)
	approxEq(t, det, 1, 1e-12, "det")
}

func TestMountForwardAxes(t *testing.T) {
	// Horizontal core trending north: sample +X -> north, +Y -> east.
	R := Mount{TrendDeg: 0, PlungeDeg: 0}.Matrix()
	x := R.MulV(Vec3{X: 1})
	y := R.MulV(Vec3{Y: 1})
	approxEq(t, x.X, 1, 1e-12, "x.north")
	approxEq(t, y.Y, 1, 1e-12, "y.east")

	// Vertical (downward) core: sample +X -> down.
	Rv := Mount{TrendDeg: 42, PlungeDeg: 90}.Matrix()
	xv := Rv.MulV(Vec3{X: 1})
	approxEq(t, xv.Z, 1, 1e-12, "x.down")

	// Plunging 25 at trend 315: documented right-hand chain, verify invert.
	Rp := Mount{TrendDeg: 315, PlungeDeg: 25}.Matrix()
	if _, e := RoundTripError(Rp, Vec3{X: 1, Y: -2, Z: 3}); e > 1e-12 {
		t.Fatalf("round trip error %.3e", e)
	}
}

func TestBeddingUntilts(t *testing.T) {
	// Bed dips 30 toward azimuth 0 (north): a vector pointing downhill in G
	// (north + down at sin30) must become horizontal north in T.
	b := Bedding{DipAzimuthDeg: 0, DipAngleDeg: 30}
	vG := Vec3{X: math.Cos(rad(30)), Z: math.Sin(rad(30))}
	vT := b.Matrix().MulV(vG)
	approxEq(t, vT.X, 1, 1e-12, "untilted north")
	approxEq(t, vT.Z, 0, 1e-12, "untilted horizontal")
}

func TestDirRoundTrip(t *testing.T) {
	for _, tc := range []struct{ d, i float64 }{{10, 45}, {200, -10}, {350, 60}, {90, 0}} {
		v := FromDir(tc.d, tc.i)
		d, i, ok := Dir(v)
		if !ok {
			t.Fatal("zero dir")
		}
		wantD := tc.d
		if wantD == 360 {
			wantD = 0
		}
		approxEq(t, d, wantD, 1e-9, "D")
		approxEq(t, i, tc.i, 1e-9, "I")
	}
}

func TestCovariancePropagation(t *testing.T) {
	R := Rz(90)
	c := Sym3{XX: 2, YY: 1, ZZ: 3, XY: 0.4}
	out := RotateSym(R, c)
	approxEq(t, out.XX, 1, 1e-12, "cxx'=cyy")
	approxEq(t, out.YY, 2, 1e-12, "cyy'=cxx")
	approxEq(t, out.ZZ, 3, 1e-12, "czz'=czz")
}

func det3(m Mat3) float64 {
	a, b, c := m.At(0, 0), m.At(0, 1), m.At(0, 2)
	d, e, f := m.At(1, 0), m.At(1, 1), m.At(1, 2)
	g, h, i := m.At(2, 0), m.At(2, 1), m.At(2, 2)
	return a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
}
