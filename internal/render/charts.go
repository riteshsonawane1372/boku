package render

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// Series colours: one accent family, then neutrals. Restrained on purpose.
var palette = []string{"#0d3b66", "#4f7cac", "#9db8d6", "#b07d2b", "#5d6677", "#c9ced6"}

const (
	colInk   = "#161b26"
	colMuted = "#5d6677"
	colGrid  = "#e3e6eb"
	colAxis  = "#8a92a0"
)

// ChartSVG renders a chart as inline SVG. Output is a pure function of the data.
func ChartSVG(c *report.Chart) string {
	if c == nil {
		return ""
	}
	switch c.Kind {
	case "bar":
		return barChart(c)
	case "line":
		return lineChart(c)
	default:
		return columnChart(c)
	}
}

type svgBuf struct{ strings.Builder }

func (s *svgBuf) f(format string, args ...any) { fmt.Fprintf(&s.Builder, format, args...) }

func (s *svgBuf) text(x, y float64, anchor, fill string, size float64, weight int, txt string) {
	s.f(`<text x="%.1f" y="%.1f" text-anchor="%s" fill="%s" font-size="%.1f" font-weight="%d">%s</text>`,
		x, y, anchor, fill, size, weight, html.EscapeString(txt))
}

// niceScale returns axis bounds and a tick step covering [lo, hi].
func niceScale(lo, hi float64, ticks int) (float64, float64, float64) {
	if lo > 0 {
		lo = 0
	}
	if hi < 0 {
		hi = 0
	}
	if hi == lo {
		hi = lo + 1
	}
	rng := niceNum(hi-lo, false)
	step := niceNum(rng/float64(ticks-1), true)
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}

func niceNum(x float64, round bool) float64 {
	exp := math.Floor(math.Log10(x))
	f := x / math.Pow(10, exp)
	var nf float64
	switch {
	case round && f < 1.5:
		nf = 1
	case round && f < 3:
		nf = 2
	case round && f < 7:
		nf = 5
	case round:
		nf = 10
	case f <= 1:
		nf = 1
	case f <= 2:
		nf = 2
	case f <= 5:
		nf = 5
	default:
		nf = 10
	}
	return nf * math.Pow(10, exp)
}

// fmtNum formats axis and value labels compactly (1,250 · 3.4 · 12.5k).
func fmtNum(v float64) string {
	av := math.Abs(v)
	switch {
	case av >= 1e6:
		return trimZeros(strconv.FormatFloat(v/1e6, 'f', 1, 64)) + "M"
	case av >= 1e4:
		return trimZeros(strconv.FormatFloat(v/1e3, 'f', 1, 64)) + "k"
	case av >= 1000:
		return withCommas(int64(math.Round(v)))
	case av == math.Trunc(av):
		return strconv.FormatFloat(v, 'f', 0, 64)
	case av >= 10:
		return trimZeros(strconv.FormatFloat(v, 'f', 1, 64))
	default:
		return trimZeros(strconv.FormatFloat(v, 'f', 2, 64))
	}
}

func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func withCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func dataRange(c *report.Chart) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range c.Series {
		for _, v := range s.Values {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	return lo, hi
}

func legend(s *svgBuf, c *report.Chart, x, y float64) {
	if len(c.Series) < 2 {
		return
	}
	for i, se := range c.Series {
		s.f(`<rect x="%.1f" y="%.1f" width="9" height="9" fill="%s"/>`, x, y-8, palette[i%len(palette)])
		s.text(x+13, y, "start", colMuted, 10, 400, se.Name)
		x += 13 + textWidth(se.Name, 10) + 18
	}
}

// textWidth approximates rendered width for a sans-serif font.
func textWidth(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case r == ' ' || r == '.' || r == ',' || r == 'i' || r == 'l' || r == 'I' || r == '1':
			w += 0.3
		case r >= 'A' && r <= 'Z', r == 'm', r == 'w', r == 'M', r == 'W':
			w += 0.68
		default:
			w += 0.54
		}
	}
	return w * size
}

// wrap breaks s into at most maxLines lines of roughly width px.
func wrap(s string, width, size float64, maxLines int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		try := strings.TrimSpace(cur + " " + w)
		if textWidth(try, size) > width && cur != "" {
			lines = append(lines, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := []rune(lines[maxLines-1])
		for len(last) > 1 && textWidth(string(last)+"…", size) > width {
			last = last[:len(last)-1]
		}
		lines[maxLines-1] = strings.TrimSpace(string(last)) + "…"
	}
	return lines
}

func columnChart(c *report.Chart) string {
	const W, H = 640.0, 300.0
	top, bottom, left, right := 24.0, 58.0, 48.0, 8.0
	if len(c.Series) > 1 {
		top = 34
	}
	lo, hi := dataRange(c)
	min, max, step := niceScale(lo, hi, 5)
	ph, pw := H-top-bottom, W-left-right
	y := func(v float64) float64 { return top + ph*(max-v)/(max-min) }

	var s svgBuf
	s.f(`<svg class="chart" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H)
	legend(&s, c, left, 12)
	for v := min; v <= max+step/2; v += step {
		yy := y(v)
		stroke := colGrid
		if math.Abs(v) < step/1e6 {
			stroke = colAxis
		}
		s.f(`<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="0.8"/>`, left, W-right, yy, yy, stroke)
		s.text(left-6, yy+3.5, "end", colMuted, 9.5, 400, fmtNum(v))
	}
	n := len(c.Labels)
	ns := len(c.Series)
	band := pw / float64(n)
	gap := band * 0.28
	bw := (band - gap) / float64(ns)
	showValues := n*ns <= 16
	for i, label := range c.Labels {
		x0 := left + float64(i)*band + gap/2
		for j, se := range c.Series {
			v := se.Values[i]
			y0, y1 := y(math.Max(v, 0)), y(math.Min(v, 0))
			x := x0 + float64(j)*bw
			s.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, x, y0, math.Max(bw-1, 1), math.Max(y1-y0, 0.5), palette[j%len(palette)])
			if showValues {
				ty := y0 - 4
				if v < 0 {
					ty = y1 + 11
				}
				s.text(x+bw/2, ty, "middle", colInk, 9, 600, fmtNum(v))
			}
		}
		for k, line := range wrap(label, band-4, 9.5, 3) {
			s.text(left+float64(i)*band+band/2, H-bottom+15+float64(k)*11.5, "middle", colMuted, 9.5, 400, line)
		}
	}
	s.f(`</svg>`)
	return s.String()
}

func barChart(c *report.Chart) string {
	const W = 640.0
	labelW := 0.0
	for _, l := range c.Labels {
		labelW = math.Max(labelW, textWidth(l, 9.5))
	}
	labelW = math.Min(math.Max(labelW, 60), 190)
	ns := len(c.Series)
	rowH := 14.0*float64(ns) + 10
	top := 8.0
	if ns > 1 {
		top = 28
	}
	H := top + rowH*float64(len(c.Labels)) + 22
	left, right := labelW+14, 44.0
	lo, hi := dataRange(c)
	min, max, step := niceScale(lo, hi, 5)
	pw := W - left - right
	x := func(v float64) float64 { return left + pw*(v-min)/(max-min) }

	var s svgBuf
	s.f(`<svg class="chart" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H)
	legend(&s, c, left, 12)
	bottom := H - 18
	for v := min; v <= max+step/2; v += step {
		xx := x(v)
		stroke := colGrid
		if math.Abs(v) < step/1e6 {
			stroke = colAxis
		}
		s.f(`<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="0.8"/>`, xx, xx, top, bottom, stroke)
		s.text(xx, H-5, "middle", colMuted, 9.5, 400, fmtNum(v))
	}
	for i, label := range c.Labels {
		y0 := top + float64(i)*rowH + 5
		lines := wrap(label, labelW, 9.5, 2)
		ly := y0 + (rowH-10)/2 + 3.5 - float64(len(lines)-1)*5.5
		for k, line := range lines {
			s.text(labelW, ly+float64(k)*11, "end", colInk, 9.5, 400, line)
		}
		for j, se := range c.Series {
			v := se.Values[i]
			x0, x1 := x(math.Min(v, 0)), x(math.Max(v, 0))
			yy := y0 + float64(j)*14
			s.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="11" fill="%s"/>`, x0, yy, math.Max(x1-x0, 0.5), palette[j%len(palette)])
			if v >= 0 {
				s.text(x1+4, yy+8.5, "start", colInk, 9, 600, fmtNum(v))
			} else {
				s.text(x0-4, yy+8.5, "end", colInk, 9, 600, fmtNum(v))
			}
		}
	}
	s.f(`</svg>`)
	return s.String()
}

func lineChart(c *report.Chart) string {
	const W, H = 640.0, 290.0
	top, bottom, left, right := 24.0, 40.0, 48.0, 16.0
	if len(c.Series) > 1 {
		top = 34
	}
	lo, hi := dataRange(c)
	// Line charts need not start at zero, but keep zero when data is close to it.
	min, max, step := niceScale(lo, hi, 5)
	if lo > 0 && lo > (hi-lo)*1.5 {
		min, max, step = niceScaleFree(lo, hi, 5)
	}
	ph, pw := H-top-bottom, W-left-right
	n := len(c.Labels)
	xAt := func(i int) float64 {
		if n == 1 {
			return left + pw/2
		}
		return left + 8 + (pw-16)*float64(i)/float64(n-1)
	}
	y := func(v float64) float64 { return top + ph*(max-v)/(max-min) }

	var s svgBuf
	s.f(`<svg class="chart" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H)
	legend(&s, c, left, 12)
	for v := min; v <= max+step/2; v += step {
		yy := y(v)
		s.f(`<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="0.8"/>`, left, W-right, yy, yy, colGrid)
		s.text(left-6, yy+3.5, "end", colMuted, 9.5, 400, fmtNum(v))
	}
	every := int(math.Ceil(float64(n) / 12))
	for i, label := range c.Labels {
		if i%every == 0 || i == n-1 {
			s.text(xAt(i), H-bottom+16, "middle", colMuted, 9.5, 400, truncateRunes(label, 14))
		}
	}
	for j, se := range c.Series {
		col := palette[j%len(palette)]
		var pts []string
		for i, v := range se.Values {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", xAt(i), y(v)))
		}
		s.f(`<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round"/>`, strings.Join(pts, " "), col)
		for i, v := range se.Values {
			s.f(`<circle cx="%.1f" cy="%.1f" r="2.6" fill="%s"/>`, xAt(i), y(v), col)
		}
		if last := len(se.Values) - 1; last >= 0 && n <= 24 {
			s.text(xAt(last), y(se.Values[last])-8, "middle", colInk, 9, 600, fmtNum(se.Values[last]))
		}
	}
	s.f(`</svg>`)
	return s.String()
}

func niceScaleFree(lo, hi float64, ticks int) (float64, float64, float64) {
	if hi-lo <= 0 {
		return niceScale(lo, hi, ticks)
	}
	rng := niceNum(hi-lo, false)
	step := niceNum(rng/float64(ticks-1), true)
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}
