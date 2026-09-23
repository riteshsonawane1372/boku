// Package research holds Boku's evidence model: sources, findings and the
// store that de-duplicates them and keeps claims linked to their sources.
//
//	Claim ──▶ Evidence ──▶ Source(s)
//
// Findings carry stable IDs (F001…) and sources carry S001…; these IDs are what
// the fact checker, synthesis and editorial agents reference, and what the
// renderer resolves into numbered citations.
package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind says how a claim was obtained.
const (
	KindReported  = "reported"  // stated by a source
	KindEstimated = "estimated" // an estimate published by a source
	KindDerived   = "derived"   // calculated by the agent from reported figures
	KindInference = "inference" // the agent's interpretation
)

// Temporal classes shown in the report. Computed deterministically; agents
// never assert that information is "current".
const (
	TemporalCurrent    = "current"
	TemporalHistorical = "historical"
	TemporalEstimated  = "estimated"
	TemporalDerived    = "derived"
	TemporalInference  = "inference"
	TemporalUnknown    = "unknown"
)

// Review statuses assigned by the fact checker.
const (
	ReviewPending  = "pending"
	ReviewAccepted = "accepted"
	ReviewFlagged  = "flagged"  // weak support; usable with caveats
	ReviewRejected = "rejected" // unsupported or contradicted; excluded
)

// Finding is one evidence-backed claim.
type Finding struct {
	ID         string   `json:"id"`
	Claim      string   `json:"claim"`
	Evidence   string   `json:"evidence"`
	SourceIDs  []string `json:"source_ids"`
	Confidence float64  `json:"confidence"`
	// AsOf is the date the claim describes: YYYY, YYYY-MM or YYYY-MM-DD.
	AsOf       string `json:"as_of,omitempty"`
	Kind       string `json:"kind"`
	Temporal   string `json:"temporal"`
	Topic      string `json:"topic,omitempty"`
	Workstream string `json:"workstream"`
	TaskID     string `json:"task_id"`
	Notes      string `json:"notes,omitempty"`
	Review     Review `json:"review"`
}

// Review records the fact checker's verdict on a finding.
type Review struct {
	Status  string `json:"status"`
	Verdict string `json:"verdict,omitempty"`
	Note    string `json:"note,omitempty"`
	Round   int    `json:"round,omitempty"`
}

// Usable reports whether the finding may be used in the report.
func (f Finding) Usable() bool {
	return f.Review.Status != ReviewRejected
}

// ResearchOutput is what research agents return (see prompts/schemas/research.json).
type ResearchOutput struct {
	Summary       string       `json:"summary"`
	Findings      []RawFinding `json:"findings"`
	Sources       []RawSource  `json:"sources"`
	OpenQuestions []string     `json:"open_questions"`
}

type RawFinding struct {
	Claim      string   `json:"claim"`
	Evidence   string   `json:"evidence"`
	SourceRefs []string `json:"source_refs"`
	Confidence float64  `json:"confidence"`
	AsOf       string   `json:"as_of"`
	Kind       string   `json:"kind"`
	Topic      string   `json:"topic"`
	Notes      string   `json:"notes"`
}

type RawSource struct {
	Ref         string `json:"ref"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Publisher   string `json:"publisher"`
	PublishedAt string `json:"published_at"`
	SourceType  string `json:"source_type"`
	Tier        int    `json:"tier"`
}

// Store is the run's evidence store. It is not safe for concurrent use; the
// orchestrator ingests results sequentially.
type Store struct {
	Sources  []Source  `json:"sources"`
	Findings []Finding `json:"findings"`

	byURL map[string]int
}

func NewStore() *Store { return &Store{byURL: map[string]int{}} }

// IngestOptions controls classification during ingest.
type IngestOptions struct {
	TaskID        string
	Workstream    string
	Now           time.Time
	FreshnessDays int
}

// IngestReport summarises what an ingest did.
type IngestReport struct {
	FindingsAdded  int
	FindingsMerged int
	SourcesAdded   int
	Unsourced      int
	Warnings       []string
}

// Ingest adds an agent's output, de-duplicating sources by normalised URL and
// findings by near-identical claims. Non-inference findings without a
// resolvable source are rejected: every factual claim must keep its source.
func (s *Store) Ingest(out ResearchOutput, opt IngestOptions) IngestReport {
	if s.byURL == nil {
		s.reindex()
	}
	var rep IngestReport
	local := map[string]string{} // agent ref → store ID
	for _, rs := range out.Sources {
		if strings.TrimSpace(rs.URL) == "" {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("source %q has no URL; dropped", rs.Title))
			continue
		}
		id, added := s.addSource(rs, opt)
		if added {
			rep.SourcesAdded++
		}
		if rs.Ref != "" {
			local[rs.Ref] = id
		}
		local[rs.URL] = id
	}

	for _, rf := range out.Findings {
		claim := strings.TrimSpace(rf.Claim)
		if claim == "" {
			continue
		}
		var ids []string
		for _, ref := range rf.SourceRefs {
			id, ok := local[strings.TrimSpace(ref)]
			if !ok {
				// Agents sometimes cite a URL they did not list.
				if strings.HasPrefix(ref, "http") {
					id, _ = s.addSource(RawSource{URL: ref, Title: ref}, opt)
					ok = true
				}
			}
			if ok && !contains(ids, id) {
				ids = append(ids, id)
			}
		}
		kind := normalizeKind(rf.Kind)
		f := Finding{
			Claim:      claim,
			Evidence:   strings.TrimSpace(rf.Evidence),
			SourceIDs:  ids,
			Confidence: clamp01(rf.Confidence),
			AsOf:       strings.TrimSpace(rf.AsOf),
			Kind:       kind,
			Topic:      strings.TrimSpace(rf.Topic),
			Workstream: opt.Workstream,
			TaskID:     opt.TaskID,
			Notes:      strings.TrimSpace(rf.Notes),
			Review:     Review{Status: ReviewPending},
		}
		if len(ids) == 0 && kind != KindInference {
			f.Review = Review{Status: ReviewRejected, Verdict: "unsourced", Note: "no resolvable source"}
			rep.Unsourced++
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("unsourced claim rejected: %q", truncate(claim, 80)))
		}
		if f.AsOf == "" {
			f.AsOf = s.newestSourceDate(ids)
		}
		f.Temporal = ClassifyTemporal(f.Kind, f.AsOf, opt.Now, opt.FreshnessDays)

		if i := s.findDuplicate(f); i >= 0 {
			existing := &s.Findings[i]
			for _, id := range f.SourceIDs {
				if !contains(existing.SourceIDs, id) {
					existing.SourceIDs = append(existing.SourceIDs, id)
				}
			}
			if existing.Review.Status == ReviewRejected && existing.Review.Verdict == "unsourced" && len(existing.SourceIDs) > 0 {
				existing.Review = Review{Status: ReviewPending}
			}
			rep.FindingsMerged++
			continue
		}
		f.ID = fmt.Sprintf("F%03d", len(s.Findings)+1)
		s.Findings = append(s.Findings, f)
		rep.FindingsAdded++
	}
	return rep
}

func (s *Store) addSource(rs RawSource, opt IngestOptions) (string, bool) {
	key := NormalizeURL(rs.URL)
	if i, ok := s.byURL[key]; ok {
		src := &s.Sources[i]
		if !contains(src.FoundBy, opt.TaskID) && opt.TaskID != "" {
			src.FoundBy = append(src.FoundBy, opt.TaskID)
		}
		// Fill gaps from the newer sighting; keep the stricter tier.
		if (src.Title == "" || src.Title == src.URL) && rs.Title != "" {
			src.Title = rs.Title
		}
		if src.Publisher == "" {
			src.Publisher = rs.Publisher
		}
		if src.PublishedAt == nil {
			src.PublishedAt = ParseDate(rs.PublishedAt)
		}
		if t := ResolveTier(Tier(rs.Tier), rs.URL); rs.Tier > 0 && t > src.Tier {
			src.Tier = t
		}
		return src.ID, false
	}
	src := Source{
		ID:          fmt.Sprintf("S%03d", len(s.Sources)+1),
		URL:         strings.TrimSpace(rs.URL),
		Title:       strings.TrimSpace(rs.Title),
		Publisher:   strings.TrimSpace(rs.Publisher),
		PublishedAt: ParseDate(rs.PublishedAt),
		AccessedAt:  opt.Now,
		SourceType:  strings.TrimSpace(rs.SourceType),
		Tier:        ResolveTier(Tier(rs.Tier), rs.URL),
	}
	if src.Title == "" {
		src.Title = src.URL
	}
	if opt.TaskID != "" {
		src.FoundBy = []string{opt.TaskID}
	}
	s.Sources = append(s.Sources, src)
	s.byURL[key] = len(s.Sources) - 1
	return src.ID, true
}

func (s *Store) newestSourceDate(ids []string) string {
	var newest *time.Time
	for _, id := range ids {
		if src := s.Source(id); src != nil && src.PublishedAt != nil {
			if newest == nil || src.PublishedAt.After(*newest) {
				newest = src.PublishedAt
			}
		}
	}
	if newest == nil {
		return ""
	}
	return newest.Format("2006-01-02")
}

func (s *Store) findDuplicate(f Finding) int {
	a := tokenSet(f.Claim)
	for i, g := range s.Findings {
		if jaccard(a, tokenSet(g.Claim)) >= 0.85 {
			return i
		}
	}
	return -1
}

func (s *Store) reindex() {
	s.byURL = make(map[string]int, len(s.Sources))
	for i, src := range s.Sources {
		s.byURL[NormalizeURL(src.URL)] = i
	}
}

// Source returns the source with the given ID, or nil.
func (s *Store) Source(id string) *Source {
	for i := range s.Sources {
		if s.Sources[i].ID == id {
			return &s.Sources[i]
		}
	}
	return nil
}

// Finding returns the finding with the given ID, or nil.
func (s *Store) Finding(id string) *Finding {
	for i := range s.Findings {
		if s.Findings[i].ID == id {
			return &s.Findings[i]
		}
	}
	return nil
}

// Usable returns findings that were not rejected.
func (s *Store) Usable() []Finding {
	var out []Finding
	for _, f := range s.Findings {
		if f.Usable() {
			out = append(out, f)
		}
	}
	return out
}

// CitedSources returns the sources referenced by usable findings, in ID order.
func (s *Store) CitedSources() []Source {
	used := map[string]bool{}
	for _, f := range s.Usable() {
		for _, id := range f.SourceIDs {
			used[id] = true
		}
	}
	var out []Source
	for _, src := range s.Sources {
		if used[src.ID] {
			out = append(out, src)
		}
	}
	return out
}

// Stats summarises the corpus for gates and the methodology appendix.
type Stats struct {
	Findings      int            `json:"findings"`
	Usable        int            `json:"usable"`
	Rejected      int            `json:"rejected"`
	Flagged       int            `json:"flagged"`
	Sources       int            `json:"sources"`
	CitedSources  int            `json:"cited_sources"`
	SourcesByTier map[Tier]int   `json:"sources_by_tier"`
	ByTemporal    map[string]int `json:"by_temporal"`
	ByKind        map[string]int `json:"by_kind"`
}

func (s *Store) Stats() Stats {
	st := Stats{SourcesByTier: map[Tier]int{}, ByTemporal: map[string]int{}, ByKind: map[string]int{}}
	st.Findings = len(s.Findings)
	st.Sources = len(s.Sources)
	for _, f := range s.Findings {
		switch f.Review.Status {
		case ReviewRejected:
			st.Rejected++
			continue
		case ReviewFlagged:
			st.Flagged++
		}
		st.Usable++
		st.ByTemporal[f.Temporal]++
		st.ByKind[f.Kind]++
	}
	for _, src := range s.CitedSources() {
		st.CitedSources++
		st.SourcesByTier[src.Tier]++
	}
	return st
}

// Save writes sources.json and findings.json into dir.
func (s *Store) Save(dir string) error {
	if err := writeJSON(filepath.Join(dir, "sources.json"), s.Sources); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "findings.json"), s.Findings)
}

// LoadStore reads a store saved by Save. Missing files yield an empty store.
func LoadStore(dir string) (*Store, error) {
	s := NewStore()
	for name, v := range map[string]any{"sources.json": &s.Sources, "findings.json": &s.Findings} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	s.reindex()
	return s, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ClassifyTemporal decides how a claim's date should be presented.
// Periods are judged by their end (a "2025" figure describes all of 2025).
func ClassifyTemporal(kind, asOf string, now time.Time, freshnessDays int) string {
	switch kind {
	case KindEstimated:
		return TemporalEstimated
	case KindDerived:
		return TemporalDerived
	case KindInference:
		return TemporalInference
	}
	end, ok := PeriodEnd(asOf)
	if !ok {
		return TemporalUnknown
	}
	if now.Sub(end) <= time.Duration(freshnessDays)*24*time.Hour {
		return TemporalCurrent
	}
	return TemporalHistorical
}

var (
	reDay     = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
	reMonth   = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
	reYear    = regexp.MustCompile(`^(\d{4})$`)
	reQuarter = regexp.MustCompile(`(?i)^(?:q([1-4])\s*(\d{4})|(\d{4})\s*-?\s*q([1-4]))$`)
	reFY      = regexp.MustCompile(`(?i)^fy\s*(\d{4})$`)
)

// PeriodEnd returns the last instant of a date or period string.
func PeriodEnd(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	if m := reDay.FindStringSubmatch(s); m != nil {
		t, err := time.Parse("2006-01-02", m[0])
		return t.Add(24*time.Hour - time.Nanosecond), err == nil
	}
	if m := reMonth.FindStringSubmatch(s); m != nil {
		return time.Date(atoi(m[1]), time.Month(atoi(m[2]))+1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond), true
	}
	if m := reQuarter.FindStringSubmatch(s); m != nil {
		q, y := atoi(m[1]), atoi(m[2])
		if q == 0 {
			y, q = atoi(m[3]), atoi(m[4])
		}
		return time.Date(y, time.Month(q*3)+1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond), true
	}
	if m := reYear.FindStringSubmatch(s); m != nil {
		return time.Date(atoi(m[1])+1, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond), true
	}
	if m := reFY.FindStringSubmatch(s); m != nil {
		return time.Date(atoi(m[1])+1, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond), true
	}
	return time.Time{}, false
}

// ParseDate parses a full or partial date; partial dates resolve to the period start.
func ParseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return &t
		}
	}
	return nil
}

func normalizeKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case KindEstimated, "estimate":
		return KindEstimated
	case KindDerived, "calculated", "derived_calculation":
		return KindDerived
	case KindInference, "interpretation", "analysis", "agent_inference":
		return KindInference
	}
	return KindReported
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}.%$]+`)

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		set[strings.Trim(w, ".")] = true
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SortedTiers returns tier keys in ascending order (for stable rendering).
func SortedTiers(m map[Tier]int) []Tier {
	var ts []Tier
	for t := range m {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	return ts
}
