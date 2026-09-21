package web

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"paleobench/internal/geomag"
)

const (
	svgW   = 460
	svgH   = 460
	margin = 46
)

// writeSVG emits an SVG document.
func writeSVG(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="Helvetica,Arial,sans-serif" font-size="11">
<rect width="100%%" height="100%%" fill="white"/>
%s</svg>`, svgW, svgH, svgW, svgH, body)
}

// ZijderveldSVG renders horizontal (N-E) and vertical (N-down) plots sharing
// an equal scale so angles are visually honest.
func ZijderveldSVG(h, v []geomag.ZijdPoint, title string) string {
	// Unified scale across both planes.
	scale := 1.0
	for _, p := range h {
		scale = math.Max(scale, math.Max(math.Abs(p.HX), math.Abs(p.HY)))
	}
	for _, p := range v {
		scale = math.Max(scale, math.Max(math.Abs(p.HX), math.Abs(p.HY)))
	}
	scale *= 1.15

	cx := float64(svgW / 2)
	cy := float64(svgH / 2)
	R := float64(svgW/2 - margin)
	toXY := func(a, b float64) (float64, float64) {
		return cx + a/scale*R, cy + b/scale*R
	}
	var s string
	s += fmt.Sprintf(`<text x="%d" y="20" font-size="14" font-weight="bold">%s</text>`, 12, svgEscape(title))

	// Axes.
	s += fmt.Sprintf(`<line x1="%0.1f" y1="%0.1f" x2="%0.1f" y2="%0.1f" stroke="#999"/>`,
		cx-R, cy, cx+R, cy)
	s += fmt.Sprintf(`<line x1="%0.1f" y1="%0.1f" x2="%0.1f" y2="%0.1f" stroke="#999"/>`,
		cx, cy-R, cx, cy+R)
	// scale ring
	s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="%0.1f" fill="none" stroke="#eee"/>`, cx, cy, R)

	// Vertical plane: N vs Down (filled square).
	s += polyline(v, toXY, "#1f5fbf")
	for _, p := range v {
		x, y := toXY(p.HX, p.HY)
		s += fmt.Sprintf(`<rect x="%0.1f" y="%0.1f" width="7" height="7" fill="#1f5fbf"/>`, x-3.5, y-3.5)
	}
	// Horizontal plane: N vs East (open circle).
	s += polyline(h, toXY, "#d1495b")
	for _, p := range h {
		x, y := toXY(p.HX, p.HY)
		s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="3.6" fill="white" stroke="#d1495b" stroke-width="1.6"/>`, x, y)
	}
	// Origin marker.
	ox, oy := toXY(0, 0)
	s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="2.6" fill="black"/>`, ox, oy)

	// Labels for first/last points.
	if len(h) > 0 {
		x0, y0 := toXY(h[0].HX, h[0].HY)
		x1, y1 := toXY(h[len(h)-1].HX, h[len(h)-1].HY)
		s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f">%d</text>`, x0+6, y0-6, h[0].Seq)
		s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f">%d</text>`, x1+6, y1-6, h[len(h)-1].Seq)
	}
	// Axis legend.
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f" fill="#555">+N</text>`, cx-R, cy-5)
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f" fill="#555">+E / +Down</text>`, cx+6, cy+R+14)
	s += `<rect x="12" y="410" width="10" height="10" fill="#1f5fbf"/><text x="28" y="419">垂直面 N-Down (实心)</text>`
	s += `<circle cx="210" cy="415" r="4" fill="white" stroke="#d1495b" stroke-width="1.6"/><text x="220" y="419">水平面 N-E (空心)</text>`
	return s
}

func polyline(pts []geomag.ZijdPoint, toXY func(float64, float64) (float64, float64), color string) string {
	if len(pts) == 0 {
		return ""
	}
	d := ""
	for i, p := range pts {
		x, y := toXY(p.HX, p.HY)
		if i == 0 {
			d += fmt.Sprintf("M%.1f,%.1f", x, y)
		} else {
			d += fmt.Sprintf(" L%.1f,%.1f", x, y)
		}
	}
	return fmt.Sprintf(`<path d="%s" fill="none" stroke="%s" stroke-width="1.5"/>`, d, color)
}

// StereonetSVG renders an equal-angle lower-hemisphere net.
func StereonetSVG(pts []geomag.StereoPoint, title string) string {
	cx := float64(svgW / 2)
	cy := float64(svgH / 2)
	R := float64(svgW/2 - margin)
	var s string
	s += fmt.Sprintf(`<text x="12" y="20" font-size="14" font-weight="bold">%s</text>`, svgEscape(title))
	s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="%0.1f" fill="white" stroke="#444"/>`, cx, cy, R)
	// 10-degree concentric nets.
	for inc := 10; inc < 90; inc += 10 {
		theta := math.Pi/2 - geomag.Deg(float64(inc))
		r := math.Tan(theta/2) * R
		s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="%0.1f" fill="none" stroke="#eee"/>`, cx, cy, r)
	}
	// Cardinal axes.
	s += fmt.Sprintf(`<line x1="%0.1f" y1="%0.1f" x2="%0.1f" y2="%0.1f" stroke="#ddd"/>`, cx-R, cy, cx+R, cy)
	s += fmt.Sprintf(`<line x1="%0.1f" y1="%0.1f" x2="%0.1f" y2="%0.1f" stroke="#ddd"/>`, cx, cy-R, cx, cy+R)
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f" text-anchor="middle">N</text>`, cx, cy-R-6)
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f" text-anchor="middle">S</text>`, cx, cy+R+16)
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f">E</text>`, cx+R+8, cy+4)
	s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f" text-anchor="end">W</text>`, cx-R-8, cy+4)

	for i, p := range pts {
		x := cx + p.X*R
		y := cy + p.Y*R
		if p.Upper {
			s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="4" fill="white" stroke="#2a9d8f" stroke-width="1.6"/>`, x, y)
		} else {
			s += fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="4" fill="#264653"/>`, x, y)
		}
		s += fmt.Sprintf(`<text x="%0.1f" y="%0.1f">%d</text>`, x+6, y-5, p.Seq)
		_ = i
	}
	s += `<circle cx="18" cy="415" r="4" fill="#264653"/><text x="28" y="419">下半球 (实心)</text>`
	s += `<circle cx="150" cy="415" r="4" fill="white" stroke="#2a9d8f" stroke-width="1.6"/><text x="160" y="419">上半球 (空心)</text>`
	return s
}

func (s *Server) loadSpecimenOr404(w http.ResponseWriter, r *http.Request, id int64) *SpecimenData {
	data, err := s.Svc.LoadSpecimen(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return nil
	}
	return data
}

func (s *Server) handleZijdSVG(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	data := s.loadSpecimenOr404(w, r, id)
	if data == nil {
		return
	}
	writeSVG(w, ZijderveldSVG(data.ZijH, data.ZijV,
		fmt.Sprintf("Zijderveld — %s (%s)", data.Specimen.Code, frameLabel(data.Frame))))
}

func (s *Server) handleStereoSVG(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	data := s.loadSpecimenOr404(w, r, id)
	if data == nil {
		return
	}
	writeSVG(w, StereonetSVG(data.Stereo,
		fmt.Sprintf("球面投影 — %s (%s)", data.Specimen.Code, frameLabel(data.Frame))))
}

func (s *Server) handleCandidateZijdSVG(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c, err := s.Svc.Store.GetCandidate(r.Context(), id)
	if err != nil || c == nil {
		writeErr(w, 404, "candidate not found")
		return
	}
	data := s.loadSpecimenOr404(w, r, c.SpecimenID)
	if data == nil {
		return
	}
	included := map[int]bool{}
	// Rebuild views from the candidate frame and highlight the window.
	gsteps := data.Steps
	views := geomag.TransformSteps(gsteps, data.Bedding, c.Frame)
	var winH, winV []geomag.ZijdPoint
	for _, sv := range views {
		if sv.Seq >= c.FromSeq && sv.Seq <= c.ToSeq {
			winH = append(winH, geomag.ZijdPoint{Seq: sv.Seq, HX: sv.V.X, HY: sv.V.Y})
			winV = append(winV, geomag.ZijdPoint{Seq: sv.Seq, HX: sv.V.X, HY: sv.V.Z})
			included[sv.Seq] = true
		}
	}
	zh, zv := geomag.Zijderveld(views)
	body := ZijderveldSVG(zh, zv,
		fmt.Sprintf("Zijderveld — 候选窗 #%d (%s)", id, frameLabel(c.Frame)))
	// Overlay window points in green using same scale as full plot.
	scale := 1.0
	for _, p := range zh {
		scale = math.Max(scale, math.Max(math.Abs(p.HX), math.Abs(p.HY)))
	}
	for _, p := range zv {
		scale = math.Max(scale, math.Max(math.Abs(p.HX), math.Abs(p.HY)))
	}
	scale *= 1.15
	cx := float64(svgW / 2)
	cy := float64(svgH / 2)
	Rr := float64(svgW/2 - margin)
	mark := func(a, b float64, filled bool) string {
		x := cx + a/scale*Rr
		y := cy + b/scale*Rr
		if filled {
			return fmt.Sprintf(`<rect x="%0.1f" y="%0.1f" width="9" height="9" fill="#2a9d8f" opacity="0.95"/>`, x-4.5, y-4.5)
		}
		return fmt.Sprintf(`<circle cx="%0.1f" cy="%0.1f" r="5" fill="#e9c46a" stroke="#2a9d8f" stroke-width="1.8"/>`, x, y)
	}
	extra := ""
	for _, p := range winV {
		extra += mark(p.HX, p.HY, true)
	}
	for _, p := range winH {
		extra += mark(p.HX, p.HY, false)
	}
	// inject extra markers before closing svg by replacing body tail marker
	writeSVG(w, body+extra)
}

func frameLabel(f string) string {
	switch f {
	case geomag.FrameSpec:
		return "标本坐标"
	case geomag.FrameGeo:
		return "地理坐标"
	case geomag.FrameTilt:
		return "倾斜校正"
	}
	return f
}
