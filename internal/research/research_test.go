package research

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

func opts(task string) IngestOptions {
	return IngestOptions{TaskID: task, Workstream: task, Now: now, FreshnessDays: 365}
}

func TestNormalizeURL(t *testing.T) {
	same := []string{
		"https://www.Example.com/report/?utm_source=x&b=2&a=1#section",
		"http://example.com/report?a=1&b=2",
		"https://example.com/report/?a=1&b=2&fbclid=zz",
	}
	want := NormalizeURL(same[0])
	for _, u := range same[1:] {
		if got := NormalizeURL(u); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", u, got, want)
		}
	}
}

func TestResolveTier(t *testing.T) {
	cases := []struct {
		claimed Tier
		url     string
		want    Tier
	}{
		{1, "https://www.reddit.com/r/kubernetes/x", 4},
		{1, "https://medium.com/@someone/post", 3},
		{3, "https://www.sec.gov/Archives/edgar/x", 1},
		{2, "https://www.ft.com/content/x", 2},
		{0, "https://example.com", 3},
		{3, "https://data.europa.eu/x", 1},
	}
	for _, c := range cases {
		if got := ResolveTier(c.claimed, c.url); got != c.want {
			t.Errorf("ResolveTier(%d, %s) = %d, want %d", c.claimed, c.url, got, c.want)
		}
	}
}

func TestIngestDeduplicatesSourcesAndFindings(t *testing.T) {
	s := NewStore()
	s.Ingest(ResearchOutput{
		Sources:  []RawSource{{Ref: "s1", URL: "https://example.com/a?utm_source=x", Title: "A", Tier: 2}},
		Findings: []RawFinding{{Claim: "Company X operates 50 data centers worldwide.", SourceRefs: []string{"s1"}, AsOf: "2026-06", Confidence: 0.9}},
	}, opts("primary"))
	rep := s.Ingest(ResearchOutput{
		Sources: []RawSource{
			{Ref: "a", URL: "https://www.example.com/a", Title: "A again", Tier: 1},
			{Ref: "b", URL: "https://other.org/b", Title: "B", Tier: 2},
		},
		Findings: []RawFinding{{Claim: "Company X operates 50 data centers worldwide", SourceRefs: []string{"a", "b"}}},
	}, opts("market"))

	if len(s.Sources) != 2 {
		t.Fatalf("sources = %d, want 2 (dedup by URL)", len(s.Sources))
	}
	if rep.SourcesAdded != 1 || rep.FindingsMerged != 1 || rep.FindingsAdded != 0 {
		t.Errorf("report = %+v", rep)
	}
	if len(s.Findings) != 1 || len(s.Findings[0].SourceIDs) != 2 {
		t.Fatalf("finding sources not merged: %+v", s.Findings)
	}
	if got := s.Sources[0].FoundBy; len(got) != 2 {
		t.Errorf("FoundBy = %v", got)
	}
}

func TestIngestRejectsUnsourcedFacts(t *testing.T) {
	s := NewStore()
	rep := s.Ingest(ResearchOutput{Findings: []RawFinding{
		{Claim: "Revenue grew 40% in 2025.", SourceRefs: []string{"missing"}},
		{Claim: "This suggests consolidation is likely.", Kind: "inference"},
	}}, opts("financial"))
	if rep.Unsourced != 1 {
		t.Fatalf("unsourced = %d", rep.Unsourced)
	}
	if s.Findings[0].Usable() {
		t.Error("unsourced reported claim must not be usable")
	}
	if !s.Findings[1].Usable() || s.Findings[1].Temporal != TemporalInference {
		t.Errorf("inference should be usable and labelled: %+v", s.Findings[1])
	}
}

func TestIngestAcceptsInlineURLRefs(t *testing.T) {
	s := NewStore()
	s.Ingest(ResearchOutput{Findings: []RawFinding{{Claim: "x", SourceRefs: []string{"https://example.com/p"}}}}, opts("p"))
	if len(s.Sources) != 1 || !s.Findings[0].Usable() {
		t.Fatalf("inline URL ref not resolved: %+v", s)
	}
}

func TestClassifyTemporal(t *testing.T) {
	cases := []struct{ kind, asOf, want string }{
		{KindReported, "2026-08-10", TemporalCurrent},
		{KindReported, "2025", TemporalCurrent}, // period ends 2025-12-31, within 365d
		{KindReported, "2024", TemporalHistorical},
		{KindReported, "Q1 2025", TemporalHistorical},
		{KindReported, "2025-Q4", TemporalCurrent},
		{KindReported, "", TemporalUnknown},
		{KindReported, "recently", TemporalUnknown},
		{KindEstimated, "2026", TemporalEstimated},
		{KindDerived, "2026", TemporalDerived},
	}
	for _, c := range cases {
		if got := ClassifyTemporal(c.kind, c.asOf, now, 365); got != c.want {
			t.Errorf("ClassifyTemporal(%s, %q) = %s, want %s", c.kind, c.asOf, got, c.want)
		}
	}
}

func TestAsOfFallsBackToSourceDate(t *testing.T) {
	s := NewStore()
	s.Ingest(ResearchOutput{
		Sources:  []RawSource{{Ref: "s1", URL: "https://example.com", PublishedAt: "2019-03-01"}},
		Findings: []RawFinding{{Claim: "Old fact", SourceRefs: []string{"s1"}}},
	}, opts("p"))
	if f := s.Findings[0]; f.AsOf != "2019-03-01" || f.Temporal != TemporalHistorical {
		t.Errorf("got as_of=%q temporal=%s", f.AsOf, f.Temporal)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	s.Ingest(ResearchOutput{
		Sources:  []RawSource{{Ref: "s1", URL: "https://example.com", Title: "E"}},
		Findings: []RawFinding{{Claim: "c", SourceRefs: []string{"s1"}}},
	}, opts("p"))
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	l, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The reloaded store must keep de-duplicating.
	l.Ingest(ResearchOutput{Sources: []RawSource{{Ref: "x", URL: "https://www.example.com/"}}}, opts("q"))
	if len(l.Sources) != 1 || len(l.Findings) != 1 {
		t.Fatalf("round trip lost state: %d sources, %d findings", len(l.Sources), len(l.Findings))
	}
}
