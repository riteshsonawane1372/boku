// Package scope is the expert interview that runs before a report: an agent
// reads the request, asks the few questions whose answers would change the
// research, and rewrites the request into a precise brief the requester
// confirms. Nothing here starts a run; the confirmed brief becomes the run's
// topic and the interview is saved in the run directory as scope.json.
//
//	request ──▶ Ask ──▶ questions ──▶ answers ──▶ Refine ──▶ brief ──▶ run topic
package scope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/prompts"
)

// Question is one thing the expert needs to know, with suggested answers.
type Question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	// Why says what the answer changes.
	Why     string   `json:"why,omitempty"`
	Options []string `json:"options"`
	// Multi is true when several options can apply at once.
	Multi bool `json:"multi,omitempty"`
}

// Interview is the expert's reading of the request and its questions.
type Interview struct {
	Understanding string     `json:"understanding"`
	Questions     []Question `json:"questions"`
}

// Answer is the requester's reply to a question; an empty Answer means the
// question was skipped and the expert should choose a default.
type Answer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

// Brief is the request rewritten after the interview.
type Brief struct {
	// Topic is the refined research request; it becomes the run's topic.
	Topic       string   `json:"topic"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	InScope     []string `json:"in_scope"`
	OutOfScope  []string `json:"out_of_scope,omitempty"`
	Assumptions []string `json:"assumptions,omitempty"`
}

// Record is what a run keeps of its interview (scope.json).
type Record struct {
	OriginalTopic string     `json:"original_topic"`
	Understanding string     `json:"understanding,omitempty"`
	Questions     []Question `json:"questions"`
	Answers       []Answer   `json:"answers"`
	Brief         Brief      `json:"brief"`
	// Topic is what the requester confirmed: the brief's topic, or their
	// edit of it.
	Topic   string  `json:"topic"`
	CostUSD float64 `json:"cost_usd"`
}

// File is the interview's artifact in a run directory.
const File = "scope.json"

// Limits on what a request may carry; the interview is short by design.
const (
	MaxTopicChars = 4000
	maxQuestions  = 6
	maxOptions    = 6
)

// Expert runs the interview on an agent runtime.
type Expert struct {
	Agent   agent.Agent
	Prompts *prompts.Library
	Config  config.Config
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
}

func (e *Expert) run(ctx context.Context, id, schemaName, objective string, inputs []agent.Artifact, queries []string) (json.RawMessage, float64, error) {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	cfg := e.Config
	sys, err := e.Prompts.System(agent.RoleScoper, prompts.Vars{
		Today: now().Format("2006-01-02"), FreshnessDays: cfg.Research.FreshnessDays,
		Depth: string(cfg.Research.Depth), Mode: string(cfg.Report.Mode),
	})
	if err != nil {
		return nil, 0, err
	}
	schema, err := e.Prompts.Schema(schemaName)
	if err != nil {
		return nil, 0, err
	}
	dir, err := os.MkdirTemp("", "boku-scope-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	model := cfg.ModelFor(agent.RoleScoper)
	if cfg.IsLocalRole(agent.RoleScoper) {
		model = "" // the local model is configured on its runtime
	}
	var tools []string
	if len(queries) > 0 {
		tools = []string{agent.ToolWebSearch}
	}
	// An interview that takes longer than a few minutes is not an interview.
	timeout := min(time.Duration(cfg.Agents.Timeout), 5*time.Minute)
	runner := &agent.Runner{Agent: e.Agent, MaxRetries: min(cfg.Agents.MaxRetries, 1)}
	res, err := runner.Run(ctx, agent.Task{
		ID: id, Role: agent.RoleScoper, Objective: objective, Inputs: inputs, Instructions: sys, Schema: schema,
		Context:       fmt.Sprintf("Today is %s. The report mode is %s and the research depth is %s.", now().Format("2 January 2006"), cfg.Report.Mode, cfg.Research.Depth),
		Tools:         tools,
		SearchQueries: queries, SearchPages: 3, Model: model, Timeout: timeout, WorkDir: dir,
	})
	if err != nil {
		return nil, res.Usage.CostUSD, err
	}
	return res.Output, res.Usage.CostUSD, nil
}

// Ask returns the expert's questions about a request, and what asking cost.
func (e *Expert) Ask(ctx context.Context, topic string) (Interview, float64, error) {
	var out Interview
	topic, err := cleanTopic(topic)
	if err != nil {
		return out, 0, err
	}
	raw, cost, err := e.run(ctx, "scoper-questions", "scope-questions",
		"Step 1 — ask. Read this research request and write the questions you need answered before research starts:\n\n"+topic,
		nil, []string{topic})
	if err != nil {
		return out, cost, fmt.Errorf("scoping questions: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, cost, fmt.Errorf("scoping questions: %w", err)
	}
	out.Understanding = strings.TrimSpace(out.Understanding)
	out.Questions = cleanQuestions(out.Questions)
	if len(out.Questions) == 0 {
		return out, cost, errors.New("the expert had no questions: the request is already specific; run it as it is")
	}
	return out, cost, nil
}

// Refine rewrites the request into a brief from the answers given.
func (e *Expert) Refine(ctx context.Context, topic string, questions []Question, answers []Answer) (Brief, float64, error) {
	var out Brief
	topic, err := cleanTopic(topic)
	if err != nil {
		return out, 0, err
	}
	raw, cost, err := e.run(ctx, "scoper-brief", "scope-brief",
		"Step 2 — brief. Rewrite this research request into the brief the research team will work from, using the requester's answers:\n\n"+topic,
		[]agent.Artifact{{Name: "interview", Content: Transcript(cleanQuestions(questions), answers)}}, nil)
	if err != nil {
		return out, cost, fmt.Errorf("scoping brief: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, cost, fmt.Errorf("scoping brief: %w", err)
	}
	out.Topic, out.Title = strings.TrimSpace(out.Topic), strings.TrimSpace(out.Title)
	if out.Topic == "" {
		return out, cost, errors.New("scoping brief: the expert returned no topic")
	}
	if len([]rune(out.Topic)) > MaxTopicChars {
		out.Topic = string([]rune(out.Topic)[:MaxTopicChars])
	}
	return out, cost, nil
}

// Transcript renders the interview as text: each question with the answer
// given, or a note that it was skipped.
func Transcript(questions []Question, answers []Answer) string {
	given := map[string]string{}
	for _, a := range answers {
		if s := strings.TrimSpace(a.Answer); s != "" {
			given[a.ID] = s
		}
	}
	var b strings.Builder
	for _, q := range questions {
		fmt.Fprintf(&b, "Q (%s): %s\n", q.ID, q.Question)
		if len(q.Options) > 0 {
			fmt.Fprintf(&b, "   Suggested answers: %s\n", strings.Join(q.Options, " | "))
		}
		if a, ok := given[q.ID]; ok {
			fmt.Fprintf(&b, "A: %s\n\n", a)
		} else {
			b.WriteString("A: (skipped — choose a sensible default and list it under assumptions)\n\n")
		}
	}
	return b.String()
}

func cleanTopic(topic string) (string, error) {
	topic = strings.TrimSpace(topic)
	switch {
	case topic == "":
		return "", errors.New("a research topic is required")
	case len([]rune(topic)) > MaxTopicChars:
		return "", fmt.Errorf("the topic is longer than %d characters", MaxTopicChars)
	}
	return topic, nil
}

// cleanQuestions drops empty questions, gives every question a unique ID and
// keeps the interview short.
func cleanQuestions(in []Question) []Question {
	var out []Question
	seen := map[string]bool{}
	for _, q := range in {
		q.Question = strings.TrimSpace(q.Question)
		if q.Question == "" || len(out) == maxQuestions {
			continue
		}
		q.ID = strings.TrimSpace(q.ID)
		if q.ID == "" || seen[q.ID] {
			q.ID = fmt.Sprintf("q%d", len(out)+1)
		}
		seen[q.ID] = true
		var opts []string
		for _, o := range q.Options {
			if o = strings.TrimSpace(o); o != "" && len(opts) < maxOptions {
				opts = append(opts, o)
			}
		}
		q.Options, q.Why = opts, strings.TrimSpace(q.Why)
		out = append(out, q)
	}
	return out
}

// Markdown renders a record for the run directory.
func (r Record) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Scoping interview\n\n**Original request.** %s\n\n", r.OriginalTopic)
	if r.Understanding != "" {
		fmt.Fprintf(&b, "**Expert's reading.** %s\n\n", r.Understanding)
	}
	b.WriteString("## Questions and answers\n\n")
	given := map[string]string{}
	for _, a := range r.Answers {
		given[a.ID] = strings.TrimSpace(a.Answer)
	}
	for _, q := range r.Questions {
		a := given[q.ID]
		if a == "" {
			a = "_skipped_"
		}
		fmt.Fprintf(&b, "- **%s** %s\n", q.Question, a)
	}
	fmt.Fprintf(&b, "\n## Confirmed topic\n\n%s\n", r.Topic)
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
	}
	list("In scope", r.Brief.InScope)
	list("Out of scope", r.Brief.OutOfScope)
	list("Assumptions", r.Brief.Assumptions)
	return b.String()
}
