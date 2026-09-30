package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/render"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/prompts"
)

// fakeAgent answers each role with canned, schema-shaped output. Hooks let
// tests inject failures per task ID.
type fakeAgent struct {
	mu    sync.Mutex
	calls map[string]int // task ID → calls
	// fail returns an error for a task, or nil.
	fail func(task agent.Task, call int) error
	// factStatus returns the fact-check status for a round.
	factStatus func(round int) string
	// badCitation makes the first editorial draft cite a missing finding.
	badCitation bool
	// cliches makes the first editorial draft use generic phrasing.
	cliches bool
	// queries records SearchQueries per task ID.
	queries map[string][]string
}

func newFake() *fakeAgent { return &fakeAgent{calls: map[string]int{}, queries: map[string][]string{}} }

func (f *fakeAgent) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func (f *fakeAgent) roleCalls(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for id, c := range f.calls {
		if strings.HasPrefix(id, prefix) {
			n += c
		}
	}
	return n
}

func (f *fakeAgent) Run(ctx context.Context, t agent.Task) (agent.Result, error) {
	f.mu.Lock()
	f.calls[t.ID]++
	call := f.calls[t.ID]
	f.queries[t.ID] = t.SearchQueries
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return agent.Result{}, err
	}
	if f.fail != nil {
		if err := f.fail(t, call); err != nil {
			return agent.Result{TaskID: t.ID, Usage: agent.Usage{CostUSD: 0.01}}, err
		}
	}
	if !strings.Contains(t.Instructions, "Today's date is 2026-09-23") {
		return agent.Result{}, fmt.Errorf("system prompt not rendered: %.80q", t.Instructions)
	}
	if len(t.Schema) == 0 {
		return agent.Result{}, errors.New("task has no schema")
	}
	var out any
	switch {
	case t.Role == agent.RolePlanner:
		out = map[string]any{
			"objective": "Assess how Kubernetes is used for AI infrastructure.", "title": "Kubernetes for AI Infrastructure",
			"subtitle": "Adoption, architecture and operating models", "report_type": "Research Report",
			"research_questions": []string{"How widely is it used?", "What architectures are common?"},
			"workstreams": []map[string]any{
				{"id": "primary", "role": "primary", "objective": "Official sources", "questions": []string{"q"}},
				{"id": "market", "role": "market", "objective": "Adoption data", "questions": []string{"q"}},
				{"id": "tech", "role": "technical", "objective": "Architectures", "questions": []string{"q"}},
				{"id": "bogus", "role": "astrology", "objective": "x", "questions": []string{"q"}},
			},
			"report_outline": []string{"Context", "Adoption", "Architecture"},
		}
	case agent.IsResearchRole(t.Role):
		out = researchOutput(t.ID)
	case t.Role == agent.RoleFactChecker:
		round := 1
		fmt.Sscanf(t.ID, "factcheck-%d", &round)
		status := "pass"
		if f.factStatus != nil {
			status = f.factStatus(round)
		}
		fc := map[string]any{"status": status, "summary": "ok", "issues": []any{}, "recommended_research": []any{},
			"verdicts": []map[string]any{{"finding_id": "F002", "verdict": "unsupported", "note": "source does not say this"}}}
		if status == "needs_revision" {
			fc["issues"] = []map[string]any{{"severity": "major", "type": "gap", "description": "Adoption data is thin", "finding_ids": []string{"F004"}}}
			fc["recommended_research"] = []map[string]any{{"role": "market", "objective": "Find survey data", "questions": []string{"Survey?"}}}
		}
		out = fc
	case t.Role == agent.RoleSynthesizer:
		out = map[string]any{"thesis": "It is widely used.", "title": "Kubernetes for AI", "insights": []any{},
			"outline": []map[string]any{{"title": "Adoption", "purpose": "p", "finding_ids": []string{"F001"}}}, "gaps": []string{"No cost data"}}
	case t.Role == agent.RoleEditorial:
		ids := corpusIDs(t)
		if len(ids) < 3 {
			return agent.Result{}, fmt.Errorf("editorial got %d findings", len(ids))
		}
		cite := ids[0]
		if f.badCitation && !strings.Contains(t.ID, "rev") {
			cite = "F999"
		}
		for _, id := range ids {
			if id == "F002" && f.roleCalls("factcheck") > 0 {
				return agent.Result{}, errors.New("rejected finding F002 leaked into editorial corpus")
			}
		}
		doc := document(cite, ids)
		if f.cliches && !strings.Contains(t.ID, "rev") {
			secs := doc["sections"].([]map[string]any)
			blocks := secs[0]["blocks"].([]map[string]any)
			blocks[0]["text"] = fmt.Sprintf("In today's rapidly evolving landscape, organisations delve into training at 41 percent [%s].", ids[0])
			doc["conclusion"] = []string{fmt.Sprintf("It is important to note that adoption will seamlessly continue [%s].", ids[2])}
			doc["key_findings"].([]map[string]any)[0]["detail"] = fmt.Sprintf("Cutting-edge metric one reached 41 percent [%s].", ids[0])
		}
		out = doc
	case t.Role == agent.RoleFormatter:
		var ps []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(t.Inputs[0].Content), &ps)
		for i := range ps {
			txt := ps[i].Text
			for _, c := range []string{"In today's rapidly evolving landscape, organisations delve into", "It is important to note that ", " seamlessly"} {
				txt = strings.ReplaceAll(txt, c, map[bool]string{true: "Organisations run", false: ""}[strings.HasPrefix(c, "In")])
			}
			if strings.HasPrefix(ps[i].ID, "finding-") {
				txt = strings.Replace(txt, "Cutting-edge metric", "Metric", 1) + " An invented 99 percent." // must be rejected: new number
			}
			ps[i].Text = txt
		}
		out = map[string]any{"passages": ps}
	default:
		return agent.Result{}, fmt.Errorf("unexpected role %q", t.Role)
	}
	b, _ := json.Marshal(out)
	return agent.Result{TaskID: t.ID, Status: agent.StatusOK, Output: b, Raw: b, Usage: agent.Usage{CostUSD: 0.05, Turns: 1}}, nil
}

// researchOutput returns three findings with distinct sources per task.
func researchOutput(taskID string) map[string]any {
	var sources, findings []map[string]any
	for i := 1; i <= 3; i++ {
		ref := fmt.Sprintf("s%d", i)
		sources = append(sources, map[string]any{
			"ref": ref, "url": fmt.Sprintf("https://example.org/%s/%d", taskID, i), "title": fmt.Sprintf("%s source %d", taskID, i),
			"publisher": "Example", "published_at": "2026-05-01", "tier": 1 + i%2,
		})
		findings = append(findings, map[string]any{
			"claim":    fmt.Sprintf("%s observed metric %d at %d percent in 2026", taskID, i, 40+i),
			"evidence": fmt.Sprintf("The report states %d%% for metric %d.", 40+i, i), "source_refs": []string{ref},
			"confidence": 0.8, "as_of": "2026-04", "kind": "reported",
		})
	}
	return map[string]any{"summary": "s", "findings": findings, "sources": sources}
}

func corpusIDs(t agent.Task) []string {
	for _, in := range t.Inputs {
		if in.Name != "research corpus" {
			continue
		}
		var c struct {
			Findings []struct{ ID string } `json:"findings"`
		}
		_ = json.Unmarshal([]byte(in.Content), &c)
		var ids []string
		for _, f := range c.Findings {
			ids = append(ids, f.ID)
		}
		return ids
	}
	return nil
}

func document(cite string, ids []string) map[string]any {
	para := func(s string) map[string]any { return map[string]any{"type": "paragraph", "text": s} }
	exec := []string{}
	for i := 0; i < 3; i++ {
		exec = append(exec, fmt.Sprintf("Kubernetes has become a common substrate for AI workloads across organisations of different sizes, and survey data point in the same direction for training and inference alike in paragraph %d [%s].", i, cite))
	}
	return map[string]any{
		"title": "Kubernetes for AI Infrastructure", "subtitle": "Adoption and architecture", "report_type": "Research Report",
		"executive_summary": exec,
		"key_findings": []map[string]any{
			{"headline": "Adoption is broad", "detail": fmt.Sprintf("Metric one reached 41 percent [%s].", ids[0])},
			{"headline": "Architectures converge", "detail": fmt.Sprintf("Common patterns recur [%s].", ids[1])},
			{"headline": "Data remains thin", "detail": fmt.Sprintf("Few sources publish costs [%s].", ids[2])},
		},
		"sections": []map[string]any{
			{"title": "Adoption", "blocks": []map[string]any{
				para(fmt.Sprintf("Organisations report adoption for training workloads at 41 percent [%s].", ids[0])),
				{"type": "chart", "finding_ids": []string{ids[0], ids[1]}, "as_of": "2026-04", "chart": map[string]any{
					"kind": "column", "title": "Metrics", "units": "percent", "labels": []string{"one", "two"},
					"series": []map[string]any{{"name": "share", "values": []float64{41, 43}}}}},
				{"type": "chart", "finding_ids": []string{ids[0]}, "as_of": "2026", "chart": map[string]any{
					"kind": "bar", "title": "Invented", "units": "percent", "labels": []string{"x"},
					"series": []map[string]any{{"name": "share", "values": []float64{77}}}}},
			}},
			{"title": "Architecture", "blocks": []map[string]any{
				para(fmt.Sprintf("Platform teams converge on shared GPU pools [%s].", ids[1])),
				{"type": "table", "title": "Patterns", "columns": []string{"Pattern", "Share"}, "rows": [][]string{{"Pools", "42% [" + ids[1] + "]"}, {"Short row"}}, "finding_ids": []string{ids[1]}},
			}},
		},
		"conclusion": []string{fmt.Sprintf("Adoption will likely continue [%s].", ids[2])},
	}
}

// fakePrinter renders the PDF with Chrome when available; otherwise tests use html/md.
type fakePrinter struct{}

func (fakePrinter) Print(ctx context.Context, html, pdf string) error {
	return (&render.PDFPrinter{}).Print(ctx, html, pdf)
}

func testConfig(t *testing.T) config.Config {
	cfg := config.Default()
	dir := t.TempDir()
	cfg.Output.Directory = filepath.Join(dir, "reports")
	cfg.Output.RunsDir = filepath.Join(dir, "runs")
	cfg.Research.Depth = config.DepthQuick
	cfg.Report.Formats = []string{"html", "md"}
	cfg.Agents.MaxRetries = 1
	return cfg
}

func newOrchestrator(t *testing.T, cfg config.Config, a agent.Agent) *Orchestrator {
	lib, err := prompts.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return &Orchestrator{
		Config: cfg, Agent: a, Prompts: lib, Log: logx.Discard(), Printer: fakePrinter{}, Version: "test",
		Backoff: func(int) time.Duration { return 0 },
	}
}

var testNow = time.Date(2026, 9, 23, 7, 45, 0, 0, time.UTC)

func newRun(t *testing.T, cfg config.Config) *run.Run {
	r, err := run.Create(cfg.Output.RunsDir, "How is Kubernetes used for AI infrastructure?", cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPipelineEndToEnd(t *testing.T) {
	cfg := testConfig(t)
	if _, err := render.FindChrome(""); err == nil && !testing.Short() {
		cfg.Report.Formats = []string{"pdf", "html", "md"}
	}
	fake := newFake()
	r := newRun(t, cfg)
	out, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if len(out.Outputs) != len(cfg.Report.Formats)+1 || !strings.HasSuffix(out.Outputs[len(out.Outputs)-1], ".references.json") {
		t.Fatalf("outputs = %v", out.Outputs)
	}
	for _, p := range out.Outputs {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Errorf("output %s missing", p)
		}
	}
	m := r.Manifest()
	if m.Status != run.StatusCompleted {
		t.Errorf("status = %s", m.Status)
	}
	for _, s := range m.Stages {
		if s.Status != run.StatusDone {
			t.Errorf("stage %s = %s", s.Name, s.Status)
		}
	}
	if fake.count("research-bogus") != 0 {
		t.Error("workstream with unknown role was run")
	}
	if m.CostUSD <= 0 {
		t.Error("cost not tracked")
	}
	for _, rel := range []string{"plan.json", "plan.md", "research/primary.json", "evidence/sources.json", "evidence/findings.json",
		"factcheck/round-1.json", "synthesis/synthesis.json", "synthesis/synthesis.md", "report/document.json",
		"report/report.json", "report/gates.json", "report/report.html", "report/references.json", "agents/planner.raw.json", "logs/boku.log"} {
		if !r.Exists(rel) {
			t.Errorf("artifact %s missing", rel)
		}
	}
	html, _ := os.ReadFile(r.Path("report", "report.html"))
	if strings.Contains(string(html), "Invented") {
		t.Error("chart with untraceable values was rendered")
	}
	if !strings.Contains(string(html), `<svg class="chart"`) {
		t.Error("traceable chart missing")
	}
	if !strings.Contains(string(html), "No cost data") {
		t.Error("synthesis gaps missing from limitations")
	}
	if strings.Contains(string(html), `id="sources"`) || !strings.Contains(string(html), ".references.json") {
		t.Error("source list must stay out of the report unless include_references is set")
	}
	if q := fake.queries["research-primary"]; len(q) == 0 || !strings.HasPrefix(q[0], "q ") || !strings.Contains(q[0], "Kubernetes") {
		t.Errorf("research task missing search queries: %v", q)
	}
}

func TestSaveRefIncludesSources(t *testing.T) {
	cfg := testConfig(t)
	cfg.Report.IncludeReferences = true
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, newFake()).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	html, _ := os.ReadFile(r.Path("report", "report.html"))
	if !strings.Contains(string(html), `id="sources"`) || !strings.Contains(string(html), "Evidence register") {
		t.Error("--save-ref should put sources and evidence register in the report")
	}
}

func TestQuickModeSkipsFactCheck(t *testing.T) {
	cfg := testConfig(t)
	cfg.ApplyMode(config.ModeQuick)
	fake := newFake()
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err != nil {
		t.Fatalf("quick run failed: %v", err)
	}
	if fake.roleCalls("factcheck") != 0 || r.Exists("factcheck/round-1.json") {
		t.Error("fact checker ran in quick mode")
	}
	var rep struct {
		Layout string `json:"layout"`
	}
	_, _ = r.ReadJSON("report/report.json", &rep)
	if rep.Layout != "compact" {
		t.Errorf("layout = %q", rep.Layout)
	}
	html, _ := os.ReadFile(r.Path("report", "report.html"))
	if !strings.Contains(string(html), "have not been independently verified") {
		t.Error("methodology should say claims were not fact-checked")
	}
}

func TestLocalFormatterFixesStyleWithoutRevision(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.cliches = true
	r := newRun(t, cfg)
	o := newOrchestrator(t, cfg, fake)
	o.Formatter = fake
	if _, err := o.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if fake.roleCalls("formatter") != 1 || fake.roleCalls("editorial-rev") != 0 {
		t.Errorf("formatter calls %d, editorial revisions %d", fake.roleCalls("formatter"), fake.roleCalls("editorial-rev"))
	}
	if !r.Exists("report/document-polished.json") || latestDocument(&state{run: r}) != "report/document-polished.json" {
		t.Fatal("polished draft not used")
	}
	html, _ := os.ReadFile(r.Path("report", "report.html"))
	if strings.Contains(string(html), "delve") || strings.Contains(string(html), "seamlessly") {
		t.Error("generic phrasing survived the formatter")
	}
	if strings.Contains(string(html), "99 percent") {
		t.Error("formatter rewrite that added a number was accepted")
	}
}

func TestResumeSkipsCompletedWork(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.fail = func(task agent.Task, call int) error {
		if task.Role == agent.RoleSynthesizer {
			return &agent.Error{Kind: agent.ErrUnavailable, Msg: "claude crashed"}
		}
		return nil
	}
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err == nil {
		t.Fatal("expected synthesis failure")
	}
	if r.Manifest().Status != run.StatusFailed {
		t.Errorf("status = %s", r.Manifest().Status)
	}
	research := fake.roleCalls("research-")
	fake.fail = nil

	reopened, err := run.Open(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), reopened); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if fake.count("planner") != 1 || fake.roleCalls("research-") != research || fake.count("factcheck-1") != 1 {
		t.Errorf("resume repeated finished work: planner=%d research=%d→%d factcheck=%d",
			fake.count("planner"), research, fake.roleCalls("research-"), fake.count("factcheck-1"))
	}
	if fake.count("synthesis") != 2 {
		t.Errorf("synthesis calls = %d, want 2", fake.count("synthesis"))
	}
	// The rejected finding must stay rejected after reloading the evidence store.
	var findings []struct {
		ID     string
		Review struct{ Status string }
	}
	_, _ = reopened.ReadJSON("evidence/findings.json", &findings)
	for _, f := range findings {
		if f.ID == "F002" && f.Review.Status != "rejected" {
			t.Errorf("F002 review = %s after resume", f.Review.Status)
		}
	}
}

func TestPartialResearchFailureContinues(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.fail = func(task agent.Task, call int) error {
		if task.ID == "research-tech" {
			return &agent.Error{Kind: agent.ErrMalformed, Msg: "bad json", Retryable: true}
		}
		return nil
	}
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err != nil {
		t.Fatalf("pipeline should survive one failed workstream: %v", err)
	}
	if got := fake.count("research-tech"); got != 2 {
		t.Errorf("research-tech attempts = %d, want 2 (1 retry)", got)
	}
	if task := r.Manifest().Tasks["research-tech"]; task == nil || task.Status != agent.StatusFailed {
		t.Errorf("failed task not recorded: %+v", task)
	}
	var rep struct {
		Appendices []struct{ Blocks []struct{ Items []string } }
	}
	_, _ = r.ReadJSON("report/report.json", &rep)
	if !strings.Contains(fmt.Sprint(rep.Appendices), "tech research workstream failed") {
		t.Error("failed workstream not disclosed in limitations")
	}
}

func TestAllResearchFailingStopsRun(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.fail = func(task agent.Task, call int) error {
		if agent.IsResearchRole(task.Role) {
			return errors.New("network down")
		}
		return nil
	}
	_, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), newRun(t, cfg))
	if err == nil || !strings.Contains(err.Error(), "all 3 research workstreams failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestFactCheckFollowUpRounds(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.factStatus = func(round int) string {
		if round == 1 {
			return "needs_revision"
		}
		return "pass"
	}
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if fake.count("followup-r1-01") != 1 || fake.count("factcheck-2") != 1 {
		t.Errorf("follow-up=%d factcheck-2=%d", fake.count("followup-r1-01"), fake.count("factcheck-2"))
	}
	if !r.Exists("research/followup-r1-01.json") {
		t.Error("follow-up artifact missing")
	}
}

func TestFactCheckFailBlocksPublication(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.factStatus = func(int) string { return "fail" }
	r := newRun(t, cfg)
	_, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r)
	var blocked *ErrBlocked
	if !errors.As(err, &blocked) || blocked.Gate != "fact-check" {
		t.Fatalf("want fact-check block, got %v", err)
	}
	if fake.count("synthesis") != 0 {
		t.Error("synthesis ran after a failed fact gate")
	}
	if r.Manifest().Status != run.StatusBlocked {
		t.Errorf("status = %s", r.Manifest().Status)
	}
	entries, _ := os.ReadDir(cfg.Output.Directory)
	if len(entries) != 0 {
		t.Error("blocked run published output")
	}
}

func TestResearchGateBlocksThinEvidence(t *testing.T) {
	cfg := testConfig(t)
	cfg.Research.MinSources = 40
	_, err := newOrchestrator(t, cfg, newFake()).Execute(context.Background(), newRun(t, cfg))
	var blocked *ErrBlocked
	if !errors.As(err, &blocked) || blocked.Gate != "research" {
		t.Fatalf("want research block, got %v", err)
	}
}

func TestEditorialRevisionFixesBadCitation(t *testing.T) {
	cfg := testConfig(t)
	fake := newFake()
	fake.badCitation = true
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if fake.count("editorial-rev1") != 1 {
		t.Errorf("revision calls = %d", fake.count("editorial-rev1"))
	}
	if !r.Exists("report/document-rev1.json") {
		t.Error("revision artifact missing")
	}
}

func TestEditorialGateBlocksWhenRevisionsDoNotHelp(t *testing.T) {
	cfg := testConfig(t)
	cfg.Research.MaxIterations = 0
	fake := newFake()
	fake.fail = func(task agent.Task, call int) error {
		if task.ID == "editorial-rev1" {
			return &agent.Error{Kind: agent.ErrUnavailable, Msg: "down"}
		}
		return nil
	}
	fake.badCitation = true
	r := newRun(t, cfg)
	_, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r)
	var blocked *ErrBlocked
	if !errors.As(err, &blocked) || blocked.Gate != "editorial" {
		t.Fatalf("want editorial block, got %v", err)
	}
	if !r.Exists("report/draft.html") {
		t.Error("draft not kept for inspection")
	}
}

func TestBudgetStopsAgents(t *testing.T) {
	cfg := testConfig(t)
	cfg.Agents.MaxCostUSD = 0.12 // planner + ~1 researcher
	cfg.Agents.MaxParallel = 1
	_, err := newOrchestrator(t, cfg, newFake()).Execute(context.Background(), newRun(t, cfg))
	if err == nil {
		t.Fatal("expected the run to stop on budget")
	}
}

func TestCancellationThenResume(t *testing.T) {
	cfg := testConfig(t)
	cfg.Agents.MaxParallel = 1
	ctx, cancel := context.WithCancel(context.Background())
	fake := newFake()
	fake.fail = func(task agent.Task, call int) error {
		if task.ID == "research-market" {
			cancel()
			return context.Canceled
		}
		return nil
	}
	r := newRun(t, cfg)
	_, err := newOrchestrator(t, cfg, fake).Execute(ctx, r)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	fake.fail = nil
	if _, err := newOrchestrator(t, cfg, fake).Execute(context.Background(), r); err != nil {
		t.Fatalf("resume after cancel: %v", err)
	}
	if fake.count("research-primary") != 1 {
		t.Errorf("completed research re-run after cancel: %d", fake.count("research-primary"))
	}
}

func TestRenderFromArtifacts(t *testing.T) {
	cfg := testConfig(t)
	r := newRun(t, cfg)
	if _, err := newOrchestrator(t, cfg, newFake()).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	noAgent := agent.Func(func(context.Context, agent.Task) (agent.Result, error) {
		t.Fatal("render must not call agents")
		return agent.Result{}, nil
	})
	out, err := newOrchestrator(t, cfg, noAgent).Render(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Outputs) != 3 {
		t.Errorf("outputs = %v", out.Outputs)
	}
}

func TestSearchQueriesKeepTheSubject(t *testing.T) {
	qs := searchQueries(Workstream{Objective: "Compare MIG and time-slicing on Kubernetes", Questions: []string{
		"What are the trade-offs of each approach?", "How does MIG isolation work on Kubernetes GPU nodes?",
	}}, "GPU Sharing on Kubernetes")
	if len(qs) != 3 || !strings.HasSuffix(qs[0], "GPU Sharing on Kubernetes") || strings.Contains(qs[1], "GPU Sharing on Kubernetes") {
		t.Errorf("queries = %q", qs)
	}
}

func TestPlanNormalize(t *testing.T) {
	p := Plan{Objective: "o", Workstreams: []Workstream{
		{ID: "Market Data", Role: "market"}, {ID: "market-data", Role: "market"}, {ID: "x", Role: "nope"},
		{ID: "a", Role: "primary"}, {ID: "b", Role: "technical"},
	}}
	notes, err := p.normalize(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Workstreams) != 3 || p.Workstreams[0].ID != "market-data" || p.Workstreams[1].ID != "market-data-2" {
		t.Errorf("workstreams = %+v", p.Workstreams)
	}
	if len(notes) != 2 {
		t.Errorf("notes = %v", notes)
	}
	empty := Plan{Objective: "o", Workstreams: []Workstream{{ID: "x", Role: "nope"}}}
	if _, err := empty.normalize(3); err == nil {
		t.Error("plan without valid workstreams accepted")
	}
}
