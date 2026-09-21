package geomag

import "math"

// SymEigen computes eigenvalues and eigenvectors of a real symmetric 3x3
// matrix using the cyclic Jacobi rotation.  Eigenvalues are returned in
// descending order; column i of the returned matrix is the unit eigenvector
// for eigenvalue i.  The routine is deterministic and allocation-light, which
// keeps replay reproducible.
func SymEigen(a [3][3]float64) (vals [3]float64, vecs Mat3) {
	const sweeps = 64
	const tol = 1e-15

	v := Identity()
	for iter := 0; iter < sweeps; iter++ {
		off := math.Hypot(a[0][1], math.Hypot(a[0][2], a[1][2]))
		if off < tol {
			break
		}
		for p := 0; p < 2; p++ {
			for q := p + 1; q < 3; q++ {
				apq := a[p][q]
				if math.Abs(apq) < 1e-300 {
					continue
				}
				app := a[p][p]
				aqq := a[q][q]
				var c, s float64
				if math.Abs(app-aqq) < 1e-14*math.Max(math.Abs(app), math.Abs(aqq)) {
					theta := math.Pi / 4
					if apq < 0 {
						theta = -theta
					}
					c, s = math.Cos(theta), math.Sin(theta)
				} else {
					tau := (aqq - app) / (2 * apq)
					t := 0.0
					if tau >= 0 {
						t = 1 / (tau + math.Sqrt(1+tau*tau))
					} else {
						t = -1 / (-tau + math.Sqrt(1+tau*tau))
					}
					c = 1 / math.Sqrt(1+t*t)
					s = t * c
				}
				// A <- J^T A J with J a Givens rotation in plane (p,q).
				for k := 0; k < 3; k++ {
					akp, akq := a[k][p], a[k][q]
					a[k][p] = c*akp - s*akq
					a[k][q] = s*akp + c*akq
				}
				for k := 0; k < 3; k++ {
					apk, aqk := a[p][k], a[q][k]
					a[p][k] = c*apk - s*aqk
					a[q][k] = s*apk + c*aqk
				}
				rotateColumns(&v, p, q, c, s)
			}
		}
	}
	vals = [3]float64{a[0][0], a[1][1], a[2][2]}
	idx := [3]int{0, 1, 2}
	for i := 1; i < 3; i++ {
		for j := i; j > 0 && vals[idx[j]] > vals[idx[j-1]]; j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	var sorted [3]float64
	var cols [3]Vec3
	for i, k := range idx {
		sorted[i] = vals[k]
		cols[i] = column(v, k)
	}
	return sorted, fromColumns(cols)
}

func rotateColumns(v *Mat3, p, q int, c, s float64) {
	cp, cq := column(*v, p), column(*v, q)
	setColumn(v, p, cp.Scale(c).Sub(cq.Scale(s)))
	setColumn(v, q, cp.Scale(s).Add(cq.Scale(c)))
}

func column(m Mat3, k int) Vec3 {
	switch k {
	case 0:
		return Vec3{m.R00, m.R10, m.R20}
	case 1:
		return Vec3{m.R01, m.R11, m.R21}
	default:
		return Vec3{m.R02, m.R12, m.R22}
	}
}

func setColumn(m *Mat3, k int, v Vec3) {
	switch k {
	case 0:
		m.R00, m.R10, m.R20 = v.X, v.Y, v.Z
	case 1:
		m.R01, m.R11, m.R21 = v.X, v.Y, v.Z
	default:
		m.R02, m.R12, m.R22 = v.X, v.Y, v.Z
	}
}

func fromColumns(cols [3]Vec3) Mat3 {
	return Mat3{
		cols[0].X, cols[1].X, cols[2].X,
		cols[0].Y, cols[1].Y, cols[2].Y,
		cols[0].Z, cols[1].Z, cols[2].Z,
	}
}
