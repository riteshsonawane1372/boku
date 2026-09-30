// Package run manages run directories: one directory per research execution
// holding the manifest and every intermediate artifact, so runs can be
// inspected, reproduced and resumed.
//
//	runs/2026-09-23T074500-ai-agents/
//	  manifest.json   run metadata, configuration, stage status, cost
//	  prompt.md       the research request
//	  plan.json       planner output
//	  research/       one JSON artifact per research task
//	  evidence/       de-duplicated sources and findings
//	  factcheck/      one artifact per fact-check round
//	  synthesis/      synthesis output
//	  report/         editorial document, built report, gates, HTML/Markdown
//	  output/         final published files
//	  agents/         raw agent responses (debugging)
//	  logs/           run log
package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/riteshsonawane1372/boku/internal/config"
)

// Stage names in execution order.
const (
	StagePlan      = "plan"
	StageResearch  = "research"
	StageFactCheck = "factcheck"
	StageSynthesis = "synthesis"
	StageEditorial = "editorial"
	StageReport    = "report"
	StageRender    = "render"
)

// Stages lists every stage in order.
var Stages = []string{StagePlan, StageResearch, StageFactCheck, StageSynthesis, StageEditorial, StageReport, StageRender}

// Status values for stages and runs.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked" // a quality gate refused publication
	StatusCompleted = "completed"
)

type Manifest struct {
	ID          string            `json:"id"`
	Topic       string            `json:"topic"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	BokuVersion string            `json:"boku_version"`
	Provider    string            `json:"provider"`
	Model       string            `json:"model,omitempty"`
	Prompts     map[string]string `json:"prompt_versions"`
	Config      config.Config     `json:"config"`
	Status      string            `json:"status"`
	Stages      []*Stage          `json:"stages"`
	Tasks       map[string]*Task  `json:"tasks"`
	CostUSD     float64           `json:"cost_usd"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type Stage struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Detail     string     `json:"detail,omitempty"`
}

// Task records one agent invocation.
type Task struct {
	ID       string    `json:"id"`
	Role     string    `json:"role"`
	Stage    string    `json:"stage"`
	Status   string    `json:"status"`
	Attempts int       `json:"attempts"`
	CostUSD  float64   `json:"cost_usd"`
	Duration string    `json:"duration"`
	Artifact string    `json:"artifact,omitempty"`
	Error    string    `json:"error,omitempty"`
	Finished time.Time `json:"finished"`
}

// Run is an open run directory. Methods are safe for concurrent use.
type Run struct {
	Dir string

	mu sync.Mutex
	m  Manifest
}

// Create makes a new run directory under runsDir.
func Create(runsDir, topic string, cfg config.Config, now time.Time) (*Run, error) {
	id := now.UTC().Format("2006-01-02T150405") + "-" + Slug(topic, 40)
	dir := filepath.Join(runsDir, id)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("run directory %s already exists", dir)
	}
	for _, sub := range []string{"research", "evidence", "factcheck", "synthesis", "report", "output", "agents", "logs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	r := &Run{Dir: dir, m: Manifest{
		ID: id, Topic: topic, CreatedAt: now, UpdatedAt: now, Config: cfg,
		Provider: cfg.Agents.Provider, Model: cfg.Agents.Model,
		Status: StatusRunning, Tasks: map[string]*Task{}, Prompts: map[string]string{},
	}}
	for _, s := range Stages {
		r.m.Stages = append(r.m.Stages, &Stage{Name: s, Status: StatusPending})
	}
	if err := r.WriteFile("prompt.md", []byte(fmt.Sprintf("# Research request\n\n%s\n\nRequested %s\n", topic, now.Format(time.RFC1123)))); err != nil {
		return nil, err
	}
	return r, r.Save()
}

// Open loads an existing run directory.
func Open(dir string) (*Run, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s is not a Boku run directory (no manifest.json)", dir)
		}
		return nil, err
	}
	r := &Run{Dir: dir}
	// Settings added after a run was created keep their defaults.
	r.m.Config = config.Default()
	if err := json.Unmarshal(b, &r.m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if r.m.Tasks == nil {
		r.m.Tasks = map[string]*Task{}
	}
	if r.m.Prompts == nil {
		r.m.Prompts = map[string]string{}
	}
	return r, nil
}

// Manifest returns a copy of the manifest.
func (r *Run) Manifest() Manifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.m
	return m
}

// Update mutates the manifest under lock and saves it.
func (r *Run) Update(fn func(m *Manifest)) error {
	r.mu.Lock()
	fn(&r.m)
	r.mu.Unlock()
	return r.Save()
}

// Save writes the manifest atomically.
func (r *Run) Save() error {
	r.mu.Lock()
	r.m.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(r.m, "", "  ")
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return r.WriteFile("manifest.json", append(b, '\n'))
}

func (r *Run) stage(name string) *Stage {
	for _, s := range r.m.Stages {
		if s.Name == name {
			return s
		}
	}
	s := &Stage{Name: name, Status: StatusPending}
	r.m.Stages = append(r.m.Stages, s)
	return s
}

// StageStart marks a stage running.
func (r *Run) StageStart(name string) {
	now := time.Now()
	_ = r.Update(func(m *Manifest) {
		s := r.stage(name)
		s.Status, s.StartedAt, s.FinishedAt, s.Error = StatusRunning, &now, nil, ""
	})
}

// StageDone marks a stage done with an optional detail line.
func (r *Run) StageDone(name, detail string) {
	now := time.Now()
	_ = r.Update(func(m *Manifest) {
		s := r.stage(name)
		s.Status, s.FinishedAt, s.Detail = StatusDone, &now, detail
	})
}

// StageFail marks a stage failed (or blocked by a gate).
func (r *Run) StageFail(name, status string, err error) {
	now := time.Now()
	_ = r.Update(func(m *Manifest) {
		s := r.stage(name)
		s.Status, s.FinishedAt = status, &now
		if err != nil {
			s.Error = err.Error()
		}
	})
}

// StageStatus returns a stage's status.
func (r *Run) StageStatus(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stage(name).Status
}

// RecordTask stores the outcome of an agent task and adds its cost.
func (r *Run) RecordTask(t Task) {
	_ = r.Update(func(m *Manifest) {
		if prev, ok := m.Tasks[t.ID]; ok {
			t.Attempts += prev.Attempts
			t.CostUSD += prev.CostUSD
			m.CostUSD -= prev.CostUSD
		}
		m.Tasks[t.ID] = &t
		m.CostUSD += t.CostUSD
	})
}

// Path joins elements onto the run directory.
func (r *Run) Path(elem ...string) string {
	return filepath.Join(append([]string{r.Dir}, elem...)...)
}

// Exists reports whether a run-relative artifact exists.
func (r *Run) Exists(rel string) bool {
	_, err := os.Stat(r.Path(rel))
	return err == nil
}

// WriteFile writes a run-relative file atomically.
func (r *Run) WriteFile(rel string, data []byte) error {
	p := r.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// WriteJSON writes v as indented JSON.
func (r *Run) WriteJSON(rel string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return r.WriteFile(rel, append(b, '\n'))
}

// ReadJSON reads a run-relative JSON artifact into v. ok is false when the
// artifact does not exist.
func (r *Run) ReadJSON(rel string, v any) (ok bool, err error) {
	b, err := os.ReadFile(r.Path(rel))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("%s: %w", rel, err)
	}
	return true, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "in": true, "for": true, "and": true, "to": true,
	"is": true, "are": true, "how": true, "what": true, "why": true, "with": true, "on": true, "by": true,
	"being": true, "its": true, "from": true, "at": true, "as": true, "does": true, "do": true, "which": true,
}

// Slug makes a short file-name-safe identifier from free text.
func Slug(s string, max int) string {
	words := strings.Fields(nonSlug.ReplaceAllString(strings.ToLower(s), " "))
	var kept []string
	for _, w := range words {
		if !stopWords[w] {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		kept = words
	}
	out := ""
	for _, w := range kept {
		next := strings.Trim(out+"-"+w, "-")
		if len(next) > max {
			break
		}
		out = next
	}
	if out == "" {
		out = "report"
	}
	return out
}
