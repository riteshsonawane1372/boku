package render

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// Chart text sizes, in canvas units (the canvas is 640 wide).
const (
	fsAxis  = 10.5
	fsValue = 10.5
	fsLabel = 11.0
)

// ChartSVG renders a chart as inline SVG. Output is a pure function of the
// data and the theme.
func ChartSVG(c *report.Chart, th Theme) string {
	cv := chartCanvas(c, th)
	if cv == nil {
		return ""
	}
	return cv.SVG()
}

// chartCanvas draws a chart; nil when there is nothing to draw.
func chartCanvas(c *report.Chart, th Theme) *canvas {
	if c == nil || len(c.Labels) == 0 || len(c.Series) == 0 {
		return nil
	}
	switch c.Kind {
	case "bar":
		return barChart(c, th)
	case "line", "area":
		return lineChart(c, th, c.Kind == "area")
	case "donut":
		return donutChart(c, th)
	case "stacked":
		return columnChart(c, th, true)
	default:
		return columnChart(c, th, false)
	}
}

func polar(cx, cy, r, deg float64) pt {
	a := deg * math.Pi / 180
	return pt{cx + r*math.Cos(a), cy + r*math.Sin(a)}
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

// legend draws the series key from (x, y) rightwards and reports whether it
// drew one (single-series charts need none).
func legend(cv *canvas, c *report.Chart, th Theme, x, y float64) bool {
	if len(c.Series) < 2 {
		return false
	}
	for i, se := range c.Series {
		cv.rect(x, y-9, 10, 10, 3, th.series(i))
		cv.text(x+15, y, "start", th.Body, fsAxis, 500, se.Name)
		x += 15 + textWidth(se.Name, fsAxis) + 20
	}
	return true
}

// textWidth approximates rendered width for the report's sans-serif face.
func textWidth(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case r == ' ' || r == '.' || r == ',' || r == 'i' || r == 'l' || r == 'I' || r == '1' || r == 'j' || r == ':' || r == '\'':
			w += 0.3
		case r >= 'A' && r <= 'Z', r == 'm', r == 'w', r == 'M', r == 'W', r == '%', r == '@':
			w += 0.72
		default:
			w += 0.57
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

// yGrid draws horizontal gridlines with their values and returns nothing;
// the zero line is drawn darker so bars have a baseline.
func yGrid(cv *canvas, th Theme, min, max, step, left, right float64, y func(float64) float64) {
	for v := min; v <= max+step/2; v += step {
		yy := y(v)
		stroke, sw := th.Rule, 0.8
		if math.Abs(v) < step/1e6 {
			stroke, sw = th.Faint, 1
		}
		cv.line(left, yy, right, yy, stroke, sw)
		cv.text(left-8, yy+3.6, "end", th.Muted, fsAxis, 400, fmtNum(v))
	}
}

// columnChart draws vertical bars: series side by side, or stacked.
func columnChart(c *report.Chart, th Theme, stacked bool) *canvas {
	const W, H = 640.0, 330.0
	top, bottom, left, right := 18.0, 58.0, 50.0, 10.0
	cv := newCanvas("chart", W, H)
	if legend(cv, c, th, left, 14) {
		top = 42
	}
	n, ns := len(c.Labels), len(c.Series)
	lo, hi := dataRange(c)
	if stacked {
		lo, hi = 0, 0
		for i := range c.Labels {
			sum := 0.0
			for _, se := range c.Series {
				sum += se.Values[i]
			}
			hi = math.Max(hi, sum)
		}
	}
	min, max, step := niceScale(lo, hi, 5)
	ph, pw := H-top-bottom, W-left-right
	y := func(v float64) float64 { return top + ph*(max-v)/(max-min) }
	yGrid(cv, th, min, max, step, left, W-right, y)

	band := pw / float64(n)
	gap := math.Max(band*0.3, 6)
	groupW := math.Min(band-gap, 96)
	x0off := (band - groupW) / 2
	for i, label := range c.Labels {
		x0 := left + float64(i)*band + x0off
		if stacked {
			base := 0.0
			for j, se := range c.Series {
				v := se.Values[i]
				if v <= 0 {
					continue
				}
				y0, y1 := y(base+v), y(base)
				cv.rect(x0, y0, groupW, math.Max(y1-y0-1, 0.5), 2, th.series(j))
				base += v
			}
			if n <= 14 {
				cv.text(x0+groupW/2, y(base)-6, "middle", th.Ink, fsValue, 600, fmtNum(base))
			}
		} else {
			bw := groupW / float64(ns)
			for j, se := range c.Series {
				v := se.Values[i]
				y0, y1 := y(math.Max(v, 0)), y(math.Min(v, 0))
				x := x0 + float64(j)*bw
				cv.rect(x, y0, math.Max(bw-2, 1), math.Max(y1-y0, 0.5), math.Min(3, bw/3), th.series(j))
				if n*ns <= 16 {
					ty := y0 - 6
					if v < 0 {
						ty = y1 + 13
					}
					cv.text(x+(bw-2)/2, ty, "middle", th.Ink, fsValue, 600, fmtNum(v))
				}
			}
		}
		for k, line := range wrap(label, band-6, fsAxis, 3) {
			cv.text(left+float64(i)*band+band/2, H-bottom+18+float64(k)*12.5, "middle", th.Muted, fsAxis, 400, line)
		}
	}
	return cv
}

func barChart(c *report.Chart, th Theme) *canvas {
	const W = 640.0
	labelW := 0.0
	for _, l := range c.Labels {
		labelW = math.Max(labelW, textWidth(l, fsLabel))
	}
	labelW = math.Min(math.Max(labelW, 60), 200)
	ns := len(c.Series)
	barH := 16.0
	if ns > 1 {
		barH = 12
	}
	rowH := (barH+2)*float64(ns) + 12
	top := 8.0
	H := top + rowH*float64(len(c.Labels)) + 26
	if ns > 1 {
		top, H = 36, H+28
	}
	cv := newCanvas("chart", W, H)
	left, right := labelW+16, 52.0
	legend(cv, c, th, left, 14)
	lo, hi := dataRange(c)
	min, max, step := niceScale(lo, hi, 5)
	pw := W - left - right
	x := func(v float64) float64 { return left + pw*(v-min)/(max-min) }
	bottom := H - 22
	for v := min; v <= max+step/2; v += step {
		xx := x(v)
		stroke, sw := th.Rule, 0.8
		if math.Abs(v) < step/1e6 {
			stroke, sw = th.Faint, 1
		}
		cv.line(xx, top, xx, bottom, stroke, sw)
		cv.text(xx, H-6, "middle", th.Muted, fsAxis, 400, fmtNum(v))
	}
	for i, label := range c.Labels {
		y0 := top + float64(i)*rowH + 6
		lines := wrap(label, labelW, fsLabel, 2)
		ly := y0 + (rowH-12)/2 + 4 - float64(len(lines)-1)*6.5
		for k, line := range lines {
			cv.text(labelW+4, ly+float64(k)*13, "end", th.Ink, fsLabel, 500, line)
		}
		for j, se := range c.Series {
			v := se.Values[i]
			x0, x1 := x(math.Min(v, 0)), x(math.Max(v, 0))
			yy := y0 + float64(j)*(barH+2)
			cv.rect(x0, yy, math.Max(x1-x0, 0.5), barH, 3, th.series(j))
			if v >= 0 {
				cv.text(x1+6, yy+barH/2+3.8, "start", th.Ink, fsValue, 600, fmtNum(v))
			} else {
				cv.text(x0-6, yy+barH/2+3.8, "end", th.Ink, fsValue, 600, fmtNum(v))
			}
		}
	}
	return cv
}

// lineChart draws series over ordered periods. With fill, or for a single
// series, the area under the line is tinted.
func lineChart(c *report.Chart, th Theme, fill bool) *canvas {
	const W, H = 640.0, 320.0
	top, bottom, left, right := 22.0, 40.0, 50.0, 44.0
	cv := newCanvas("chart", W, H)
	if legend(cv, c, th, left, 14) {
		top = 44
	}
	lo, hi := dataRange(c)
	// Line charts need not start at zero, but keep zero when data is close to it.
	min, max, step := niceScale(lo, hi, 5)
	if lo > 0 && lo > (hi-lo)*1.5 && !fill {
		min, max, step = niceScaleFree(lo, hi, 5)
	}
	ph, pw := H-top-bottom, W-left-right
	n := len(c.Labels)
	xAt := func(i int) float64 {
		if n == 1 {
			return left + pw/2
		}
		return left + 10 + (pw-20)*float64(i)/float64(n-1)
	}
	y := func(v float64) float64 { return top + ph*(max-v)/(max-min) }
	yGrid(cv, th, min, max, step, left, W-right, y)
	every := int(math.Ceil(float64(n) / 10))
	for i, label := range c.Labels {
		if i%every == 0 || (i == n-1 && i%every > every/2) {
			cv.text(xAt(i), H-bottom+19, "middle", th.Muted, fsAxis, 400, truncateRunes(label, 14))
		}
	}
	type endLabel struct {
		y   float64
		txt string
		col string
	}
	var ends []endLabel
	for j, se := range c.Series {
		col := th.series(j)
		pts := make([]pt, len(se.Values))
		for i, v := range se.Values {
			pts[i] = pt{xAt(i), y(v)}
		}
		if (fill || len(c.Series) == 1) && n > 1 {
			base := y(math.Max(min, math.Min(0, max)))
			area := append([]pt{{pts[0].x, base}}, pts...)
			area = append(area, pt{pts[n-1].x, base})
			op := 0.1
			if fill {
				op = 0.18
			}
			cv.polygon(area, col, op)
		}
		cv.polyline(pts, col, 2.4)
		if n <= 16 {
			for _, p := range pts {
				d := cv.circle(p.x, p.y, 3.4, col)
				d.stroke, d.sw = "#ffffff", 1.4
			}
		}
		if n > 0 && n <= 30 {
			ends = append(ends, endLabel{pts[n-1].y, fmtNum(se.Values[n-1]), col})
		}
	}
	// End-of-line values sit to the right of the last point, nudged apart.
	sort.SliceStable(ends, func(a, b int) bool { return ends[a].y < ends[b].y })
	for i := range ends {
		if i > 0 && ends[i].y-ends[i-1].y < 13 {
			ends[i].y = ends[i-1].y + 13
		}
		cv.text(xAt(n-1)+9, ends[i].y+3.8, "start", mix(ends[i].col, th.Ink, 0.25), fsValue, 700, ends[i].txt)
	}
	return cv
}

// donutChart draws the first series as shares of its total.
func donutChart(c *report.Chart, th Theme) *canvas {
	n := len(c.Labels)
	const W = 640.0
	H := math.Max(250, float64(n)*26+60)
	cv := newCanvas("chart", W, H)
	vals := c.Series[0].Values
	total := 0.0
	for _, v := range vals {
		total += math.Max(v, 0)
	}
	cx, cy, r, r0 := 150.0, H/2, 104.0, 66.0
	if total <= 0 {
		cv.wedge(cx, cy, r0, r, 0, 359.99, th.Rule)
		return cv
	}
	gap := 1.4
	if n == 1 {
		gap = 0
	}
	a, best := -90.0, 0
	for i, v := range vals {
		if v <= 0 {
			continue
		}
		if v > vals[best] {
			best = i
		}
		sweep := math.Min(v/total*360, 359.99)
		if sweep > gap*2 {
			cv.wedge(cx, cy, r0, r, a+gap/2, a+sweep-gap/2, th.series(i))
		}
		a += sweep
	}
	cv.text(cx, cy+2, "middle", th.Ink, 26, 700, fmtNum(math.Round(vals[best]/total*1000)/10)+"%")
	cv.text(cx, cy+20, "middle", th.Muted, fsAxis, 500, truncateRunes(c.Labels[best], 18))

	// Key: label, value and share per slice.
	lx, rowH := 300.0, 26.0
	y := cy - float64(n)*rowH/2 + 17
	for i, label := range c.Labels {
		cv.rect(lx, y-10, 11, 11, 3, th.series(i))
		cv.text(lx+19, y, "start", th.Ink, fsLabel, 500, wrap(label, 190, fsLabel, 1)[0])
		cv.text(W-70, y, "end", th.Ink, fsLabel, 600, fmtNum(vals[i]))
		cv.text(W-8, y, "end", th.Muted, fsAxis, 400, fmtNum(math.Round(math.Max(vals[i], 0)/total*1000)/10)+"%")
		if i < n-1 {
			cv.line(lx, y+8.5, W-8, y+8.5, th.Rule, 0.8)
		}
		y += rowH
	}
	return cv
}

func niceScaleFree(lo, hi float64, ticks int) (float64, float64, float64) {
	if hi-lo <= 0 {
		return niceScale(lo, hi, ticks)
	}
	rng := niceNum(hi-lo, false)
	step := niceNum(rng/float64(ticks-1), true)
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}
