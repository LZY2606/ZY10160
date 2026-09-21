// Package fixture holds the fixed, deterministic acceptance specimens.
//
// Trajectories are authored in the geographic (G) frame and mapped into the
// measurement (M) frame with the documented inverse rotation chain, so the
// "import raw in M -> transform to G -> recover authored direction" loop is
// an end-to-end check of the coordinate math.
package fixture

import (
	"embed"
	"encoding/json"
	"math"
	"strconv"

	"polaritytrace/internal/geom"
)

//go:embed data/*.json
var dataFS embed.FS

type Step struct {
	Key       string     `json:"key"`
	Treatment string     `json:"treatment"`
	Level     float64    `json:"level"`
	Rep       int        `json:"rep"`
	X, Y, Z   float64    `json:"-"`
	Cov       [6]float64 `json:"cov"` // XX, YY, ZZ, XY, YZ, XZ in M frame
}

type Specimen struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Mount       geom.Mount   `json:"mount"`
	Bedding     geom.Bedding `json:"bedding"`
	Steps       []Step       `json:"steps"`
}

type rawStep struct {
	Key       string     `json:"key"`
	Treatment string     `json:"treatment"`
	Level     float64    `json:"level"`
	Rep       int        `json:"rep"`
	V         [3]float64 `json:"v"`
	Cov       [6]float64 `json:"cov"`
}
type rawSpecimen struct {
	ID, Name, Description string
	Mount                 geom.Mount
	Bedding               geom.Bedding
	Steps                 []rawStep
}

// LoadAll reads the committed JSON fixtures.
func LoadAll() ([]Specimen, error) {
	entries, err := dataFS.ReadDir("data")
	if err != nil {
		return nil, err
	}
	var out []Specimen
	for _, e := range entries {
		b, err := dataFS.ReadFile("data/" + e.Name())
		if err != nil {
			return nil, err
		}
		var raw rawSpecimen
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, err
		}
		s := Specimen{
			ID: raw.ID, Name: raw.Name, Description: raw.Description,
			Mount: raw.Mount, Bedding: raw.Bedding,
		}
		for _, r := range raw.Steps {
			s.Steps = append(s.Steps, Step{
				Key: r.Key, Treatment: r.Treatment, Level: r.Level, Rep: r.Rep,
				X: r.V[0], Y: r.V[1], Z: r.V[2], Cov: r.Cov,
			})
		}
		out = append(out, s)
	}
	return out, nil
}

func (s Step) Vec() geom.Vec3 { return geom.Vec3{X: s.X, Y: s.Y, Z: s.Z} }
func (s Step) Sym() geom.Sym3 {
	return geom.Sym3{XX: s.Cov[0], YY: s.Cov[1], ZZ: s.Cov[2],
		XY: s.Cov[3], YZ: s.Cov[4], XZ: s.Cov[5]}
}

// ---- deterministic generation -------------------------------------------

type lcg struct{ state uint64 }

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }

// float64 returns deterministic pseudo-noise in [-1,1).
func (g *lcg) float64() float64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return float64(g.state>>40)/(1<<24) - 1
}

// gauss returns a deterministic approximately-normal value.
func (g *lcg) gauss() float64 {
	return (g.float64() + g.float64() + g.float64()) / 3
}

const noiseScale = 0.018 // mA/m jitter on authored points
const varAxis = 0.0016   // (0.04 mA/m)^2 per-axis measurement variance

func goodCov(g *lcg) [6]float64 {
	return [6]float64{varAxis, varAxis, varAxis,
		0.2 * varAxis * g.float64(),
		0.2 * varAxis * g.float64(),
		0.2 * varAxis * g.float64()}
}

// authored is one G-frame trajectory point with optional per-component
// magnitudes; mapping into M frame happens in emit.
type authored struct {
	key       string
	treatment string
	level     float64
	rep       int
	vG        geom.Vec3
	covG      geom.Sym3
}

func toM(s *Specimen, a authored) Step {
	_, R, err := geom.Chain(s.Mount, s.Bedding, "G")
	if err != nil {
		panic(err)
	}
	vM := R.T().MulV(a.vG)
	cM := geom.RotateSym(R.T(), a.covG)
	return Step{
		Key: a.key, Treatment: a.treatment, Level: a.level, Rep: a.rep,
		X: vM.X, Y: vM.Y, Z: vM.Z,
		Cov: [6]float64{cM.XX, cM.YY, cM.ZZ, cM.XY, cM.YZ, cM.XZ},
	}
}

func covFrom(c [6]float64) geom.Sym3 {
	return geom.Sym3{XX: c[0], YY: c[1], ZZ: c[2], XY: c[3], YZ: c[4], XZ: c[5]}
}

// Generate rebuilds the three acceptance specimens deterministically.
func Generate() []Specimen {
	specs := []*Specimen{
		{
			ID: "S1-STABLE", Name: "S1 稳定单分量",
			Description: "AF 退磁轨迹近似一条直线，自由拟合与原点约束拟合均收敛，MAD 很小。",
			Mount:       geom.Mount{TrendDeg: 315, PlungeDeg: 25},
			Bedding:     geom.Bedding{DipAzimuthDeg: 20, DipAngleDeg: 30},
		},
		{
			ID: "S2-ORIGIN", Name: "S2 末端趋近原点",
			Description: "热退磁：低温黏滞组分先被剥离，350°C 后稳定分量直线趋向原点，末端近零磁矩。",
			Mount:       geom.Mount{TrendDeg: 90, PlungeDeg: 0},
			Bedding:     geom.Bedding{DipAzimuthDeg: 120, DipAngleDeg: 20},
		},
		{
			ID: "S3-REPLICA", Name: "S3 重复级异载荷",
			Description: "AF 30 mT 存在两个独立复测且载荷不一致：重复行保留不合并，并给出异载荷诊断。",
			Mount:       geom.Mount{TrendDeg: 0, PlungeDeg: 10},
			Bedding:     geom.Bedding{DipAzimuthDeg: 45, DipAngleDeg: 15},
		},
	}

	// S1: clean AF line along D=10 I=45, 12.0 -> 1.4 mA/m.
	{
		s := specs[0]
		g := newLCG(101)
		u := geom.FromDir(10, 45)
		levels := []float64{0, 5, 10, 20, 30, 40, 50, 60, 80}
		mags := []float64{12.0, 10.5, 9.2, 7.6, 6.1, 4.7, 3.5, 2.4, 1.4}
		for i, l := range levels {
			jit := geom.Vec3{X: g.gauss() * noiseScale, Y: g.gauss() * noiseScale, Z: g.gauss() * noiseScale}
			key := "AF" + trimLevel(l)
			s.Steps = append(s.Steps, toM(s, authored{
				key: key, treatment: "AF", level: l, rep: 1,
				vG: u.Scale(mags[i]).Add(jit), covG: covFrom(goodCov(g)),
			}))
		}
	}

	// S2: thermal two-component path collapsing onto the origin.
	{
		s := specs[1]
		g := newLCG(202)
		vrm := geom.FromDir(350, 60)   // removed between 200 and 350 C
		chrm := geom.FromDir(200, -10) // linear to origin after 350 C
		levels := []float64{100, 200, 300, 350, 400, 450, 500, 550, 580, 600}
		for _, l := range levels {
			mVrm := clamp((350-l)/150, 0, 1) * 9.0
			mCh := clamp((600-l)/250, 0, 1) * 7.5
			jit := geom.Vec3{X: g.gauss() * noiseScale, Y: g.gauss() * noiseScale, Z: g.gauss() * noiseScale}
			s.Steps = append(s.Steps, toM(s, authored{
				key: "TH" + trimLevel(l), treatment: "TH", level: l, rep: 1,
				vG: vrm.Scale(mVrm).Add(chrm.Scale(mCh)).Add(jit), covG: covFrom(goodCov(g)),
			}))
		}
	}

	// S3: single AF component with a discordant replicate at 30 mT.
	{
		s := specs[2]
		g := newLCG(303)
		u := geom.FromDir(300, 20)
		type lv struct {
			l   float64
			mag float64
		}
		levels := []lv{{0, 11}, {10, 9.4}, {20, 7.8}, {30, 6.3}, {40, 4.9}, {60, 3.1}}
		for _, e := range levels {
			reps := 1
			if e.l == 30 {
				reps = 2
			}
			for rep := 1; rep <= reps; rep++ {
				jit := geom.Vec3{X: g.gauss() * noiseScale, Y: g.gauss() * noiseScale, Z: g.gauss() * noiseScale}
				v := u.Scale(e.mag).Add(jit)
				if e.l == 30 && rep == 2 {
					// Independent re-measurement with anomalous loading:
					// large perpendicular offset, same nominal treatment level.
					v = v.Add(geom.Vec3{X: 1.35, Y: -0.55, Z: 0.7})
				}
				key := "AF" + trimLevel(e.l)
				if reps > 1 {
					key = key + "-R" + string(rune('0'+rep))
				}
				s.Steps = append(s.Steps, toM(s, authored{
					key: key, treatment: "AF", level: e.l, rep: rep,
					vG: v, covG: covFrom(goodCov(g)),
				}))
			}
		}
	}

	out := make([]Specimen, len(specs))
	for i, s := range specs {
		out[i] = *s
	}
	return out
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func trimLevel(l float64) string {
	if math.Mod(l, 1) == 0 {
		return strconv.Itoa(int(l))
	}
	return strconv.FormatFloat(l, 'f', -1, 64)
}
