// Package fixtures embeds the fixed acceptance datasets.  The generator is
// seeded so re-importing after wiping the database reproduces every level,
// covariance and attitude exactly.
package fixtures

import (
	_ "embed"
	"encoding/json"
	"math"
	"math/rand/v2"
)

// Specimen is the fixture exchange format (one JSON document per project).
type Specimen struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Azimuth float64 `json:"azimuth"`
	Plunge  float64 `json:"plunge"`
	Roll    float64 `json:"roll"`
	// Bedding, when present, enables tilt correction.
	Bedding *struct {
		Strike float64 `json:"strike"`
		Dip    float64 `json:"dip"`
	} `json:"bedding,omitempty"`
	Steps []Step `json:"steps"`
}

type Step struct {
	Seq     int     `json:"seq"`
	Kind    string  `json:"kind"`
	Level   float64 `json:"level"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	SigmaXX float64 `json:"sigma_xx"`
	SigmaYY float64 `json:"sigma_yy"`
	SigmaZZ float64 `json:"sigma_zz"`
	SigmaXY float64 `json:"sigma_xy"`
	Note    string  `json:"note,omitempty"`
}

//go:embed fixtures.json
var raw []byte

// Load returns the three fixed acceptance specimens.
func Load() ([]Specimen, error) {
	var docs struct {
		Project   string     `json:"project"`
		Specimens []Specimen `json:"specimens"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, err
	}
	return docs.Specimens, nil
}

// MustLoad panics on parse errors (startup only).
func MustLoad() []Specimen {
	s, err := Load()
	if err != nil {
		panic(err)
	}
	return s
}

// ---- deterministic generator ---------------------------------------------
//
// The three trajectories:
//
//	STABLE-1  one stable single component: points collapse onto a line whose
//	          direction stays fixed as moment decays.
//	ORIGIN-1  endpoint approaches the origin; line must be anchored to be
//	          interpreted correctly, with a visible free-fit bias.
//	DUP-1     a repeated demagnetization level carrying independent
//	          remeasurements with anomalous load between repeats.

func gaussian(rng *rand.Rand) float64 {
	// Box-Muller.
	u := 1 - rng.Float64()
	v := rng.Float64()
	return math.Sqrt(-2*math.Log(u)) * math.Cos(2*math.Pi*v)
}

// Generate rebuilds fixtures.json deterministically.
func Generate() []byte {
	rng := rand.New(rand.NewPCG(0x5eed1164, 0xa55))
	out := struct {
		Project   string     `json:"project"`
		Specimens []Specimen `json:"specimens"`
	}{Project: "acceptance"}

	mkCov := func(s float64) (float64, float64, float64, float64) {
		// variance scales loosely with the noise floor parameter s.
		v := s * s
		return v, v * 0.9, v * 1.1, v * 0.05
	}

	// STABLE-1: AF 0..100 mT. True component direction in specimen frame is
	// roughly (0.45, 0.20, 0.87); moment decays linearly.
	{
		sp := Specimen{
			Code: "STABLE-1", Name: "稳定单分量",
			Azimuth: 312, Plunge: 18, Roll: -6,
		}
		u := normalize3(0.45, 0.20, 0.87)
		moments := []float64{9.8, 9.1, 8.4, 7.6, 6.8, 5.9, 5.0, 4.1, 3.2, 2.3, 1.4}
		levels := []float64{0, 5, 10, 20, 30, 40, 50, 60, 70, 80, 100}
		for i, m := range moments {
			sig := 0.05 + 0.004*float64(i)
			sxx, syy, szz, sxy := mkCov(sig)
			x := m*u[0] + sig*gaussian(rng)
			y := m*u[1] + sig*gaussian(rng)
			z := m*u[2] + sig*gaussian(rng)
			sp.Steps = append(sp.Steps, Step{
				Seq: i + 1, Kind: "AF", Level: levels[i],
				X: x, Y: y, Z: z, SigmaXX: sxx, SigmaYY: syy, SigmaZZ: szz, SigmaXY: sxy,
			})
		}
		out.Specimens = append(out.Specimens, sp)
	}

	// ORIGIN-1: thermal, high unblocking tail collapses nearly to origin.
	// Includes an early viscous overprint so the low-temp steps are off the
	// characteristic line; the characteristic window is ~300..580C.
	{
		sp := Specimen{
			Code: "ORIGIN-1", Name: "末端趋近原点",
			Azimuth: 40, Plunge: 3, Roll: 2,
		}
		u := normalize3(0.80, 0.35, 0.48)
		// (temperature, characteristic moment)
		type tl struct {
			t float64
			m float64
		}
		seq := []tl{
			{20, 12.0}, {100, 11.8}, {200, 10.9}, {300, 8.2},
			{350, 6.4}, {400, 4.9}, {450, 3.6}, {500, 2.4},
			{540, 1.4}, {560, 0.7}, {580, 0.18}, {600, 0.0006},
		}
		overprint := normalize3(0.10, 0.90, 0.42)
		for i, p := range seq {
			vis := 0.0
			if p.t <= 200 {
				vis = 2.6 * (1 - float64(i)/2.5)
				if vis < 0 {
					vis = 0
				}
			}
			sig := 0.045
			if p.m < 0.01 {
				// The final tail is measured at a much lower noise floor;
				// covariance shrinks with it.
				sig = 0.0002
			}
			sxx, syy, szz, sxy := mkCov(sig)
			base := scale3(u, p.m)
			ov := scale3(overprint, vis)
			x := base[0] + ov[0] + sig*gaussian(rng)
			y := base[1] + ov[1] + sig*gaussian(rng)
			z := base[2] + ov[2] + sig*gaussian(rng)
			sp.Steps = append(sp.Steps, Step{
				Seq: i + 1, Kind: "TH", Level: p.t,
				X: x, Y: y, Z: z, SigmaXX: sxx, SigmaYY: syy, SigmaZZ: szz, SigmaXY: sxy,
			})
		}
		out.Specimens = append(out.Specimens, sp)
	}

	// DUP-1: AF sequence with two independent runs at 40 mT.  The second
	// 40 mT remeasurement has an anomalous load (remount offset), clearly
	// different from the first.  The two repeats must remain separate rows.
	{
		sp := Specimen{
			Code: "DUP-1", Name: "重复级异载荷",
			Azimuth: 178, Plunge: 27, Roll: 9,
		}
		u := normalize3(-0.30, 0.55, 0.78)
		type tl struct {
			t    float64
			m    float64
			note string
		}
		seq := []tl{
			{0, 10.2, ""}, {10, 9.3, ""}, {20, 8.2, ""}, {30, 7.0, ""},
			{40, 5.9, "run A"}, {40, 4.1, "run B anomalous remount"},
			{50, 5.0, ""}, {60, 4.0, ""}, {80, 2.6, ""},
		}
		anomaly := normalize3(0.72, -0.40, 0.56)
		for i, p := range seq {
			sig := 0.05
			sxx, syy, szz, sxy := mkCov(sig)
			dir := u
			mag := p.m
			if p.note == "run B anomalous remount" {
				// Blend in a substantially different vector component.
				v1 := scale3(u, p.m)
				v2 := scale3(anomaly, 2.2)
				x := v1[0] + v2[0] + sig*gaussian(rng)
				y := v1[1] + v2[1] + sig*gaussian(rng)
				z := v1[2] + v2[2] + sig*gaussian(rng)
				sp.Steps = append(sp.Steps, Step{
					Seq: i + 1, Kind: "AF", Level: p.t,
					X: x, Y: y, Z: z, SigmaXX: sxx, SigmaYY: syy, SigmaZZ: szz, SigmaXY: sxy, Note: p.note,
				})
				continue
			}
			x := mag*dir[0] + sig*gaussian(rng)
			y := mag*dir[1] + sig*gaussian(rng)
			z := mag*dir[2] + sig*gaussian(rng)
			sp.Steps = append(sp.Steps, Step{
				Seq: i + 1, Kind: "AF", Level: p.t,
				X: x, Y: y, Z: z, SigmaXX: sxx, SigmaYY: syy, SigmaZZ: szz, SigmaXY: sxy, Note: p.note,
			})
		}
		out.Specimens = append(out.Specimens, sp)
	}

	b, _ := json.MarshalIndent(out, "", "  ")
	return b
}

func normalize3(x, y, z float64) [3]float64 {
	n := math.Sqrt(x*x + y*y + z*z)
	return [3]float64{x / n, y / n, z / n}
}

func scale3(u [3]float64, s float64) [3]float64 {
	return [3]float64{u[0] * s, u[1] * s, u[2] * s}
}
