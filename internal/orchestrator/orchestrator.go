// Package orchestrator runs Boku's research pipeline:
//
//	plan → research (parallel) → fact-check ⟲ follow-up research
//	     → research & fact gates → synthesis → editorial ⟲ revision
//	     → build report + editorial gate → render + PDF gate → publish
//
// Every step writes its artifact into the run directory before moving on and
// skips work whose artifact already exists, so an interrupted run resumes
// where it stopped.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/research"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/validation"
	"github.com/riteshsonawane1372/boku/prompts"
)

// Printer turns an HTML file into a PDF.
type Printer interface {
	Print(ctx context.Context, htmlPath, pdfPath string) error
}

// Orchestrator wires agents, prompts and the run directory together.
type Orchestrator struct {
	Config  config.Config
	Agent   agent.Agent
	Prompts *prompts.Library
	Log     *logx.Logger
	Printer Printer
	// Formatter runs style fixes on the local model; nil disables the pass.
	Formatter agent.Agent
	Version   string
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
	// Backoff overrides retry backoff (tests).
	Backoff func(int) time.Duration
}

// ErrBlocked is returned when a quality gate refuses publication.
type ErrBlocked struct {
	Gate   string
	Errors []string
}

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("%s gate failed; report not published:\n  - %s", e.Gate, strings.Join(e.Errors, "\n  - "))
}

// Outcome summarises a completed run.
type Outcome struct {
	RunDir  string
	Outputs []string
	CostUSD float64
	Gates   []validation.Gate
}

// state is the per-execution working set.
type state struct {
	run       *run.Run
	runner    *agent.Runner
	fmtRun    *agent.Runner // local formatter; nil when unavailable
	store     *research.Store
	ingested  map[string]bool
	asOf      time.Time // the run's reference date for freshness
	plan      *Plan
	lastFC    *validation.FactCheck
	fcRounds  int
	failed    []string // research tasks that failed permanently
	gates     []validation.Gate
	docPath   string         // final editorial draft
	autoCited bool           // quick mode added citations by text matching
	report    *report.Report // built report
	repo      string         // repository overview when explaining a codebase
}

const totalSteps = 7

// Execute runs (or resumes) the pipeline for r.
func (o *Orchestrator) Execute(ctx context.Context, r *run.Run) (*Outcome, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	m := r.Manifest()
	st := &state{run: r, asOf: m.CreatedAt, ingested: map[string]bool{}}
	st.runner = &agent.Runner{
		Agent:      o.Agent,
		MaxRetries: o.Config.Agents.MaxRetries,
		Budget:     agent.NewBudget(o.Config.Agents.MaxCostUSD, m.CostUSD),
		Backoff:    o.Backoff,
		OnRetry: func(t agent.Task, attempt int, err error) {
			o.Log.Warn("retrying agent", "task", t.ID, "attempt", attempt+1, "error", firstLine(err.Error()))
		},
	}
	if o.Formatter != nil {
		st.fmtRun = &agent.Runner{Agent: o.Formatter, MaxRetries: 1, Backoff: o.Backoff}
	}
	if f, err := os.OpenFile(r.Path("logs", "boku.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		defer f.Close()
		o.Log.AttachFile(f)
		defer o.Log.AttachFile(nil)
	}
	_ = r.Update(func(m *run.Manifest) {
		m.Status, m.Error, m.BokuVersion = run.StatusRunning, "", o.Version
		for _, role := range agent.AllRoles {
			m.Prompts[role] = o.Prompts.Version(role)
		}
	})
	if err := o.loadEvidence(st); err != nil {
		return nil, err
	}
	if o.Config.Research.Codebase != "" {
		// Built before research fans out, so parallel tasks only read it.
		st.repo = repoOverview(ctx, o.Config.Research.Codebase)
	}
	o.Log.Info("run started", "id", m.ID, "dir", r.Dir)

	out, err := o.pipeline(ctx, st)
	var blocked *ErrBlocked
	switch {
	case err == nil:
		_ = r.Update(func(m *run.Manifest) { m.Status = run.StatusCompleted })
	case errors.As(err, &blocked):
		_ = r.Update(func(m *run.Manifest) { m.Status, m.Error = run.StatusBlocked, err.Error() })
	default:
		_ = r.Update(func(m *run.Manifest) { m.Status, m.Error = run.StatusFailed, err.Error() })
	}
	if out != nil {
		out.CostUSD = r.Manifest().CostUSD
	}
	return out, err
}

func (o *Orchestrator) pipeline(ctx context.Context, st *state) (*Outcome, error) {
	steps := []struct {
		stage, label string
		fn           func(context.Context, *state) (string, error)
	}{
		{run.StagePlan, "Planning research", o.stagePlan},
		{run.StageResearch, "Researching", o.stageResearch},
		{run.StageFactCheck, "Cross-checking evidence", o.stageFactCheck},
		{run.StageSynthesis, "Synthesizing findings", o.stageSynthesis},
		{run.StageEditorial, "Writing and editing report", o.stageEditorial},
		{run.StageReport, "Running quality gates", o.stageReport},
	}
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		o.Log.Step(i+1, totalSteps, s.label)
		st.run.StageStart(s.stage)
		detail, err := s.fn(ctx, st)
		if err != nil {
			status := run.StatusFailed
			var blocked *ErrBlocked
			if errors.As(err, &blocked) {
				status = run.StatusBlocked
			}
			st.run.StageFail(s.stage, status, err)
			return nil, err
		}
		st.run.StageDone(s.stage, detail)
	}
	o.Log.Step(totalSteps, totalSteps, "Rendering report")
	st.run.StageStart(run.StageRender)
	out, err := o.stageRender(ctx, st)
	if err != nil {
		status := run.StatusFailed
		var blocked *ErrBlocked
		if errors.As(err, &blocked) {
			status = run.StatusBlocked
		}
		st.run.StageFail(run.StageRender, status, err)
		return nil, err
	}
	st.run.StageDone(run.StageRender, strings.Join(out.Outputs, ", "))
	out.Gates = st.gates
	return out, nil
}

// taskSpec describes one agent call.
type taskSpec struct {
	ID          string
	Role        string
	Stage       string
	Schema      string
	Objective   string
	Context     string
	Constraints []string
	Inputs      []agent.Artifact
	Tools       []string
	// SearchQueries seed retrieval for providers without web tools;
	// SearchPages caps the pages fetched (0 = configured default).
	SearchQueries []string
	SearchPages   int
	Artifact      string // run-relative path the output is written to
}

var webTools = []string{agent.ToolWebSearch, agent.ToolWebFetch}

// runTask executes an agent task and writes its structured output to
// spec.Artifact. The raw provider response is kept under agents/.
func (o *Orchestrator) runTask(ctx context.Context, st *state, spec taskSpec) (json.RawMessage, error) {
	sys, err := o.Prompts.System(spec.Role, prompts.Vars{
		Today:         st.asOf.Format("2006-01-02"),
		FreshnessDays: o.Config.Research.FreshnessDays,
		Depth:         string(o.Config.Research.Depth),
		Mode:          string(o.Config.Report.Mode),
		Codebase:      o.Config.Research.Codebase != "",
	})
	if err != nil {
		return nil, err
	}
	schema, err := o.Prompts.Schema(spec.Schema)
	if err != nil {
		return nil, err
	}
	workDir := st.run.Path("agents", spec.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	if o.Config.Research.Codebase != "" && usesFiles(spec.Tools) {
		// File tools resolve paths against the repository being explained.
		workDir = o.Config.Research.Codebase
	}
	runner, model := st.runner, o.Config.ModelFor(spec.Role)
	if spec.Role == agent.RoleFormatter || o.Config.IsLocalRole(spec.Role) {
		model = "" // the local model is configured on its runtime
	}
	if spec.Role == agent.RoleFormatter {
		if st.fmtRun == nil {
			return nil, errors.New("no local formatter configured")
		}
		runner = st.fmtRun
	}
	task := agent.Task{
		ID: spec.ID, Role: spec.Role, Objective: spec.Objective, Context: spec.Context,
		Inputs: spec.Inputs, Constraints: spec.Constraints, Instructions: sys, Schema: schema,
		Tools: spec.Tools, SearchQueries: spec.SearchQueries, SearchPages: spec.SearchPages, Model: model,
		Timeout: time.Duration(o.Config.Agents.Timeout), WorkDir: workDir,
	}
	o.Log.Debug("agent started", "task", spec.ID, "role", spec.Role)
	start := o.Now()
	res, err := runner.Run(ctx, task)
	if len(res.Raw) > 0 {
		_ = st.run.WriteFile(filepath.Join("agents", spec.ID+".raw.json"), res.Raw)
	}
	rec := run.Task{
		ID: spec.ID, Role: spec.Role, Stage: spec.Stage, Status: agent.StatusOK,
		Attempts: res.Usage.Attempts, CostUSD: res.Usage.CostUSD,
		Duration: o.Now().Sub(start).Round(time.Second).String(), Artifact: spec.Artifact, Finished: o.Now(),
	}
	if err != nil {
		rec.Status, rec.Error = agent.StatusFailed, err.Error()
		st.run.RecordTask(rec)
		return nil, err
	}
	var pretty json.RawMessage = res.Output
	if b, err := json.MarshalIndent(res.Output, "", "  "); err == nil {
		pretty = b
	}
	if err := st.run.WriteFile(spec.Artifact, append(pretty, '\n')); err != nil {
		return nil, err
	}
	st.run.RecordTask(rec)
	o.Log.Debug("agent finished", "task", spec.ID, "cost", fmt.Sprintf("$%.2f", res.Usage.CostUSD))
	return res.Output, nil
}

// loadEvidence restores the evidence store and ingest log from the run.
func (o *Orchestrator) loadEvidence(st *state) error {
	store, err := research.LoadStore(st.run.Path("evidence"))
	if err != nil {
		return fmt.Errorf("load evidence: %w", err)
	}
	st.store = store
	var ingested []string
	if _, err := st.run.ReadJSON("evidence/ingested.json", &ingested); err != nil {
		return err
	}
	for _, name := range ingested {
		st.ingested[name] = true
	}
	return nil
}

// saveEvidence persists the store, the ingest log and inspectable Markdown.
func (o *Orchestrator) saveEvidence(st *state) error {
	if err := st.store.Save(st.run.Path("evidence")); err != nil {
		return err
	}
	var names []string
	for n := range st.ingested {
		names = append(names, n)
	}
	sort.Strings(names)
	if err := st.run.WriteJSON("evidence/ingested.json", names); err != nil {
		return err
	}
	return st.run.WriteFile("evidence/findings.md", []byte(st.store.Markdown("Evidence register", nil)))
}

// ingest adds a research artifact to the store once.
func (o *Orchestrator) ingest(st *state, rel, workstream string, out research.ResearchOutput) {
	if st.ingested[rel] {
		return
	}
	rep := st.store.Ingest(out, research.IngestOptions{
		TaskID: strings.TrimSuffix(filepath.Base(rel), ".json"), Workstream: workstream,
		Now: st.asOf, FreshnessDays: o.Config.Research.FreshnessDays,
	})
	st.ingested[rel] = true
	for _, w := range rep.Warnings {
		o.Log.Debug(w, "artifact", rel)
	}
	if rep.Unsourced > 0 {
		o.Log.Warn("unsourced claims rejected", "artifact", rel, "count", rep.Unsourced)
	}
	o.Log.Debug("ingested", "artifact", rel, "findings", rep.FindingsAdded, "merged", rep.FindingsMerged, "sources", rep.SourcesAdded)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}
