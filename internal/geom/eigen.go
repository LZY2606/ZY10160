package geom

import "math"

// Jacobi computes eigenvalues (descending) and eigenvectors (columns) of a
// real symmetric 3x3 matrix using cyclic Jacobi rotations. This avoids any
// cgo dependency and gives full eigenvectors needed for PCA diagnostics.
func Jacobi(s Sym3) (vals [3]float64, vecs Mat3) {
	a := [3][3]float64{
		{s.XX, s.XY, s.XZ},
		{s.XY, s.YY, s.YZ},
		{s.XZ, s.YZ, s.ZZ},
	}
	v := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

	for sweep := 0; sweep < 50; sweep++ {
		off := math.Abs(a[0][1]) + math.Abs(a[0][2]) + math.Abs(a[1][2])
		if off < 1e-15 {
			break
		}
		for p := 0; p < 2; p++ {
			for q := p + 1; q < 3; q++ {
				apq := a[p][q]
				if math.Abs(apq) < 1e-18 {
					continue
				}
				app, aqq := a[p][p], a[q][q]
				phi := 0.5 * math.Atan2(2*apq, aqq-app)
				c := math.Cos(phi)
				sn := math.Sin(phi)
				for k := 0; k < 3; k++ {
					akp := a[k][p]
					akq := a[k][q]
					a[k][p] = c*akp - sn*akq
					a[k][q] = sn*akp + c*akq
				}
				for k := 0; k < 3; k++ {
					apk := a[p][k]
					aqk := a[q][k]
					a[p][k] = c*apk - sn*aqk
					a[q][k] = sn*apk + c*aqk
				}
				for k := 0; k < 3; k++ {
					vkp := v[k][p]
					vkq := v[k][q]
					v[k][p] = c*vkp - sn*vkq
					v[k][q] = sn*vkp + c*vkq
				}
			}
		}
	}

	type ev struct {
		val float64
		col int
	}
	evs := []ev{{a[0][0], 0}, {a[1][1], 1}, {a[2][2], 2}}
	for i := 0; i < 2; i++ {
		for j := i + 1; j < 3; j++ {
			if evs[j].val > evs[i].val {
				evs[i], evs[j] = evs[j], evs[i]
			}
		}
	}
	for i, e := range evs {
		vals[i] = e.val
		for r := 0; r < 3; r++ {
			vecs.A[r*3+i] = v[r][e.col]
		}
	}
	return vals, vecs
}
