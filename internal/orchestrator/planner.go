package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/run"
)

// Plan is the planner's output (prompts/schemas/planner.json).
type Plan struct {
	Objective         string       `json:"objective"`
	Title             string       `json:"title"`
	Subtitle          string       `json:"subtitle"`
	ReportType        string       `json:"report_type"`
	Audience          string       `json:"audience"`
	TimeSensitivity   string       `json:"time_sensitivity"`
	ResearchQuestions []string     `json:"research_questions"`
	Workstreams       []Workstream `json:"workstreams"`
	RequiredSources   []string     `json:"required_sources"`
	Deliverables      []string     `json:"deliverables"`
	ReportOutline     []string     `json:"report_outline"`
}

type Workstream struct {
	ID              string   `json:"id"`
	Role            string   `json:"role"`
	Objective       string   `json:"objective"`
	Questions       []string `json:"questions"`
	PrioritySources []string `json:"priority_sources"`
}

// normalize enforces the orchestrator's rules on a plan: known roles only,
// unique IDs, and at most maxAgents workstreams. It returns what it changed.
func (p *Plan) normalize(maxAgents int) (notes []string, err error) {
	seen := map[string]bool{}
	var ws []Workstream
	for _, w := range p.Workstreams {
		if !agent.IsResearchRole(w.Role) {
			notes = append(notes, fmt.Sprintf("dropped workstream %q with unknown role %q", w.ID, w.Role))
			continue
		}
		id := run.Slug(w.ID, 32)
		if id == "report" || id == "" {
			id = w.Role
		}
		base := id
		for n := 2; seen[id]; n++ {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		seen[id] = true
		w.ID = id
		ws = append(ws, w)
	}
	if len(ws) > maxAgents {
		notes = append(notes, fmt.Sprintf("planner proposed %d workstreams; keeping the first %d (agents.max_agents)", len(ws), maxAgents))
		ws = ws[:maxAgents]
	}
	if len(ws) == 0 {
		return notes, errors.New("planner produced no usable workstreams")
	}
	p.Workstreams = ws
	if strings.TrimSpace(p.Objective) == "" {
		return notes, errors.New("planner produced no objective")
	}
	return notes, nil
}

func (o *Orchestrator) stagePlan(ctx context.Context, st *state) (string, error) {
	const rel = "plan.json"
	var plan Plan
	ok, err := st.run.ReadJSON(rel, &plan)
	if err != nil {
		return "", err
	}
	if ok {
		o.Log.Info("plan loaded from run directory")
	} else {
		topic := st.run.Manifest().Topic
		cons := []string{
			fmt.Sprintf("Create at most %d workstreams; fewer is better when the question is narrow.", o.Config.Agents.MaxAgents),
			fmt.Sprintf("Research depth is %s.", o.Config.Research.Depth),
		}
		if len(o.Config.Research.Sources) > 0 {
			cons = append(cons, "The requester suggests these sources or source types: "+strings.Join(o.Config.Research.Sources, "; ")+".")
		}
		raw, err := o.runTask(ctx, st, taskSpec{
			ID: "planner", Role: agent.RolePlanner, Stage: run.StagePlan, Schema: "planner",
			Objective:   "Create the research plan for this request:\n\n" + topic,
			Context:     fmt.Sprintf("Today is %s. Information within %d days counts as current.", st.asOf.Format("2 January 2006"), o.Config.Research.FreshnessDays),
			Constraints: cons, Tools: []string{agent.ToolWebSearch}, Artifact: rel,
		})
		if err != nil {
			return "", fmt.Errorf("planner: %w", err)
		}
		if err := json.Unmarshal(raw, &plan); err != nil {
			return "", fmt.Errorf("planner output: %w", err)
		}
	}
	notes, err := plan.normalize(o.Config.Agents.MaxAgents)
	for _, n := range notes {
		o.Log.Warn(n)
	}
	if err != nil {
		return "", err
	}
	if err := st.run.WriteJSON(rel, plan); err != nil {
		return "", err
	}
	_ = st.run.WriteFile("plan.md", []byte(planMarkdown(plan)))
	st.plan = &plan

	var roles []string
	for _, w := range plan.Workstreams {
		roles = append(roles, w.ID+" ("+w.Role+")")
	}
	o.Log.Info("planner completed", "title", plan.Title, "workstreams", len(plan.Workstreams))
	for _, w := range plan.Workstreams {
		o.Log.Info("  workstream", "id", w.ID, "role", w.Role)
	}
	return strings.Join(roles, ", "), nil
}

func planMarkdown(p Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n_%s_\n\n**Objective.** %s\n\n**Type:** %s · **Audience:** %s · **Time sensitivity:** %s\n\n## Research questions\n\n",
		p.Title, p.Subtitle, p.Objective, p.ReportType, p.Audience, p.TimeSensitivity)
	for _, q := range p.ResearchQuestions {
		fmt.Fprintf(&b, "- %s\n", q)
	}
	b.WriteString("\n## Workstreams\n\n")
	for _, w := range p.Workstreams {
		fmt.Fprintf(&b, "### %s (%s)\n\n%s\n\n", w.ID, w.Role, w.Objective)
		for _, q := range w.Questions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
		if len(w.PrioritySources) > 0 {
			fmt.Fprintf(&b, "\nPriority sources: %s\n", strings.Join(w.PrioritySources, "; "))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Proposed outline\n\n")
	for i, s := range p.ReportOutline {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	return b.String()
}
