package research

import (
	"encoding/json"
	"fmt"
	"strings"
)

// promptFinding and promptSource are the compact forms shown to downstream agents.
type promptFinding struct {
	ID         string   `json:"id"`
	Claim      string   `json:"claim"`
	Evidence   string   `json:"evidence,omitempty"`
	Sources    []string `json:"sources"`
	AsOf       string   `json:"as_of,omitempty"`
	Kind       string   `json:"kind"`
	Temporal   string   `json:"temporal"`
	Confidence float64  `json:"confidence"`
	Topic      string   `json:"topic,omitempty"`
	Review     string   `json:"review,omitempty"`
	ReviewNote string   `json:"review_note,omitempty"`
}

type promptSource struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Publisher string `json:"publisher,omitempty"`
	URL       string `json:"url"`
	Published string `json:"published,omitempty"`
	Tier      Tier   `json:"tier"`
}

// CorpusJSON renders findings and their sources as compact JSON for agent
// prompts. With usableOnly, rejected findings are omitted.
func (s *Store) CorpusJSON(usableOnly bool) string {
	used := map[string]bool{}
	var fs []promptFinding
	for _, f := range s.Findings {
		if usableOnly && !f.Usable() {
			continue
		}
		pf := promptFinding{
			ID: f.ID, Claim: f.Claim, Evidence: f.Evidence, Sources: f.SourceIDs,
			AsOf: f.AsOf, Kind: f.Kind, Temporal: f.Temporal, Confidence: f.Confidence, Topic: f.Topic,
		}
		if f.Review.Status != ReviewPending && f.Review.Status != ReviewAccepted {
			pf.Review, pf.ReviewNote = f.Review.Status, f.Review.Note
		}
		fs = append(fs, pf)
		for _, id := range f.SourceIDs {
			used[id] = true
		}
	}
	var ss []promptSource
	for _, src := range s.Sources {
		if !used[src.ID] {
			continue
		}
		ps := promptSource{ID: src.ID, Title: src.Title, Publisher: src.Publisher, URL: src.URL, Tier: src.Tier}
		if src.PublishedAt != nil {
			ps.Published = src.PublishedAt.Format("2006-01-02")
		}
		ss = append(ss, ps)
	}
	b, _ := json.MarshalIndent(map[string]any{"findings": fs, "sources": ss}, "", " ")
	return string(b)
}

// ClaimList is a one-line-per-finding summary used to stop agents repeating
// research that already exists.
func (s *Store) ClaimList(max int) string {
	var b strings.Builder
	n := 0
	for _, f := range s.Findings {
		if !f.Usable() {
			continue
		}
		if n == max {
			fmt.Fprintf(&b, "… and %d more\n", len(s.Findings)-n)
			break
		}
		fmt.Fprintf(&b, "- %s: %s\n", f.ID, truncate(f.Claim, 160))
		n++
	}
	return b.String()
}

// Markdown renders a findings file for human inspection.
func (s *Store) Markdown(title string, filter func(Finding) bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	for _, f := range s.Findings {
		if filter != nil && !filter(f) {
			continue
		}
		fmt.Fprintf(&b, "## %s — %s\n\n", f.ID, f.Claim)
		if f.Evidence != "" {
			fmt.Fprintf(&b, "%s\n\n", f.Evidence)
		}
		fmt.Fprintf(&b, "- kind: %s · temporal: %s · as of: %s · confidence: %.2f · review: %s\n",
			f.Kind, f.Temporal, nonEmpty(f.AsOf, "n/a"), f.Confidence, f.Review.Status)
		for _, id := range f.SourceIDs {
			if src := s.Source(id); src != nil {
				fmt.Fprintf(&b, "- [%s] %s — %s (tier %d)\n", src.ID, src.Title, src.URL, src.Tier)
			}
		}
		if f.Review.Note != "" {
			fmt.Fprintf(&b, "- fact-check: %s\n", f.Review.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
