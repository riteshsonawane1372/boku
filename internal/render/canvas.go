package render

import (
	"fmt"
	"html"
	"strings"
)

// canvas is a drawing made of simple shapes. Charts and diagrams draw onto a
// canvas once; it is then written as inline SVG for the report and as native
// DrawingML shapes for the slide deck, so both show the same figure.
type canvas struct {
	W, H  float64
	class string // svg class: chart | diagram
	ops   []*shape
}

type pt struct{ x, y float64 }

const (
	opRect = iota
	opLine
	opPolyline
	opPolygon
	opCircle
	opText
	opCurve // cubic Bézier: pts[0] start, pts[1..2] controls, pts[3] end
	opWedge // ring segment between radii r0..r and angles a0..a1 (degrees, clockwise from 3 o'clock)
)

type shape struct {
	kind       int
	x, y, w, h float64 // rect; circle and wedge centre (x, y); text anchor point (x, baseline y)
	r, r0      float64
	a0, a1     float64
	pts        []pt
	fill       string // "" = none
	stroke     string
	sw         float64
	opacity    float64 // fill opacity; 0 means opaque
	arrow      bool
	text       string
	anchor     string // start | middle | end
	size       float64
	weight     int
}

func newCanvas(class string, w, h float64) *canvas { return &canvas{W: w, H: h, class: class} }

func (c *canvas) add(s *shape) *shape { c.ops = append(c.ops, s); return s }

func (c *canvas) rect(x, y, w, h, r float64, fill string) *shape {
	return c.add(&shape{kind: opRect, x: x, y: y, w: w, h: h, r: r, fill: fill})
}

func (c *canvas) line(x1, y1, x2, y2 float64, stroke string, sw float64) *shape {
	return c.add(&shape{kind: opLine, pts: []pt{{x1, y1}, {x2, y2}}, stroke: stroke, sw: sw})
}

func (c *canvas) polyline(pts []pt, stroke string, sw float64) *shape {
	return c.add(&shape{kind: opPolyline, pts: pts, stroke: stroke, sw: sw})
}

func (c *canvas) polygon(pts []pt, fill string, opacity float64) *shape {
	return c.add(&shape{kind: opPolygon, pts: pts, fill: fill, opacity: opacity})
}

func (c *canvas) circle(x, y, r float64, fill string) *shape {
	return c.add(&shape{kind: opCircle, x: x, y: y, r: r, fill: fill})
}

func (c *canvas) text(x, y float64, anchor, fill string, size float64, weight int, txt string) *shape {
	return c.add(&shape{kind: opText, x: x, y: y, anchor: anchor, fill: fill, size: size, weight: weight, text: txt})
}

func (c *canvas) curve(p0, p1, p2, p3 pt, stroke string, sw float64) *shape {
	return c.add(&shape{kind: opCurve, pts: []pt{p0, p1, p2, p3}, stroke: stroke, sw: sw})
}

func (c *canvas) wedge(x, y, r0, r, a0, a1 float64, fill string) *shape {
	return c.add(&shape{kind: opWedge, x: x, y: y, r0: r0, r: r, a0: a0, a1: a1, fill: fill})
}

// SVG writes the canvas as an inline SVG element.
func (c *canvas) SVG() string {
	var b strings.Builder
	f := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	f(`<svg class="%s" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, c.class, c.W, c.H)
	arrows := map[string]bool{}
	for _, s := range c.ops {
		if s.arrow && !arrows[s.stroke] {
			arrows[s.stroke] = true
			f(`<defs><marker id="arrow-%s" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="%s"/></marker></defs>`, s.stroke[1:], s.stroke)
		}
	}
	paint := func(s *shape) string {
		var p strings.Builder
		fill := s.fill
		if fill == "" {
			fill = "none"
		}
		fmt.Fprintf(&p, ` fill="%s"`, fill)
		if s.opacity > 0 && s.opacity < 1 {
			fmt.Fprintf(&p, ` fill-opacity="%.2f"`, s.opacity)
		}
		if s.stroke != "" {
			fmt.Fprintf(&p, ` stroke="%s" stroke-width="%.1f" stroke-linejoin="round" stroke-linecap="round"`, s.stroke, s.sw)
		}
		if s.arrow {
			fmt.Fprintf(&p, ` marker-end="url(#arrow-%s)"`, s.stroke[1:])
		}
		return p.String()
	}
	points := func(ps []pt) string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = fmt.Sprintf("%.1f,%.1f", p.x, p.y)
		}
		return strings.Join(out, " ")
	}
	for _, s := range c.ops {
		switch s.kind {
		case opRect:
			f(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f"`, s.x, s.y, s.w, s.h)
			if s.r > 0 {
				f(` rx="%.1f"`, s.r)
			}
			f(`%s/>`, paint(s))
		case opLine:
			f(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"%s/>`, s.pts[0].x, s.pts[0].y, s.pts[1].x, s.pts[1].y, paint(s))
		case opPolyline:
			f(`<polyline points="%s"%s/>`, points(s.pts), paint(s))
		case opPolygon:
			f(`<polygon points="%s"%s/>`, points(s.pts), paint(s))
		case opCircle:
			f(`<circle cx="%.1f" cy="%.1f" r="%.1f"%s/>`, s.x, s.y, s.r, paint(s))
		case opText:
			f(`<text x="%.1f" y="%.1f" text-anchor="%s" fill="%s" font-size="%.1f" font-weight="%d">%s</text>`,
				s.x, s.y, s.anchor, s.fill, s.size, s.weight, html.EscapeString(s.text))
		case opCurve:
			p := s.pts
			f(`<path d="M%.1f,%.1f C%.1f,%.1f %.1f,%.1f %.1f,%.1f"%s/>`, p[0].x, p[0].y, p[1].x, p[1].y, p[2].x, p[2].y, p[3].x, p[3].y, paint(s))
		case opWedge:
			f(`<path d="%s"%s/>`, wedgePath(s), paint(s))
		}
	}
	f(`</svg>`)
	return b.String()
}

// wedgePath is the SVG path of a ring segment.
func wedgePath(s *shape) string {
	o0, o1 := polar(s.x, s.y, s.r, s.a0), polar(s.x, s.y, s.r, s.a1)
	i0, i1 := polar(s.x, s.y, s.r0, s.a0), polar(s.x, s.y, s.r0, s.a1)
	large := 0
	if s.a1-s.a0 > 180 {
		large = 1
	}
	return fmt.Sprintf("M%.2f,%.2f A%.1f,%.1f 0 %d 1 %.2f,%.2f L%.2f,%.2f A%.1f,%.1f 0 %d 0 %.2f,%.2f Z",
		o0.x, o0.y, s.r, s.r, large, o1.x, o1.y, i1.x, i1.y, s.r0, s.r0, large, i0.x, i0.y)
}
