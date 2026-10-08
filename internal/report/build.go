package report

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/research"
)

// Citation markers inside built text: "\uE000" + "1,4" + "\uE001".
const (
	citeOpen  = "\uE000"
	citeClose = "\uE001"
)

// Issue is a problem found while building the report.
type Issue struct {
	Severity string `json:"severity"` // error | warning
	Message  string `json:"message"`
}

func (i Issue) String() string { return i.Severity + ": " + i.Message }

// BuildOptions carries run information for generated content.
type BuildOptions struct {
	Charts   bool
	Diagrams bool
	// Layout is auto, full, compact or paper; auto follows the document's
	// request (full or compact). Paper is chosen by configuration only.
	Layout string
	// IncludeReferences adds the source list and evidence register to the
	// rendered document. They are in Report.Sources/Evidence either way.
	IncludeReferences bool
	// Lenient turns citations of unknown or rejected findings into warnings
	// (the citation is dropped) instead of errors. Used for quick reports
	// written by small local models.
	Lenient bool
	// Method is shown in the generated methodology appendix; nil omits it.
	Method *Method
}

// Method describes how the research was produced.
type Method struct {
	Workstreams     []string
	FactCheckRounds int
	FactCheckStatus string
	FreshnessDays   int
	Limitations     []string
	// Unverified lists points the fact checker could not verify; the report
	// shows them in a highlighted notice after the executive summary.
	Unverified []string
}

var markerRe = regexp.MustCompile(`\s*\[\s*(F\d{1,4}(?:\s*[,;]\s*F\d{1,4})*)\s*\]`)

type builder struct {
	store   *research.Store
	numbers map[string]int // source ID → citation number
	order   []string
	cited   map[string]bool // finding IDs cited anywhere
	issues  []Issue
	figures int
	tables  int
	opt     BuildOptions
	layout  string
}

// Build validates a Document against the evidence store and produces a Report.
// Errors in the returned issues mean the report must not be published.
func Build(doc Document, store *research.Store, meta Metadata, opt BuildOptions) (*Report, []Issue) {
	b := &builder{store: store, numbers: map[string]int{}, cited: map[string]bool{}, opt: opt}
	r := &Report{Metadata: meta}
	if r.Metadata.Title == "" {
		r.Metadata.Title = strings.TrimSpace(doc.Title)
	}
	r.Metadata.Subtitle = nonEmpty(r.Metadata.Subtitle, strings.TrimSpace(doc.Subtitle))
	r.Metadata.ReportType = nonEmpty(r.Metadata.ReportType, nonEmpty(strings.TrimSpace(doc.ReportType), "Research Report"))
	r.IncludeReferences = opt.IncludeReferences
	r.Layout = resolveLayout(opt.Layout, doc.Layout)
	b.layout = r.Layout
	r.Labels = Labels{
		Summary:     label(doc.SummaryTitle, "Executive summary"),
		KeyFindings: label(doc.KeyFindingsTitle, "Key findings"),
		Conclusion:  label(doc.ConclusionTitle, "Conclusion"),
	}
	if r.Layout == LayoutPaper {
		// A paper's summary is always its abstract.
		r.Labels.Summary = "Abstract"
		r.Labels.KeyFindings = label(doc.KeyFindingsTitle, "Highlights")
		for _, k := range doc.Keywords {
			if k = strings.TrimSpace(k); k != "" && len(r.Keywords) < 8 {
				r.Keywords = append(r.Keywords, k)
			}
		}
	}

	for _, p := range doc.ExecutiveSummary {
		if strings.TrimSpace(p) != "" {
			r.ExecutiveSummary = append(r.ExecutiveSummary, Block{Type: BlockParagraph, Text: b.text(p, "executive summary")})
		}
	}
	if opt.Method != nil {
		r.Unverified = opt.Method.Unverified
	}
	for _, k := range doc.KeyFindings {
		if strings.TrimSpace(k.Headline) == "" {
			continue
		}
		r.KeyFindings = append(r.KeyFindings, KeyFinding{
			Headline: b.text(k.Headline, "key findings"),
			Detail:   b.text(k.Detail, "key findings"),
		})
	}
	n := 0
	for _, s := range doc.Sections {
		sec := b.section(s)
		if len(sec.Blocks) == 0 {
			b.warn("section %q has no content; omitted", s.Title)
			continue
		}
		n++
		sec.Number = n
		r.Sections = append(r.Sections, sec)
	}
	for _, p := range doc.Conclusion {
		if strings.TrimSpace(p) != "" {
			r.Conclusion = append(r.Conclusion, Block{Type: BlockParagraph, Text: b.text(p, "conclusion")})
		}
	}
	for _, s := range doc.Appendices {
		if sec := b.section(s); len(sec.Blocks) > 0 {
			r.Appendices = append(r.Appendices, sec)
		}
	}
	if opt.Method != nil {
		r.Appendices = append(r.Appendices, b.methodAppendix(*opt.Method))
	}
	r.Evidence = b.evidence()
	if opt.IncludeReferences {
		if reg := b.evidenceRegister(r.Evidence); len(reg.Blocks) > 0 {
			r.Appendices = append(r.Appendices, reg)
		}
	}

	for i, id := range b.order {
		src := store.Source(id)
		r.Sources = append(r.Sources, Citation{
			Number: i + 1, SourceID: id, Title: src.Title, Publisher: src.Publisher, URL: src.URL,
			Published: src.PublishedAt, Accessed: src.AccessedAt, Tier: int(src.Tier), TierLabel: src.Tier.Label(),
		})
	}
	for _, is := range b.issues {
		if is.Severity == "warning" {
			r.Warnings = append(r.Warnings, is.Message)
		}
	}
	return r, b.issues
}

func (b *builder) errorf(format string, args ...any) {
	b.issues = append(b.issues, Issue{Severity: "error", Message: fmt.Sprintf(format, args...)})
}

// citeProblem records a bad citation: an error, or a warning when lenient.
func (b *builder) citeProblem(format string, args ...any) {
	if b.opt.Lenient {
		b.warn(format+"; citation dropped", args...)
		return
	}
	b.errorf(format, args...)
}

func resolveLayout(configured, requested string) string {
	switch configured {
	case LayoutFull, LayoutCompact, LayoutPaper:
		return configured
	}
	if requested == LayoutCompact {
		return LayoutCompact
	}
	return LayoutFull
}

func label(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 60 {
		return def
	}
	return s
}

func (b *builder) warn(format string, args ...any) {
	b.issues = append(b.issues, Issue{Severity: "warning", Message: fmt.Sprintf(format, args...)})
}

// text resolves finding markers into citation markers.
func (b *builder) text(s, where string) string {
	s = strings.TrimSpace(s)
	return markerRe.ReplaceAllStringFunc(s, func(m string) string {
		ids := splitIDs(markerRe.FindStringSubmatch(m)[1])
		nums := b.cite(ids, where)
		if len(nums) == 0 {
			return ""
		}
		return citeOpen + joinInts(nums) + citeClose
	})
}

// cite returns citation numbers for the sources behind the given findings.
func (b *builder) cite(ids []string, where string) []int {
	var nums []int
	for _, id := range ids {
		f := b.store.Finding(id)
		if f == nil {
			b.citeProblem("%s cites unknown finding %s", where, id)
			continue
		}
		if !f.Usable() {
			b.citeProblem("%s cites rejected finding %s (%s)", where, id, f.Review.Note)
			continue
		}
		b.cited[id] = true
		for _, sid := range f.SourceIDs {
			if b.store.Source(sid) == nil {
				continue
			}
			num, ok := b.numbers[sid]
			if !ok {
				b.order = append(b.order, sid)
				num = len(b.order)
				b.numbers[sid] = num
			}
			if !containsInt(nums, num) {
				nums = append(nums, num)
			}
		}
	}
	sort.Ints(nums)
	return nums
}

func (b *builder) section(s Section) Section {
	out := Section{Title: strings.TrimSpace(s.Title)}
	where := fmt.Sprintf("section %q", out.Title)
	for _, bl := range s.Blocks {
		if nb, ok := b.block(bl, where); ok {
			out.Blocks = append(out.Blocks, nb)
		}
	}
	return out
}

func (b *builder) block(bl Block, where string) (Block, bool) {
	switch bl.Type {
	case BlockSubheading:
		bl.Text = strings.TrimSpace(bl.Text)
		return bl, bl.Text != ""
	case BlockParagraph, BlockCallout:
		if strings.TrimSpace(bl.Text) == "" {
			return bl, false
		}
		bl.Text = b.text(bl.Text, where)
		bl.Title = strings.TrimSpace(bl.Title)
		return bl, true
	case BlockBullets:
		var items []string
		for _, it := range bl.Items {
			if strings.TrimSpace(it) != "" {
				items = append(items, b.text(it, where))
			}
		}
		bl.Items = items
		return bl, len(items) > 0
	case BlockTable:
		return b.table(bl, where)
	case BlockChart:
		if !b.opt.Charts {
			return bl, false
		}
		return b.chart(bl, where)
	case BlockDiagram:
		if !b.opt.Diagrams {
			return bl, false
		}
		return b.diagram(bl, where)
	}
	b.warn("%s: unknown block type %q dropped", where, bl.Type)
	return bl, false
}

func (b *builder) table(bl Block, where string) (Block, bool) {
	cols := len(bl.Columns)
	if cols == 0 || len(bl.Rows) == 0 {
		b.warn("%s: empty table %q dropped", where, bl.Title)
		return bl, false
	}
	var rows [][]string
	for _, row := range bl.Rows {
		if len(row) != cols {
			b.warn("%s: table %q row had %d cells for %d columns; normalised", where, bl.Title, len(row), cols)
			fixed := make([]string, cols)
			copy(fixed, row)
			row = fixed
		}
		for i := range row {
			row[i] = b.text(row[i], where)
		}
		rows = append(rows, row)
	}
	bl.Rows = rows
	bl.Text = b.text(bl.Text, where)
	bl.SourceNums = b.provenance(bl.FindingIDs, where, "table "+bl.Title)
	b.tables++
	bl.Number = b.tables
	return bl, true
}

func (b *builder) provenance(ids []string, where, what string) []int {
	if len(ids) == 0 {
		b.warn("%s: %s has no finding_ids; source line omitted", where, what)
		return nil
	}
	return b.cite(ids, where)
}

func (b *builder) chart(bl Block, where string) (Block, bool) {
	c := bl.Chart
	if c == nil {
		b.warn("%s: chart block without chart data dropped", where)
		return bl, false
	}
	name := fmt.Sprintf("chart %q", c.Title)
	switch {
	case strings.TrimSpace(c.Title) == "":
		b.warn("%s: untitled chart dropped", where)
		return bl, false
	case strings.TrimSpace(c.Units) == "":
		b.warn("%s: %s has no units; dropped", where, name)
		return bl, false
	case len(c.Labels) == 0 || len(c.Series) == 0:
		b.warn("%s: %s has no data; dropped", where, name)
		return bl, false
	case len(bl.FindingIDs) == 0:
		b.warn("%s: %s cites no findings; dropped (chart data must be traceable)", where, name)
		return bl, false
	}
	switch c.Kind {
	case "bar", "column", "line":
	default:
		c.Kind = "column"
	}
	for _, s := range c.Series {
		if len(s.Values) != len(c.Labels) {
			b.warn("%s: %s series %q has %d values for %d labels; dropped", where, name, s.Name, len(s.Values), len(c.Labels))
			return bl, false
		}
	}
	var evidence []string
	for _, id := range bl.FindingIDs {
		if f := b.store.Finding(id); f != nil && f.Usable() {
			evidence = append(evidence, f.Claim, f.Evidence)
			if bl.AsOf == "" {
				bl.AsOf = f.AsOf
			}
		}
	}
	nums := numbersIn(strings.Join(evidence, " "))
	for _, s := range c.Series {
		for _, v := range s.Values {
			if !traceable(v, nums) {
				b.warn("%s: %s value %v does not appear in cited findings %v; chart dropped", where, name, v, bl.FindingIDs)
				return bl, false
			}
		}
	}
	if strings.TrimSpace(bl.AsOf) == "" {
		b.warn("%s: %s has no date; dropped", where, name)
		return bl, false
	}
	bl.SourceNums = b.cite(bl.FindingIDs, where)
	b.figures++
	bl.Number = b.figures
	return bl, true
}

func (b *builder) diagram(bl Block, where string) (Block, bool) {
	d := bl.Diagram
	if d == nil || len(d.Nodes) == 0 {
		b.warn("%s: diagram without nodes dropped", where)
		return bl, false
	}
	if len(d.Nodes) > 24 {
		b.warn("%s: diagram %q has %d nodes (max 24); dropped", where, d.Title, len(d.Nodes))
		return bl, false
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		ids[n.ID] = true
	}
	var edges []Edge
	for _, e := range d.Edges {
		if ids[e.From] && ids[e.To] && e.From != e.To {
			edges = append(edges, e)
		} else {
			b.warn("%s: diagram %q edge %s→%s references unknown node; dropped", where, d.Title, e.From, e.To)
		}
	}
	d.Edges = edges
	switch d.Kind {
	case "architecture", "process", "timeline":
	default:
		d.Kind = "process"
	}
	if len(bl.FindingIDs) > 0 {
		bl.SourceNums = b.cite(bl.FindingIDs, where)
	}
	b.figures++
	bl.Number = b.figures
	return bl, true
}

func (b *builder) methodAppendix(m Method) Section {
	st := b.store.Stats()
	sec := Section{Title: "Methodology and evidence quality"}
	add := func(bl Block) { sec.Blocks = append(sec.Blocks, bl) }
	doc := "report"
	if b.layout == LayoutPaper {
		doc = "paper"
	}
	check := fmt.Sprintf("An independent fact-checking agent reviewed the corpus over %d round(s) (final status: %s) before synthesis and editing.", m.FactCheckRounds, nonEmpty(m.FactCheckStatus, "n/a"))
	if m.FactCheckRounds == 0 {
		check = "Fact-checking was not run for this " + doc + ", so claims have not been independently verified; treat it as a first pass."
	}
	add(Block{Type: BlockParagraph, Text: fmt.Sprintf(
		"This "+doc+" was produced by Boku, a multi-agent research pipeline. Research workstreams (%s) gathered evidence from public sources; each claim was recorded with its sources and date. %s Information dated within %d days of publication is labelled current; older material is labelled historical.",
		strings.Join(m.Workstreams, ", "), check, m.FreshnessDays)})

	rows := [][]string{
		{"Findings recorded", strconv.Itoa(st.Findings)},
		{"Findings used (after fact-check)", strconv.Itoa(st.Usable)},
		{"Findings rejected", strconv.Itoa(st.Rejected)},
		{"Findings flagged as weakly supported", strconv.Itoa(st.Flagged)},
		{"Distinct sources supporting usable findings", strconv.Itoa(st.CitedSources)},
	}
	for _, t := range research.SortedTiers(st.SourcesByTier) {
		rows = append(rows, []string{"  " + t.Label(), strconv.Itoa(st.SourcesByTier[t])})
	}
	b.tables++
	add(Block{Type: BlockTable, Title: "Evidence base", Columns: []string{"Measure", "Count"}, Rows: rows, Number: b.tables})

	if len(m.Unverified) > 0 {
		add(Block{Type: BlockSubheading, Text: "Not verified"})
		add(Block{Type: BlockCallout, Tone: "unverified", Title: "Not verified by fact-check", Text: "The fact checker could not verify the following, and statements in this report that touch on them should be treated as unconfirmed."})
		add(Block{Type: BlockBullets, Items: m.Unverified})
	}
	if len(m.Limitations) > 0 {
		add(Block{Type: BlockSubheading, Text: "Limitations and unresolved questions"})
		add(Block{Type: BlockBullets, Items: m.Limitations})
	}
	return sec
}

// evidence lists every finding the report cites with its evidence status.
func (b *builder) evidence() []EvidenceEntry {
	var out []EvidenceEntry
	for _, f := range b.store.Usable() {
		if !b.cited[f.ID] {
			continue
		}
		var nums []int
		for _, sid := range f.SourceIDs {
			if n, ok := b.numbers[sid]; ok {
				nums = append(nums, n)
			}
		}
		if len(nums) == 0 {
			continue
		}
		status := capitalize(nonEmpty(f.Temporal, research.TemporalUnknown))
		if f.Review.Status == research.ReviewFlagged {
			status += " · weak support"
		}
		out = append(out, EvidenceEntry{ID: f.ID, Claim: f.Claim, Status: status, AsOf: f.AsOf, Sources: nums})
	}
	return out
}

// evidenceRegister renders the evidence entries as an appendix table.
func (b *builder) evidenceRegister(entries []EvidenceEntry) Section {
	sec := Section{Title: "Evidence register"}
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.ID, truncate(e.Claim, 220), e.Status, nonEmpty(e.AsOf, "—"), citeOpen + joinInts(e.Sources) + citeClose})
	}
	if len(rows) == 0 {
		return sec
	}
	sec.Blocks = append(sec.Blocks, Block{Type: BlockParagraph, Text: "Each cited claim with its evidence status. Current: dated within the freshness window. Historical: older than the window. Estimated: a published estimate, not a reported figure. Derived: calculated from reported figures. Inference: analytical interpretation. Unknown: no reliable date."})
	b.tables++
	sec.Blocks = append(sec.Blocks, Block{Type: BlockTable, Title: "Cited findings", Columns: []string{"ID", "Claim", "Status", "As of", "Sources"}, Rows: rows, Number: b.tables})
	return sec
}

var numRe = regexp.MustCompile(`-?\d[\d,]*(?:\.\d+)?`)

func numbersIn(s string) []float64 {
	var out []float64
	for _, m := range numRe.FindAllString(s, -1) {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", ""), 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// traceable reports whether v appears in nums, allowing unit rescaling
// (e.g. "$4.2 billion" charted as 4,200 $M).
func traceable(v float64, nums []float64) bool {
	for _, n := range nums {
		for _, scale := range []float64{1, 1e3, 1e6, 1e9, 1e-3, 1e-6, 1e-9} {
			if approxEqual(v, n*scale) {
				return true
			}
		}
	}
	return false
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func splitIDs(s string) []string {
	var ids []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, p)
		}
	}
	return ids
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
