package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// build lays the report out as slides: cover, agenda, summary, findings, one
// or more slides per section (text, then each exhibit on its own slide),
// conclusion, sources and a closing note on method.
func (d *deck) build() {
	r := d.r
	d.cover()
	if len(r.Sections) >= 3 {
		d.agenda()
	}
	d.summary()
	if len(r.Unverified) > 0 {
		d.unverified()
	}
	for at := 0; at < len(r.KeyFindings); at += 6 {
		d.findings(r.KeyFindings[at:min(at+6, len(r.KeyFindings))], at)
	}
	for _, sec := range r.Sections {
		d.section(sec)
	}
	if len(r.Conclusion) > 0 {
		var items []string
		var notes []string
		for _, p := range r.Conclusion {
			items = append(items, lede(p.Text, 230))
			notes = append(notes, noteText(p.Text))
		}
		s := d.add()
		y := d.chrome(s, r.Labels.Conclusion, r.Labels.Conclusion)
		d.bullets(s, padX, y, bodyW*0.82, bodyBottom-y, items[:min(len(items), 5)])
		s.notes = notes
	}
	if r.IncludeReferences {
		for at := 0; at < len(r.Sources); at += 12 {
			d.sources(r.Sources[at:min(at+12, len(r.Sources))], at > 0)
		}
	}
	d.closing()
}

// chrome draws what every content slide shares — accent bar, kicker, title
// and footer — and returns the y where the body starts.
func (d *deck) chrome(s *slide, kicker, title string) float64 {
	th := d.th
	s.box(padX, 40, 44, 5, box{grad: []string{th.Accent, th.Accent2, th.Accent3}, radius: 2.5})
	s.text(padX, 54, bodyW, 16, []para{{runs: []run{{t: strings.ToUpper(kicker)}}, size: 9.5, color: th.Accent, bold: true, track: 140}}, textOpt{})
	size := 28.0
	plain := report.PlainText(title)
	for _, sz := range []float64{28, 24, 21, 18} {
		size = sz
		if len(wrap(plain, bodyW-40, sz, 9)) <= 2 {
			break
		}
	}
	lines := len(wrap(plain, bodyW-40, size, 3))
	h := float64(lines) * size * 1.14
	s.text(padX, 76, bodyW-20, h+6, []para{{runs: []run{{t: plain}}, size: size, color: th.Ink, bold: true, leading: 96, track: -30}}, textOpt{})

	n := len(d.slides)
	s.box(padX, 508, bodyW, 0.75, box{fill: th.Rule})
	s.text(padX, 514, bodyW-80, 14, []para{label(truncateRunes(d.r.Metadata.Title, 90), 8, th.Faint, false)}, textOpt{})
	s.text(slideW-padX-60, 514, 60, 14, []para{{runs: []run{{t: fmt.Sprint(n)}}, size: 8.5, color: th.Ink, bold: true, align: "r"}}, textOpt{})
	return 76 + h + 26
}

// bullets writes items as a bulleted list, at the largest size that fits.
func (d *deck) bullets(s *slide, x, y, w, h float64, items []string) {
	size := 12.0
	for _, sz := range []float64{17, 16, 15, 14, 13, 12} {
		total := 0.0
		for _, it := range items {
			total += textHeight(it, w-sz*1.4, sz, 1.32) + sz*0.75
		}
		size = sz
		if total <= h {
			break
		}
	}
	var ps []para
	for _, it := range items {
		// A leading "## " marks a group label (a subheading in the report).
		if rest, ok := strings.CutPrefix(it, "## "); ok {
			ps = append(ps, para{runs: []run{{t: rest}}, size: size * 0.92, color: d.th.Accent, bold: true, after: size * 0.35, leading: 112})
			continue
		}
		ps = append(ps, para{runs: d.rich(it), size: size, color: d.th.Body, after: size * 0.75, leading: 118, bullet: d.th.Accent})
	}
	s.text(x, y, w, h, ps, textOpt{})
}

func (d *deck) cover() {
	r, th := d.r, d.th
	s := d.add()
	ink, soft := th.Ink, th.Muted
	kicker := box{fill: "#ffffff", line: mix(th.Accent, "#ffffff", 0.6), lw: 0.75, radius: 13}
	kickerText := th.Accent
	switch th.Name {
	case "meridian":
		s.box(0, 0, slideW, slideH, box{fill: th.Accent})
		s.box(520, -260, 760, 640, box{fill: th.Accent2, alpha: 0.6, glow: true, ellipse: true})
		s.box(-260, 280, 620, 560, box{fill: th.Accent2, alpha: 0.3, glow: true, ellipse: true})
		ink, soft = "#ffffff", mix(th.Accent, "#ffffff", 0.72)
		kicker, kickerText = box{fill: mix(th.Accent, "#ffffff", 0.12), line: mix(th.Accent, "#ffffff", 0.4), lw: 0.75, radius: 13}, "#ffffff"
	case "ember":
		s.box(0, 0, slideW, 128, box{grad: []string{th.Accent, th.Accent2, th.Accent3}})
		kicker, kickerText = box{fill: mix(th.Accent, "#ffffff", 0.18), line: mix(th.Accent, "#ffffff", 0.6), lw: 0.75, radius: 13}, "#ffffff"
	case "slate":
		s.box(0, 0, slideW, 8, box{fill: th.Accent})
	default:
		s.box(480, -300, 800, 680, box{fill: th.Accent2, alpha: 0.42, glow: true, ellipse: true})
		s.box(-320, -80, 760, 700, box{fill: th.Accent, alpha: 0.3, glow: true, ellipse: true})
		s.box(380, 220, 820, 620, box{fill: th.Accent3, alpha: 0.26, glow: true, ellipse: true})
	}
	kw := textWidth(strings.ToUpper(r.Metadata.ReportType), 9.5)*1.25 + 34
	s.text(padX, 62, kw, 26, []para{{runs: []run{{t: strings.ToUpper(r.Metadata.ReportType)}}, size: 9.5, color: kickerText, bold: true, align: "ctr", track: 140}}, textOpt{anchor: "ctr", box: &kicker, nowrap: true})

	size := 46.0
	for _, sz := range []float64{46, 40, 34, 30} {
		size = sz
		if len(wrap(r.Metadata.Title, 760, sz, 9)) <= 3 {
			break
		}
	}
	lines := len(wrap(r.Metadata.Title, 760, size, 4))
	y := 168.0
	s.box(padX, y-28, 52, 6, box{grad: []string{th.Accent2, th.Accent3}, radius: 3})
	if th.Name != "meridian" {
		s.box(padX, y-28, 52, 6, box{grad: []string{th.Accent, th.Accent2, th.Accent3}, radius: 3})
	}
	h := float64(lines) * size * 1.1
	s.text(padX, y, 800, h+8, []para{{runs: []run{{t: r.Metadata.Title}}, size: size, color: ink, bold: true, leading: 94, track: -60}}, textOpt{})
	if r.Metadata.Subtitle != "" {
		s.text(padX, y+h+18, 680, 70, []para{{runs: []run{{t: r.Metadata.Subtitle}}, size: 17, color: mix(ink, soft, 0.45), leading: 120}}, textOpt{})
	}
	meta := []string{r.Metadata.Date.Format("January 2006"), fmt.Sprintf("%d cited sources", len(r.Sources)), nonEmptyStr(r.Metadata.Author, "Boku research agents")}
	s.box(padX, 452, bodyW, 0.75, box{fill: mix(ink, soft, 0.6), alpha: 0.5})
	s.text(padX, 464, bodyW, 18, []para{label(strings.Join(meta, "   ·   "), 11, soft, true)}, textOpt{})
	s.notes = []string{"Research question: " + r.Metadata.Topic, "Compiled from public sources by an automated multi-agent research pipeline. Verify material facts before relying on them."}
}

func (d *deck) agenda() {
	r, th := d.r, d.th
	s := d.add()
	y := d.chrome(s, "Agenda", "In this deck")
	secs := r.Sections
	cols := 1
	if len(secs) > 5 {
		cols = 2
	}
	per := (len(secs) + cols - 1) / cols
	colW := (bodyW - 40*float64(cols-1)) / float64(cols)
	rowH := math.Min(58, (bodyBottom-y)/float64(per))
	for i, sec := range secs {
		x := padX + float64(i/per)*(colW+40)
		yy := y + float64(i%per)*rowH
		s.text(x, yy, 44, rowH-10, []para{label(fmt.Sprintf("%02d", sec.Number), 15, th.Accent, true)}, textOpt{anchor: "ctr"})
		ps := []para{label(truncateRunes(sec.Title, 70), 15, th.Ink, true)}
		if sec.Takeaway != "" && rowH >= 50 {
			ps = append(ps, label(truncateRunes(report.PlainText(sec.Takeaway), 105), 10.5, th.Muted, false))
		}
		s.text(x+48, yy, colW-48, rowH-10, ps, textOpt{anchor: "ctr"})
		s.box(x, yy+rowH-6, colW, 0.75, box{fill: th.Rule})
	}
}

func (d *deck) summary() {
	r := d.r
	s := d.add()
	y := d.chrome(s, r.Labels.Summary, r.Metadata.Title)
	var items []string
	for _, p := range r.ExecutiveSummary {
		items = append(items, lede(p.Text, 240))
		s.notes = append(s.notes, noteText(p.Text))
	}
	items = items[:min(len(items), 4)]
	w := bodyW * 0.82
	if n := len(r.Metrics); n > 0 {
		w = bodyW - 330
		d.tiles(s, r.Metrics, slideW-padX-300, y, 300, bodyBottom-y, 2)
	}
	d.bullets(s, padX, y, w, bodyBottom-y, items)
}

// tiles draws metric tiles in a grid of the given column count.
func (d *deck) tiles(s *slide, ms []report.Metric, x, y, w, h float64, cols int) {
	th := d.th
	cols = min(cols, len(ms))
	rows := (len(ms) + cols - 1) / cols
	gap := 14.0
	tw := (w - gap*float64(cols-1)) / float64(cols)
	tileH := math.Min((h-gap*float64(rows-1))/float64(rows), 150)
	for i, m := range ms {
		tx, ty := x+float64(i%cols)*(tw+gap), y+float64(i/cols)*(tileH+gap)
		s.box(tx, ty, tw, tileH, box{fill: mix(th.Accent, "#ffffff", 0.95), line: th.Rule, lw: 0.75, radius: 12})
		s.box(tx+16, ty+16, 28, 4, box{grad: []string{th.Accent, th.Accent2}, radius: 2})
		value := m.Value
		if mark := trendMark(m.Trend); mark != "" {
			value += " " + mark
		}
		size := 30.0
		for _, sz := range []float64{30, 26, 22, 19, 16} {
			size = sz
			if textWidth(value, sz)*1.08 <= tw-32 {
				break
			}
		}
		ps := []para{
			{runs: []run{{t: value}}, size: size, color: th.Ink, bold: true, after: 4, leading: 100, track: -40},
			{runs: []run{{t: m.Label}}, size: 11, color: th.Ink, bold: true, after: 3, leading: 110},
		}
		if m.Detail != "" && tileH >= 110 {
			ps = append(ps, para{runs: d.rich(lede(m.Detail, 90)), size: 9.5, color: th.Muted, leading: 112})
		}
		s.text(tx+16, ty+28, tw-32, tileH-36, ps, textOpt{})
	}
}

func (d *deck) unverified() {
	const red, soft = "#b42318", "#fef3f2"
	s := d.add()
	y := d.chrome(s, "Not verified by fact-check", "Points the fact checker could not verify")
	s.box(padX, y, bodyW, bodyBottom-y, box{fill: soft, line: mix(red, "#ffffff", 0.6), lw: 0.75, radius: 12})
	ps := []para{{runs: []run{{t: "Treat any statement in this deck that touches on the following as unconfirmed."}}, size: 13, color: red, bold: true, after: 10}}
	items := d.r.Unverified
	size := 13.0
	if len(items) > 6 {
		size = 11
	}
	for i, u := range items {
		if i == 10 {
			ps = append(ps, label(fmt.Sprintf("… and %d more, listed in the report.", len(items)-i), size, d.th.Body, false))
			break
		}
		ps = append(ps, para{runs: []run{{t: truncateRunes(u, 260)}}, size: size, color: d.th.Ink, after: size * 0.5, leading: 114, bullet: red})
	}
	s.text(padX, y, bodyW, bodyBottom-y, ps, textOpt{inset: [4]float64{22, 18, 22, 14}})
	s.notes = append([]string{"The fact checker could not verify:"}, items...)
}

func (d *deck) findings(kf []report.KeyFinding, offset int) {
	th := d.th
	s := d.add()
	title := "What the evidence shows"
	if offset > 0 {
		title += " (continued)"
	}
	y := d.chrome(s, d.r.Labels.KeyFindings, title)
	cols := len(kf)
	if cols > 3 {
		cols = (len(kf) + 1) / 2
	}
	rows := (len(kf) + cols - 1) / cols
	gap := 16.0
	cw := (bodyW - gap*float64(cols-1)) / float64(cols)
	ch := math.Min((bodyBottom-y-gap*float64(rows-1))/float64(rows), 230)
	for i, k := range kf {
		x, yy := padX+float64(i%cols)*(cw+gap), y+float64(i/cols)*(ch+gap)
		s.box(x, yy, cw, ch, box{fill: "#ffffff", line: th.Rule, lw: 0.75, radius: 12})
		badge := box{fill: mix(th.Accent, "#ffffff", 0.9), radius: 7}
		s.text(x+16, yy+16, 30, 24, []para{{runs: []run{{t: fmt.Sprintf("%02d", offset+i+1)}}, size: 10.5, color: th.Accent, bold: true, align: "ctr"}}, textOpt{anchor: "ctr", box: &badge})
		head, detail := 14.0, 10.5
		if rows > 1 || cols > 2 {
			head = 13
		}
		hw := cw - 32
		used := textHeight(k.Headline, hw, head, 1.24)
		ps := []para{{runs: d.rich(k.Headline), size: head, color: th.Ink, bold: true, after: 6, leading: 108}}
		if room := ch - 62 - used; k.Detail != "" && room > detail*2.6 {
			maxChars := int(room / (detail * 1.3) * hw / (detail * 0.52))
			ps = append(ps, para{runs: d.rich(lede(k.Detail, min(maxChars, 260))), size: detail, color: th.Muted, leading: 116})
		}
		s.text(x+16, yy+50, hw, ch-58, ps, textOpt{})
		s.notes = append(s.notes, fmt.Sprintf("%d. %s %s", offset+i+1, noteText(k.Headline), noteText(k.Detail)))
	}
}

// page is the text of one section slide being assembled.
type page struct {
	items   []string
	weight  float64
	callout *report.Block
	notes   []string
}

func (p *page) empty() bool { return len(p.items) == 0 && p.callout == nil }

// section turns a section into slides: its prose becomes bullets (the first
// sentence of each paragraph; the notes keep the full text) and every chart,
// diagram, table and set of metrics gets a slide of its own.
func (d *deck) section(sec report.Section) {
	kicker := fmt.Sprintf("Section %02d · %s", sec.Number, sec.Title)
	headline := sec.Title
	if sec.Takeaway != "" {
		headline = sec.Takeaway
	}
	first := true
	pg := &page{}
	flush := func() {
		if pg.empty() {
			return
		}
		s := d.add()
		title := headline
		if !first {
			title = sec.Title
		}
		first = false
		y := d.chrome(s, kicker, title)
		bottom := bodyBottom
		if c := pg.callout; c != nil {
			text := lede(c.Text, 300)
			h := textHeight(text, bodyW-44, 12.5, 1.3) + 46
			bottom = bodyBottom - h - 14
			col, soft := d.calloutColors(c.Tone)
			s.box(padX, bottom+14, bodyW, h, box{fill: soft, radius: 10})
			s.box(padX, bottom+14, 4, h, box{fill: col, radius: 2})
			s.text(padX+22, bottom+26, bodyW-44, h-20, []para{
				{runs: []run{{t: strings.ToUpper(calloutLabel(*c))}}, size: 8.5, color: col, bold: true, after: 4, track: 130},
				{runs: d.rich(text), size: 12.5, color: d.th.Ink, leading: 116},
			}, textOpt{})
		}
		if len(pg.items) > 0 {
			d.bullets(s, padX, y, bodyW*0.86, bottom-y, pg.items)
		}
		s.notes = pg.notes
		pg = &page{}
	}
	addItem := func(text, note string, weight float64) {
		if pg.weight+weight > 5 {
			flush()
		}
		pg.items, pg.weight = append(pg.items, text), pg.weight+weight
		if note != "" {
			pg.notes = append(pg.notes, note)
		}
	}

	blocks := sec.Blocks
	used := map[int]bool{} // blocks shown as an exhibit's commentary
	for i, bl := range blocks {
		switch bl.Type {
		case report.BlockSubheading:
			if pg.weight > 3.5 {
				flush()
			}
			addItem("## "+bl.Text, "", 0.6)
		case report.BlockParagraph:
			if !used[i] {
				addItem(lede(bl.Text, 210), noteText(bl.Text), 1)
			}
		case report.BlockBullets:
			for _, it := range bl.Items {
				addItem(lede(it, 180), noteText(it), 0.8)
			}
		case report.BlockCallout:
			if used[i] {
				continue
			}
			if pg.callout != nil {
				flush()
			}
			c := bl
			pg.callout = &c
			pg.notes = append(pg.notes, calloutLabel(bl)+": "+noteText(bl.Text))
		case report.BlockChart, report.BlockDiagram, report.BlockTable, report.BlockMetrics:
			// The paragraph next to an exhibit interprets it: show it beside
			// the exhibit instead of as a bullet.
			comment, commentLabel := "", "What it shows"
			n := len(pg.items)
			switch {
			case i > 0 && blocks[i-1].Type == report.BlockParagraph && !used[i-1] && n > 0 && !strings.HasPrefix(pg.items[n-1], "## ") && (!first || n > 1):
				comment = blocks[i-1].Text
				pg.items, pg.weight = pg.items[:n-1], pg.weight-1
				if m := len(pg.notes); m > 0 {
					pg.notes = pg.notes[:m-1]
				}
			case i+1 < len(blocks) && blocks[i+1].Type == report.BlockParagraph:
				comment = blocks[i+1].Text
				used[i+1] = true
			case i+1 < len(blocks) && blocks[i+1].Type == report.BlockCallout:
				comment, commentLabel = blocks[i+1].Text, calloutLabel(blocks[i+1])
				used[i+1] = true
			}
			// A lone subheading left behind belongs with the exhibit.
			if n := len(pg.items); n > 0 && strings.HasPrefix(pg.items[n-1], "## ") && pg.callout == nil && n == 1 {
				pg = &page{}
			}
			flush()
			d.exhibit(sec, bl, comment, commentLabel)
			first = false
		}
	}
	flush()
}

func (d *deck) calloutColors(tone string) (string, string) {
	switch tone {
	case "caution":
		return "#b45309", "#fffbeb"
	case "unverified":
		return "#b42318", "#fef3f2"
	case "estimate", "note":
		return d.th.Muted, d.th.Tint
	}
	return d.th.Accent, mix(d.th.Accent, "#ffffff", 0.93)
}

// exhibit puts one chart, diagram, table or set of metrics on a slide, with
// the report's commentary on it beside or below.
func (d *deck) exhibit(sec report.Section, bl report.Block, comment, commentLabel string) {
	th := d.th
	kicker := fmt.Sprintf("Section %02d · %s", sec.Number, sec.Title)
	source := sourceText(bl)
	notes := []string{}
	if comment != "" {
		notes = append(notes, noteText(comment))
	}
	footer := func(s *slide) {
		if source != "" {
			s.text(padX, bodyBottom-12, bodyW, 14, []para{label(truncateRunes(source, 200), 8.5, th.Muted, false)}, textOpt{})
		}
		s.notes = notes
	}
	// side draws the commentary panel and returns the width left for the figure.
	side := func(s *slide, y float64) float64 {
		if comment == "" {
			return bodyW
		}
		const pw = 250.0
		x := slideW - padX - pw
		text := lede(comment, 260)
		s.box(x, y, pw, bodyBottom-y-24, box{fill: mix(th.Accent, "#ffffff", 0.95), radius: 12})
		s.text(x+18, y+18, pw-36, bodyBottom-y-60, []para{
			{runs: []run{{t: strings.ToUpper(commentLabel)}}, size: 8.5, color: th.Accent, bold: true, after: 8, track: 130},
			{runs: d.rich(text), size: 13, color: th.Ink, leading: 122},
		}, textOpt{})
		return bodyW - pw - 28
	}

	switch bl.Type {
	case report.BlockChart:
		s := d.add()
		y := d.chrome(s, fmt.Sprintf("%s · Figure %d", kicker, bl.Number), bl.Chart.Title)
		s.text(padX, y-20, bodyW, 16, []para{label(bl.Chart.Units, 11, th.Muted, false)}, textOpt{})
		w := side(s, y+4)
		s.draw(chartCanvas(bl.Chart, th), padX, y+4, w, bodyBottom-y-30)
		footer(s)
	case report.BlockDiagram:
		s := d.add()
		y := d.chrome(s, fmt.Sprintf("%s · Figure %d", kicker, bl.Number), bl.Diagram.Title)
		w := side(s, y)
		s.draw(diagramCanvas(bl.Diagram, th), padX, y, w, bodyBottom-y-26)
		footer(s)
	case report.BlockMetrics:
		s := d.add()
		y := d.chrome(s, kicker, nonEmptyStr(bl.Title, sec.Title))
		d.tiles(s, bl.Metrics, padX, y+6, bodyW, 170, 4)
		if comment != "" {
			s.text(padX, y+200, bodyW*0.8, 90, []para{{runs: d.rich(lede(comment, 300)), size: 14, color: th.Body, leading: 124}}, textOpt{})
		}
		s.notes = notes
	case report.BlockTable:
		size := 11.0
		if len(bl.Columns) >= 5 {
			size = 10
		}
		widths := tableWidths(bl.Columns, bl.Rows, bodyW)
		rowH := func(row []string) float64 {
			lines := 1
			for i, c := range row {
				if i < len(widths) {
					lines = max(lines, len(wrap(report.PlainText(c), widths[i]-18, size, 12)))
				}
			}
			return float64(lines)*size*1.3 + 13
		}
		for at, part := 0, 0; at < len(bl.Rows); part++ {
			s := d.add()
			title := bl.Title
			if part > 0 {
				title += " (continued)"
			}
			y := d.chrome(s, fmt.Sprintf("%s · Table %d", kicker, bl.Number), title)
			room := bodyBottom - y - 30 - size*2.2
			if comment != "" && part == 0 {
				room -= 54
			}
			end := at
			for end < len(bl.Rows) && (end == at || room-rowH(bl.Rows[end]) >= 0) {
				room -= rowH(bl.Rows[end])
				end++
			}
			s.table(padX, y, widths, bl.Columns, bl.Rows[at:end], size, d)
			if comment != "" && part == 0 {
				s.text(padX, bodyBottom-70, bodyW, 50, []para{{runs: d.rich(lede(comment, 240)), size: 12.5, color: th.Body, leading: 118}}, textOpt{anchor: "b"})
			}
			footer(s)
			at = end
		}
	}
}

func (d *deck) sources(list []report.Citation, cont bool) {
	th := d.th
	s := d.add()
	title := "Sources"
	if cont {
		title += " (continued)"
	}
	y := d.chrome(s, "References", title)
	colW := (bodyW - 36) / 2
	rowH := (bodyBottom - y) / 6
	for i, c := range list {
		x, yy := padX+float64(i/6)*(colW+36), y+float64(i%6)*rowH
		meta := c.Publisher
		if dt := fmtDate(c.Published); dt != "" {
			meta = strings.TrimPrefix(meta+" · "+dt, " · ")
		}
		s.text(x, yy, 30, rowH-6, []para{label(fmt.Sprint(c.Number), 10, th.Accent, true)}, textOpt{})
		s.text(x+30, yy, colW-30, rowH-6, []para{
			label(truncateRunes(c.Title, 92), 10, th.Ink, true),
			label(truncateRunes(strings.TrimPrefix(meta+" · "+c.TierLabel, " · "), 96), 8.5, th.Muted, false),
			label(truncateRunes(c.URL, 96), 8, th.Faint, false),
		}, textOpt{})
	}
}

// closing says how the deck was made and where its citations resolve.
func (d *deck) closing() {
	r, th := d.r, d.th
	s := d.add()
	y := d.chrome(s, "About this deck", "Method and evidence")
	stats := []report.Metric{
		{Value: fmt.Sprint(len(r.Sources)), Label: "sources cited"},
		{Value: fmt.Sprint(len(r.Evidence)), Label: "findings cited"},
		{Value: fmt.Sprint(len(r.Sections)), Label: "sections"},
	}
	d.tiles(s, stats, padX, y+4, 560, 96, 3)
	refs := "Numbered citations refer to the report's source list"
	if !r.IncludeReferences && r.Metadata.ReferencesFile != "" {
		refs = "Numbered citations refer to the companion file " + r.Metadata.ReferencesFile
	}
	ps := []para{
		{runs: []run{{t: "Research question: “" + truncateRunes(r.Metadata.Topic, 320) + "”"}}, size: 12.5, color: th.Ink, after: 10, leading: 120},
		{runs: []run{{t: "Compiled from public sources by Boku, an automated multi-agent research pipeline: parallel researchers, an independent fact-checker, synthesis and an editor. Every figure on these slides traces to a cited finding. " + refs + ". Speaker notes carry the full text behind each slide."}}, size: 11.5, color: th.Body, after: 10, leading: 124},
		{runs: []run{{t: "Verify material facts before relying on them."}}, size: 11.5, color: th.Muted, leading: 124},
	}
	s.text(padX, y+124, bodyW*0.84, bodyBottom-y-130, ps, textOpt{})
	s.text(padX, bodyBottom-10, bodyW, 14, []para{label(strings.TrimSpace(r.Metadata.Generator+" · Run "+r.Metadata.RunID), 8.5, th.Faint, false)}, textOpt{})
}

// sourceText is an exhibit's provenance line as plain text.
func sourceText(b report.Block) string {
	var parts []string
	if len(b.SourceNums) > 0 {
		refs := make([]string, len(b.SourceNums))
		for i, n := range b.SourceNums {
			refs[i] = fmt.Sprintf("[%d]", n)
		}
		parts = append(parts, "Source: "+strings.Join(refs, ", ")+".")
	}
	if b.AsOf != "" {
		parts = append(parts, "Data as of "+b.AsOf+".")
	}
	if b.Chart != nil && b.Chart.Note != "" {
		parts = append(parts, b.Chart.Note)
	}
	if b.Type == report.BlockTable && b.Text != "" {
		parts = append(parts, noteText(b.Text))
	}
	return strings.Join(parts, " ")
}
