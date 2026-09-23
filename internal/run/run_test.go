package run

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riteshsonawane1372/boku/internal/config"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"How is Kubernetes being used for AI infrastructure?": "kubernetes-used-ai-infrastructure",
		"JPMorgan's annual financial performance":             "jpmorgan-s-annual-financial-performance",
		"???":   "report",
		"the a": "the-a",
	}
	for in, want := range cases {
		if got := Slug(in, 40); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slug(strings.Repeat("word ", 50), 20); len(got) > 20 {
		t.Errorf("slug too long: %q", got)
	}
}

func TestCreateOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 23, 7, 45, 0, 0, time.UTC)
	r, err := Create(dir, "AI agents in the enterprise", config.Default(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.Dir, "2026-09-23T074500-ai-agents-enterprise") {
		t.Errorf("dir = %s", r.Dir)
	}
	if _, err := Create(dir, "AI agents in the enterprise", config.Default(), now); err == nil {
		t.Error("duplicate run directory allowed")
	}
	r.StageStart(StagePlan)
	r.StageDone(StagePlan, "3 workstreams")
	r.StageFail(StageResearch, StatusFailed, errors.New("boom"))
	r.RecordTask(Task{ID: "planner", CostUSD: 0.5, Attempts: 1})
	r.RecordTask(Task{ID: "planner", CostUSD: 0.25, Attempts: 1}) // retried on resume

	o, err := Open(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	m := o.Manifest()
	if m.Topic != "AI agents in the enterprise" || o.StageStatus(StagePlan) != StatusDone || o.StageStatus(StageResearch) != StatusFailed {
		t.Errorf("manifest = %+v", m)
	}
	if m.CostUSD != 0.75 || m.Tasks["planner"].Attempts != 2 {
		t.Errorf("cost=%v attempts=%d", m.CostUSD, m.Tasks["planner"].Attempts)
	}
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("opening a non-run directory should fail")
	}
}

func TestConcurrentTaskRecording(t *testing.T) {
	r, err := Create(t.TempDir(), "x", config.Default(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.RecordTask(Task{ID: string(rune('a' + i)), CostUSD: 0.1})
		}(i)
	}
	wg.Wait()
	if m := r.Manifest(); len(m.Tasks) != 20 {
		t.Errorf("tasks = %d", len(m.Tasks))
	}
}

func TestReadJSONMissingAndMalformed(t *testing.T) {
	r, _ := Create(t.TempDir(), "x", config.Default(), time.Now())
	var v map[string]any
	if ok, err := r.ReadJSON("nope.json", &v); ok || err != nil {
		t.Errorf("missing: ok=%v err=%v", ok, err)
	}
	_ = r.WriteFile("bad.json", []byte("{not json"))
	if _, err := r.ReadJSON("bad.json", &v); err == nil {
		t.Error("malformed JSON not reported")
	}
}
