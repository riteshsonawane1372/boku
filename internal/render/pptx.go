package render

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// PPTX renders the report as a 16:9 PowerPoint deck.
//
// The deck is written directly as Office Open XML: there is no template file
// and no office suite involved, so the output is a pure function of the
// report. Charts and diagrams are the same drawings as in the PDF, emitted as
// native shapes that stay sharp and editable in PowerPoint, Keynote and
// Google Slides. Each slide's speaker notes carry the full text the slide
// condenses, with its citation numbers.
func PPTX(r *report.Report) ([]byte, error) {
	d := &deck{r: r, th: themeOf(r)}
	d.build()
	return d.pack()
}

// Slide geometry in points (16:9, 13.333in × 7.5in).
const (
	slideW, slideH = 960.0, 540.0
	padX           = 56.0
	bodyW          = slideW - 2*padX
	bodyBottom     = 492.0
	deckFont       = "Arial" // installed everywhere PowerPoint, Keynote and Slides run
)

func emu(pt float64) int { return int(math.Round(pt * 12700)) }

type deck struct {
	r      *report.Report
	th     Theme
	slides []*slide
}

type slide struct {
	b     strings.Builder
	id    int
	notes []string
}

func (d *deck) add() *slide {
	s := &slide{id: 1}
	d.slides = append(d.slides, s)
	return s
}

func (s *slide) next() int { s.id++; return s.id }

// ---------- shapes ----------

// box describes a filled shape. Colours are "#rrggbb"; "" means none.
type box struct {
	fill    string
	alpha   float64  // fill opacity, 0 = opaque
	grad    []string // linear gradient stops; overrides fill
	glow    bool     // radial fade of fill to transparent (cover mesh)
	radius  float64  // corner radius in pt
	ellipse bool
	line    string
	lw      float64
}

func hex(c string) string { return strings.ToUpper(strings.TrimPrefix(c, "#")) }

func solid(c string, alpha float64) string {
	if c == "" {
		return "<a:noFill/>"
	}
	if alpha > 0 && alpha < 1 {
		return fmt.Sprintf(`<a:solidFill><a:srgbClr val="%s"><a:alpha val="%d"/></a:srgbClr></a:solidFill>`, hex(c), int(alpha*100000))
	}
	return fmt.Sprintf(`<a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, hex(c))
}

func (o box) fillXML() string {
	switch {
	case o.glow:
		return fmt.Sprintf(`<a:gradFill rotWithShape="1"><a:gsLst><a:gs pos="0"><a:srgbClr val="%[1]s"><a:alpha val="%[2]d"/></a:srgbClr></a:gs><a:gs pos="100000"><a:srgbClr val="%[1]s"><a:alpha val="0"/></a:srgbClr></a:gs></a:gsLst><a:path path="circle"><a:fillToRect l="50000" t="50000" r="50000" b="50000"/></a:path></a:gradFill>`,
			hex(o.fill), int(o.alpha*100000))
	case len(o.grad) > 1:
		var gs strings.Builder
		for i, c := range o.grad {
			fmt.Fprintf(&gs, `<a:gs pos="%d"><a:srgbClr val="%s"/></a:gs>`, i*100000/(len(o.grad)-1), hex(c))
		}
		return `<a:gradFill rotWithShape="1"><a:gsLst>` + gs.String() + `</a:gsLst><a:lin ang="0" scaled="0"/></a:gradFill>`
	}
	return solid(o.fill, o.alpha)
}

func lineXML(c string, w float64, tail string) string {
	if c == "" {
		return "<a:ln><a:noFill/></a:ln>"
	}
	return fmt.Sprintf(`<a:ln w="%d" cap="rnd">%s<a:round/>%s</a:ln>`, emu(w), solid(c, 0), tail)
}

func xfrm(x, y, w, h float64, flip string) string {
	return fmt.Sprintf(`<a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, flip, emu(x), emu(y), emu(math.Max(w, 0)), emu(math.Max(h, 0)))
}

func geom(o box, w, h float64) string {
	switch {
	case o.ellipse:
		return `<a:prstGeom prst="ellipse"><a:avLst/></a:prstGeom>`
	case o.radius > 0 && math.Min(w, h) > 0:
		adj := int(math.Min(o.radius/math.Min(w, h), 0.5) * 100000)
		return fmt.Sprintf(`<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val %d"/></a:avLst></a:prstGeom>`, adj)
	}
	return `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`
}

func (s *slide) box(x, y, w, h float64, o box) {
	id := s.next()
	fmt.Fprintf(&s.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>%s%s%s%s</p:spPr></p:sp>`,
		id, id, xfrm(x, y, w, h, ""), geom(o, w, h), o.fillXML(), lineXML(o.line, o.lw, ""))
}

// para is one paragraph; run fields override the paragraph's defaults.
type para struct {
	runs    []run
	size    float64
	color   string
	bold    bool
	align   string  // l | ctr | r
	after   float64 // space after, pt
	leading int     // line spacing, percent
	bullet  string  // bullet colour; "" for none
	track   int     // letter spacing, 1/100 pt
}

type run struct {
	t     string
	bold  bool
	ital  bool
	sup   bool
	color string
}

type textOpt struct {
	anchor string // t | ctr | b
	nowrap bool
	box    *box // shape behind the text
	inset  [4]float64
}

func (s *slide) text(x, y, w, h float64, ps []para, o textOpt) {
	id := s.next()
	shape := box{}
	if o.box != nil {
		shape = *o.box
	}
	anchor := o.anchor
	if anchor == "" {
		anchor = "t"
	}
	wrap := "square"
	if o.nowrap {
		wrap = "none"
	}
	fmt.Fprintf(&s.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr>%s%s%s%s</p:spPr>`,
		id, id, xfrm(x, y, w, h, ""), geom(shape, w, h), shape.fillXML(), lineXML(shape.line, shape.lw, ""))
	fmt.Fprintf(&s.b, `<p:txBody><a:bodyPr wrap="%s" lIns="%d" tIns="%d" rIns="%d" bIns="%d" anchor="%s"><a:noAutofit/></a:bodyPr><a:lstStyle/>`,
		wrap, emu(o.inset[0]), emu(o.inset[1]), emu(o.inset[2]), emu(o.inset[3]), anchor)
	for _, p := range ps {
		s.b.WriteString(paraXML(p))
	}
	if len(ps) == 0 {
		s.b.WriteString(`<a:p><a:endParaRPr lang="en-US"/></a:p>`)
	}
	s.b.WriteString(`</p:txBody></p:sp>`)
}

func paraXML(p para) string {
	var b strings.Builder
	align := p.align
	if align == "" {
		align = "l"
	}
	leading := p.leading
	if leading == 0 {
		leading = 108
	}
	indent := ""
	if p.bullet != "" {
		m := emu(p.size * 1.15)
		indent = fmt.Sprintf(` marL="%d" indent="-%d"`, m, m)
	}
	fmt.Fprintf(&b, `<a:p><a:pPr%s algn="%s"><a:lnSpc><a:spcPct val="%d"/></a:lnSpc><a:spcBef><a:spcPts val="0"/></a:spcBef><a:spcAft><a:spcPts val="%d"/></a:spcAft>`,
		indent, align, leading*1000, int(p.after*100))
	if p.bullet != "" {
		fmt.Fprintf(&b, `<a:buClr><a:srgbClr val="%s"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="•"/>`, hex(p.bullet))
	} else {
		b.WriteString(`<a:buNone/>`)
	}
	b.WriteString(`</a:pPr>`)
	for _, r := range p.runs {
		if r.t == "" {
			continue
		}
		attrs := fmt.Sprintf(` lang="en-US" sz="%d"`, int(math.Round(p.size*100)))
		if p.bold || r.bold {
			attrs += ` b="1"`
		}
		if r.ital {
			attrs += ` i="1"`
		}
		if p.track != 0 {
			attrs += fmt.Sprintf(` spc="%d"`, p.track)
		}
		if r.sup {
			attrs += ` baseline="30000"`
		}
		color := r.color
		if color == "" {
			color = p.color
		}
		fmt.Fprintf(&b, `<a:r><a:rPr%s dirty="0">%s<a:latin typeface="%s"/><a:cs typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`,
			attrs, solid(color, 0), deckFont, deckFont, xesc(r.t))
	}
	fmt.Fprintf(&b, `<a:endParaRPr lang="en-US" sz="%d" dirty="0"/></a:p>`, int(math.Round(p.size*100)))
	return b.String()
}

// xesc escapes text for XML and drops characters XML 1.0 cannot carry.
func xesc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n':
			b.WriteByte(' ')
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xE000 && r <= 0xE001):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// label is a one-run paragraph.
func label(t string, size float64, color string, bold bool) para {
	return para{runs: []run{{t: t}}, size: size, color: color, bold: bold}
}

// rich turns built report text into runs: emphasis is kept and citation
// numbers become small superscripts.
func (d *deck) rich(s string) []run {
	var out []run
	for _, seg := range report.Inline(s) {
		if len(seg.Cites) > 0 {
			out = append(out, run{t: " " + citeList(seg.Cites), sup: true, color: d.th.Muted})
			continue
		}
		out = append(out, run{t: seg.Text, bold: seg.Bold, ital: seg.Italic})
	}
	return out
}

func citeList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ",")
}

// noteText is built text as plain text with [n] citations.
func noteText(s string) string {
	var b strings.Builder
	for _, seg := range report.Inline(s) {
		if len(seg.Cites) > 0 {
			b.WriteString(" [" + citeList(seg.Cites) + "]")
			continue
		}
		b.WriteString(seg.Text)
	}
	return b.String()
}

var sentenceEnd = regexp.MustCompile(`([.!?]["”’)]?(?:\x{E000}[\d,]+\x{E001})?)\s+[\p{Lu}\d“"(]`)

var abbreviations = regexp.MustCompile(`(?i)\b(?:e\.g|i\.e|vs|etc|approx|est|inc|ltd|co|corp|no|fig|mr|ms|dr|st|u\.s|u\.k)\.$`)

// lede returns the opening of built text — its first sentence, or first two
// when the first is very short — cut to about max characters. Citation markers inside it are kept.
func lede(s string, max int) string {
	s = strings.TrimSpace(s)
	out := s
	for from := 0; from < len(s); {
		loc := sentenceEnd.FindStringSubmatchIndex(s[from:])
		if loc == nil {
			break
		}
		punct := from + loc[2] + 1
		if abbreviations.MatchString(s[:punct]) {
			from = punct
			continue
		}
		// Keep a closing quote and the citation that follows the full stop.
		out = s[:from+loc[3]]
		// A very short opener ("X is not enough.") says little alone: take
		// the next sentence with it.
		if len([]rune(report.PlainText(out))) >= 70 {
			break
		}
		from += loc[3]
		out = s
	}
	plain := []rune(report.PlainText(out))
	if len(plain) <= max {
		return out
	}
	cut := string(plain[:max])
	if i := strings.LastIndexByte(cut, ' '); i > max*2/3 {
		cut = cut[:i]
	}
	cut = strings.TrimRight(cut, " ,;:—-") + "…"
	// Re-attach the sentence's citations so the cut text stays sourced.
	var cites []int
	for _, seg := range report.Inline(out) {
		for _, n := range seg.Cites {
			if !containsNum(cites, n) {
				cites = append(cites, n)
			}
		}
	}
	if len(cites) > 0 {
		cut += "" + citeList(cites) + ""
	}
	return cut
}

func containsNum(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// textHeight estimates the height of wrapped text.
func textHeight(s string, w, size float64, leading float64) float64 {
	return float64(len(wrap(report.PlainText(s), w, size, 99))) * size * leading
}

// ---------- canvas → DrawingML ----------

// draw places a chart or diagram canvas inside the given box, keeping its
// aspect ratio, as a group of native shapes.
func (s *slide) draw(c *canvas, x, y, w, h float64) {
	if c == nil || c.W <= 0 || c.H <= 0 {
		return
	}
	k := math.Min(w/c.W, h/c.H)
	ox, oy := x+(w-c.W*k)/2, y+(h-c.H*k)/2
	gid := s.next()
	fmt.Fprintf(&s.b, `<p:grpSp><p:nvGrpSpPr><p:cNvPr id="%d" name="Figure %d"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="%[3]d" y="%[4]d"/><a:ext cx="%[5]d" cy="%[6]d"/><a:chOff x="%[3]d" y="%[4]d"/><a:chExt cx="%[5]d" cy="%[6]d"/></a:xfrm></p:grpSpPr>`,
		gid, gid, emu(ox), emu(oy), emu(c.W*k), emu(c.H*k))
	X := func(v float64) float64 { return ox + v*k }
	Y := func(v float64) float64 { return oy + v*k }
	for _, op := range c.ops {
		switch op.kind {
		case opRect:
			id := s.next()
			o := box{fill: op.fill, alpha: op.opacity, radius: op.r * k, line: op.stroke, lw: op.sw * k}
			fmt.Fprintf(&s.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Rect %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>%s%s%s%s</p:spPr></p:sp>`,
				id, id, xfrm(X(op.x), Y(op.y), op.w*k, op.h*k, ""), geom(o, op.w*k, op.h*k), o.fillXML(), lineXML(o.line, o.lw, ""))
		case opCircle:
			id := s.next()
			o := box{fill: op.fill, ellipse: true, line: op.stroke, lw: op.sw * k}
			fmt.Fprintf(&s.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Dot %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>%s%s%s%s</p:spPr></p:sp>`,
				id, id, xfrm(X(op.x-op.r), Y(op.y-op.r), 2*op.r*k, 2*op.r*k, ""), geom(o, 1, 1), o.fillXML(), lineXML(o.line, o.lw, ""))
		case opLine, opPolyline, opCurve:
			// Straight segments only: preset lines draw the same in every
			// viewer, which freeform paths do not.
			pts := op.pts
			if op.kind == opCurve {
				pts = flatten(op.pts, 10)
			}
			for i := 1; i < len(pts); i++ {
				a, b := pts[i-1], pts[i]
				flip, tail := "", ""
				if (b.x-a.x)*(b.y-a.y) < 0 {
					flip = ` flipV="1"`
				}
				if op.arrow && i == len(pts)-1 {
					// The arrowhead sits at the segment's end point, which a
					// flipped or right-to-left line has at its other end.
					tail = `<a:tailEnd type="triangle" w="med" len="med"/>`
					if b.x < a.x || (b.x == a.x && b.y < a.y) {
						tail = `<a:headEnd type="triangle" w="med" len="med"/>`
					}
				}
				id := s.next()
				fmt.Fprintf(&s.b, `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="%d" name="Line %d"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr>%s<a:prstGeom prst="line"><a:avLst/></a:prstGeom>%s</p:spPr></p:cxnSp>`,
					id, id, xfrm(X(math.Min(a.x, b.x)), Y(math.Min(a.y, b.y)), math.Abs(b.x-a.x)*k, math.Abs(b.y-a.y)*k, flip), lineXML(op.stroke, op.sw*k, tail))
			}
		case opPolygon:
			s.path(op, X, Y, k)
		case opWedge:
			s.wedge(op, X, Y, k)
		case opText:
			size := op.size * k
			w := textWidth(op.text, size)*1.3 + 6
			tx, align := X(op.x), "l"
			switch op.anchor {
			case "middle":
				tx, align = tx-w/2, "ctr"
			case "end":
				tx, align = tx-w, "r"
			}
			s.text(tx, Y(op.y)-size*0.96, w, size*1.25, []para{{runs: []run{{t: op.text}}, size: size, color: op.fill, bold: op.weight >= 600, align: align, leading: 100}}, textOpt{nowrap: true})
		}
	}
	s.b.WriteString(`</p:grpSp>`)
}

// flatten approximates a cubic Bézier with n straight segments.
func flatten(p []pt, n int) []pt {
	out := make([]pt, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		out = append(out, pt{
			u*u*u*p[0].x + 3*u*u*t*p[1].x + 3*u*t*t*p[2].x + t*t*t*p[3].x,
			u*u*u*p[0].y + 3*u*u*t*p[1].y + 3*u*t*t*p[2].y + t*t*t*p[3].y,
		})
	}
	return out
}

// path writes a filled polygon as a custom geometry.
func (s *slide) path(op *shape, X, Y func(float64) float64, k float64) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range op.pts {
		minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
		minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	w, h := math.Max((maxX-minX)*k, 0.1), math.Max((maxY-minY)*k, 0.1)
	px := func(p pt) string {
		return fmt.Sprintf(`<a:pt x="%d" y="%d"/>`, emu((p.x-minX)*k), emu((p.y-minY)*k))
	}
	var d strings.Builder
	d.WriteString(`<a:moveTo>` + px(op.pts[0]) + `</a:moveTo>`)
	for _, p := range op.pts[1:] {
		d.WriteString(`<a:lnTo>` + px(p) + `</a:lnTo>`)
	}
	d.WriteString(`<a:close/>`)
	id := s.next()
	fmt.Fprintf(&s.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Area %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>%s<a:custGeom><a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="0" t="0" r="r" b="b"/><a:pathLst><a:path w="%d" h="%d">%s</a:path></a:pathLst></a:custGeom>%s<a:ln><a:noFill/></a:ln></p:spPr></p:sp>`,
		id, id, xfrm(X(minX), Y(minY), w, h, ""), emu(w), emu(h), d.String(), solid(op.fill, op.opacity))
}

// wedge writes a ring segment as short, thick, flat-capped strokes along its
// centre line. Arc presets and freeform paths are drawn differently (or not
// at all) by some viewers; straight lines are not.
func (s *slide) wedge(op *shape, X, Y func(float64) float64, k float64) {
	mid, thick := (op.r+op.r0)/2, op.r-op.r0
	steps := int(math.Ceil((op.a1 - op.a0) / 3))
	if steps < 1 {
		steps = 1
	}
	step := (op.a1 - op.a0) / float64(steps)
	// Each stroke is lengthened a little so neighbours overlap at the rim.
	over := 0.0
	if steps > 1 {
		over = 2 * math.Atan(thick/2*math.Tan(step*math.Pi/360)/mid) * 180 / math.Pi
	}
	for i := 0; i < steps; i++ {
		from, to := op.a0+float64(i)*step, op.a0+float64(i+1)*step
		if i > 0 {
			from -= over
		}
		if i < steps-1 {
			to += over
		}
		a, b := polar(op.x, op.y, mid, from), polar(op.x, op.y, mid, to)
		flip := ""
		if (b.x-a.x)*(b.y-a.y) < 0 {
			flip = ` flipV="1"`
		}
		id := s.next()
		fmt.Fprintf(&s.b, `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="%d" name="Slice %d"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr>%s<a:prstGeom prst="line"><a:avLst/></a:prstGeom><a:ln w="%d" cap="flat">%s</a:ln></p:spPr></p:cxnSp>`,
			id, id, xfrm(X(math.Min(a.x, b.x)), Y(math.Min(a.y, b.y)), math.Abs(b.x-a.x)*k, math.Abs(b.y-a.y)*k, flip), emu(thick*k), solid(op.fill, 0))
	}
}

// ---------- tables ----------

// table writes rows as a native table and returns nothing; the caller
// paginates. widths are in points and sum to the table width.
func (s *slide) table(x, y float64, widths []float64, cols []string, rows [][]string, size float64, d *deck) {
	total := 0.0
	for _, w := range widths {
		total += w
	}
	id := s.next()
	fmt.Fprintf(&s.b, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table %d"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr firstRow="1"/><a:tblGrid>`,
		id, id, emu(x), emu(y), emu(total), emu(float64(len(rows)+1)*size*2.4))
	for _, w := range widths {
		fmt.Fprintf(&s.b, `<a:gridCol w="%d"/>`, emu(w))
	}
	s.b.WriteString(`</a:tblGrid>`)
	th := d.th
	cell := func(p para, fill, rule string) {
		none := `<a:noFill/>`
		fmt.Fprintf(&s.b, `<a:tc><a:txBody><a:bodyPr/><a:lstStyle/>%s</a:txBody><a:tcPr marL="%d" marR="%d" marT="%d" marB="%d" anchor="ctr"><a:lnL w="0">%s</a:lnL><a:lnR w="0">%s</a:lnR><a:lnT w="0">%s</a:lnT><a:lnB w="9525">%s</a:lnB>%s</a:tcPr></a:tc>`,
			paraXML(p), emu(8), emu(8), emu(5.5), emu(5.5), none, none, none, solid(rule, 0), solid(fill, 0))
	}
	fmt.Fprintf(&s.b, `<a:tr h="%d">`, emu(size*2.2))
	for _, c := range cols {
		cell(para{runs: []run{{t: strings.ToUpper(c)}}, size: size * 0.78, color: th.Muted, bold: true, track: 60}, th.Tint, th.Rule)
	}
	s.b.WriteString(`</a:tr>`)
	for _, row := range rows {
		fmt.Fprintf(&s.b, `<a:tr h="%d">`, emu(size*2.2))
		for i, c := range row {
			// Cells carry no citation numbers: the slide's source line does.
			p := para{runs: []run{{t: report.PlainText(c)}}, size: size, color: th.Body}
			if i == 0 {
				p.color, p.bold = th.Ink, true
			}
			cell(p, "#ffffff", th.Rule)
		}
		s.b.WriteString(`</a:tr>`)
	}
	s.b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
}

// tableWidths shares the width between columns by how much text they hold.
func tableWidths(cols []string, rows [][]string, total float64) []float64 {
	weights := make([]float64, len(cols))
	sum := 0.0
	for i, c := range cols {
		longest := float64(len([]rune(c)))
		for _, r := range rows {
			if i < len(r) {
				longest = math.Max(longest, float64(len([]rune(report.PlainText(r[i])))))
			}
		}
		weights[i] = math.Min(math.Max(longest, 8), 46)
		sum += weights[i]
	}
	for i := range weights {
		weights[i] = total * weights[i] / sum
	}
	return weights
}

// ---------- package ----------

func (d *deck) pack() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	modified := d.r.Metadata.Date.UTC()
	write := func(name, content string) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(content))
		return err
	}
	const head = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	const ns = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	const relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	const emptyTree = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
	rels := func(items ...[2]string) string {
		var b strings.Builder
		b.WriteString(head + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
		for i, it := range items {
			fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s" Target="%s"/>`, i+1, it[0], it[1])
		}
		b.WriteString(`</Relationships>`)
		return b.String()
	}
	n := len(d.slides)

	var types strings.Builder
	types.WriteString(head + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	override := func(part, ct string) {
		fmt.Fprintf(&types, `<Override PartName="%s" ContentType="application/vnd.openxmlformats-%s+xml"/>`, part, ct)
	}
	override("/ppt/presentation.xml", "officedocument.presentationml.presentation.main")
	override("/ppt/slideMasters/slideMaster1.xml", "officedocument.presentationml.slideMaster")
	override("/ppt/slideLayouts/slideLayout1.xml", "officedocument.presentationml.slideLayout")
	override("/ppt/notesMasters/notesMaster1.xml", "officedocument.presentationml.notesMaster")
	override("/ppt/theme/theme1.xml", "officedocument.theme")
	override("/ppt/theme/theme2.xml", "officedocument.theme")
	override("/ppt/presProps.xml", "officedocument.presentationml.presProps")
	override("/ppt/viewProps.xml", "officedocument.presentationml.viewProps")
	override("/ppt/tableStyles.xml", "officedocument.presentationml.tableStyles")
	for i := 1; i <= n; i++ {
		override(fmt.Sprintf("/ppt/slides/slide%d.xml", i), "officedocument.presentationml.slide")
		override(fmt.Sprintf("/ppt/notesSlides/notesSlide%d.xml", i), "officedocument.presentationml.notesSlide")
	}
	override("/docProps/core.xml", "package.core-properties")
	override("/docProps/app.xml", "officedocument.extended-properties")
	types.WriteString(`</Types>`)

	m := d.r.Metadata
	stamp := modified.Format("2006-01-02T15:04:05Z")
	files := [][2]string{
		{"[Content_Types].xml", types.String()},
		{"_rels/.rels", rels(
			[2]string{relNS + "/officeDocument", "ppt/presentation.xml"},
			[2]string{"http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", "docProps/core.xml"},
			[2]string{relNS + "/extended-properties", "docProps/app.xml"})},
		{"docProps/core.xml", head + fmt.Sprintf(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>%s</dc:title><dc:subject>%s</dc:subject><dc:creator>%s</dc:creator><cp:lastModifiedBy>%s</cp:lastModifiedBy><dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified></cp:coreProperties>`,
			xesc(m.Title), xesc(m.Topic), xesc(nonEmptyStr(m.Author, "Boku research agents")), xesc(m.Generator), stamp, stamp)},
		{"docProps/app.xml", head + fmt.Sprintf(`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><TotalTime>0</TotalTime><Application>%s</Application><PresentationFormat>Widescreen</PresentationFormat><Slides>%d</Slides><Notes>%d</Notes></Properties>`, xesc(nonEmptyStr(m.Generator, "Boku")), n, n)},
		{"ppt/presProps.xml", head + `<p:presentationPr ` + ns + `/>`},
		{"ppt/viewProps.xml", head + `<p:viewPr ` + ns + `><p:normalViewPr><p:restoredLeft sz="15620"/><p:restoredTop sz="80000"/></p:normalViewPr><p:slideViewPr><p:cSldViewPr><p:cViewPr varScale="1"><p:scale><a:sx n="100" d="100"/><a:sy n="100" d="100"/></p:scale><p:origin x="0" y="0"/></p:cViewPr></p:cSldViewPr></p:slideViewPr></p:viewPr>`},
		{"ppt/tableStyles.xml", head + `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BB5C5B4A11}"/>`},
		{"ppt/theme/theme1.xml", head + themeXML(d.th, "Boku "+d.th.Name)},
		{"ppt/theme/theme2.xml", head + themeXML(d.th, "Boku notes")},
		{"ppt/slideMasters/slideMaster1.xml", head + `<p:sldMaster ` + ns + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>` + emptyTree + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="2800" b="1"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="1600"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/></a:defRPr></a:lvl1pPr></p:bodyStyle><p:otherStyle><a:lvl1pPr><a:defRPr sz="1600"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/></a:defRPr></a:lvl1pPr></p:otherStyle></p:txStyles></p:sldMaster>`},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", rels(
			[2]string{relNS + "/slideLayout", "../slideLayouts/slideLayout1.xml"},
			[2]string{relNS + "/theme", "../theme/theme1.xml"})},
		{"ppt/slideLayouts/slideLayout1.xml", head + `<p:sldLayout ` + ns + ` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>` + emptyTree + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", rels([2]string{relNS + "/slideMaster", "../slideMasters/slideMaster1.xml"})},
		{"ppt/notesMasters/notesMaster1.xml", head + `<p:notesMaster ` + ns + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>` + emptyTree +
			`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldImg" idx="2"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="685800" y="685800"/><a:ext cx="5486400" cy="3086100"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln w="12700"><a:solidFill><a:prstClr val="black"/></a:solidFill></a:ln></p:spPr></p:sp>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" sz="quarter" idx="3"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="685800" y="4400550"/><a:ext cx="5486400" cy="3600450"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr vert="horz" lIns="91440" tIns="45720" rIns="91440" bIns="45720" rtlCol="0"/><a:lstStyle/><a:p><a:pPr lvl="0"/><a:r><a:rPr lang="en-US"/><a:t>Notes</a:t></a:r></a:p></p:txBody></p:sp>` +
			`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:notesStyle><a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1"><a:defRPr sz="1200" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/><a:cs typeface="+mn-cs"/></a:defRPr></a:lvl1pPr></p:notesStyle></p:notesMaster>`},
		{"ppt/notesMasters/_rels/notesMaster1.xml.rels", rels([2]string{relNS + "/theme", "../theme/theme2.xml"})},
	}

	var pres, presRels strings.Builder
	pres.WriteString(head + `<p:presentation ` + ns + ` saveSubsetFonts="1"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="rId2"/></p:notesMasterIdLst><p:sldIdLst>`)
	relItems := [][2]string{
		{relNS + "/slideMaster", "slideMasters/slideMaster1.xml"},
		{relNS + "/notesMaster", "notesMasters/notesMaster1.xml"},
		{relNS + "/presProps", "presProps.xml"},
		{relNS + "/viewProps", "viewProps.xml"},
		{relNS + "/theme", "theme/theme1.xml"},
		{relNS + "/tableStyles", "tableStyles.xml"},
	}
	for i, s := range d.slides {
		fmt.Fprintf(&pres, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, len(relItems)+1)
		relItems = append(relItems, [2]string{relNS + "/slide", fmt.Sprintf("slides/slide%d.xml", i+1)})
		files = append(files,
			[2]string{fmt.Sprintf("ppt/slides/slide%d.xml", i+1), head + `<p:sld ` + ns + `><p:cSld><p:spTree>` + emptyTree + s.b.String() + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`},
			[2]string{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), rels(
				[2]string{relNS + "/slideLayout", "../slideLayouts/slideLayout1.xml"},
				[2]string{relNS + "/notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", i+1)})},
			[2]string{fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", i+1), head + notesXML(ns, emptyTree, s.notes)},
			[2]string{fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", i+1), rels(
				[2]string{relNS + "/notesMaster", "../notesMasters/notesMaster1.xml"},
				[2]string{relNS + "/slide", fmt.Sprintf("../slides/slide%d.xml", i+1)})})
	}
	fmt.Fprintf(&pres, `</p:sldIdLst><p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/><p:defaultTextStyle><a:defPPr><a:defRPr lang="en-US"/></a:defPPr><a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangingPunct="1"><a:defRPr sz="1800" kern="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/><a:cs typeface="+mn-cs"/></a:defRPr></a:lvl1pPr></p:defaultTextStyle></p:presentation>`, emu(slideW), emu(slideH))
	presRels.WriteString(rels(relItems...))
	files = append(files, [2]string{"ppt/presentation.xml", pres.String()}, [2]string{"ppt/_rels/presentation.xml.rels", presRels.String()})

	for _, f := range files {
		if err := write(f[0], f[1]); err != nil {
			return nil, fmt.Errorf("render pptx: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("render pptx: %w", err)
	}
	return buf.Bytes(), nil
}

func notesXML(ns, emptyTree string, notes []string) string {
	var b strings.Builder
	b.WriteString(`<p:notes ` + ns + `><p:cSld><p:spTree>` + emptyTree +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/>`)
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			b.WriteString(`<a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>` + xesc(n) + `</a:t></a:r></a:p>`)
		}
	}
	if len(notes) == 0 {
		b.WriteString(`<a:p><a:endParaRPr lang="en-US"/></a:p>`)
	}
	b.WriteString(`</p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`)
	return b.String()
}

// themeXML is the deck's Office theme: the report theme's colours and font,
// so shapes a reader adds in PowerPoint match the generated ones.
func themeXML(t Theme, name string) string {
	c := func(tag, col string) string {
		return fmt.Sprintf(`<a:%s><a:srgbClr val="%s"/></a:%s>`, tag, hex(col), tag)
	}
	fonts := func(tag string) string {
		return fmt.Sprintf(`<a:%s><a:latin typeface="%s"/><a:ea typeface=""/><a:cs typeface=""/></a:%s>`, tag, deckFont, tag)
	}
	fill := `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	ln := `<a:ln w="9525" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>`
	effect := `<a:effectStyle><a:effectLst/></a:effectStyle>`
	return `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="` + xesc(name) + `"><a:themeElements><a:clrScheme name="` + xesc(name) + `">` +
		c("dk1", t.Ink) + c("lt1", "#ffffff") + c("dk2", t.Body) + c("lt2", t.Tint) +
		c("accent1", t.Palette[0]) + c("accent2", t.Palette[1]) + c("accent3", t.Palette[2]) + c("accent4", t.Palette[3]) + c("accent5", t.Palette[4]) + c("accent6", t.Palette[5]) +
		c("hlink", t.Accent) + c("folHlink", t.Muted) +
		`</a:clrScheme><a:fontScheme name="Boku">` + fonts("majorFont") + fonts("minorFont") + `</a:fontScheme><a:fmtScheme name="Boku"><a:fillStyleLst>` + fill + fill + fill +
		`</a:fillStyleLst><a:lnStyleLst>` + ln + ln + ln + `</a:lnStyleLst><a:effectStyleLst>` + effect + effect + effect +
		`</a:effectStyleLst><a:bgFillStyleLst>` + fill + fill + fill + `</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`
}
