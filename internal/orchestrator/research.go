package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/research"
	"github.com/riteshsonawane1372/boku/internal/run"
)

// researchJob is one research task and where its output goes.
type researchJob struct {
	ID         string
	Workstream Workstream
	Artifact   string
	// Known lists claims already in the store, so the agent does not repeat them.
	Known string
	// Reason explains why a follow-up exists (fact-check issue).
	Reason string
}

func (o *Orchestrator) stageResearch(ctx context.Context, st *state) (string, error) {
	var jobs []researchJob
	for _, w := range st.plan.Workstreams {
		jobs = append(jobs, researchJob{ID: "research-" + w.ID, Workstream: w, Artifact: "research/" + w.ID + ".json"})
	}
	pending := 0
	for _, j := range jobs {
		if !st.run.Exists(j.Artifact) {
			pending++
		}
	}
	if pending < len(jobs) {
		o.Log.Info("resuming research", "completed", len(jobs)-pending, "remaining", pending)
	}
	if pending > 0 {
		o.Log.Info(fmt.Sprintf("spawning %d research agents", pending), "max_parallel", o.Config.Agents.MaxParallel)
	}
	failed := o.runResearch(ctx, st, jobs)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(failed) == len(jobs) {
		return "", fmt.Errorf("all %d research workstreams failed", len(jobs))
	}
	for _, j := range jobs {
		if err := o.ingestResearch(st, j); err != nil {
			o.Log.Warn("could not ingest research artifact", "artifact", j.Artifact, "error", err)
		}
	}
	if err := o.saveEvidence(st); err != nil {
		return "", err
	}
	stats := st.store.Stats()
	o.Log.Info("research completed", "findings", stats.Findings, "sources", stats.Sources)
	return fmt.Sprintf("%d findings from %d sources; %d workstream(s) failed", stats.Findings, stats.Sources, len(failed)), nil
}

// runResearch runs jobs whose artifacts are missing, at most max_parallel at a
// time. A failed job does not stop the others; failures are returned.
func (o *Orchestrator) runResearch(ctx context.Context, st *state, jobs []researchJob) (failed []string) {
	var mu sync.Mutex
	g := errgroup.Group{}
	g.SetLimit(o.Config.Agents.MaxParallel)
	for _, j := range jobs {
		if st.run.Exists(j.Artifact) {
			continue
		}
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			start := time.Now()
			_, err := o.runTask(ctx, st, o.researchSpec(st, j))
			if err != nil {
				if ctx.Err() == nil {
					o.Log.Warn("research failed", "workstream", j.Workstream.ID, "error", firstLine(err.Error()))
				}
				mu.Lock()
				failed = append(failed, j.Workstream.ID)
				st.failed = append(st.failed, j.Workstream.ID)
				mu.Unlock()
				return nil
			}
			o.Log.Info(fmt.Sprintf("%s research completed", j.Workstream.ID), "role", j.Workstream.Role, "took", time.Since(start).Round(time.Second))
			return nil
		})
	}
	_ = g.Wait()
	return failed
}

func (o *Orchestrator) ingestResearch(st *state, j researchJob) error {
	if st.ingested[j.Artifact] || !st.run.Exists(j.Artifact) {
		return nil
	}
	var out research.ResearchOutput
	if _, err := st.run.ReadJSON(j.Artifact, &out); err != nil {
		return err
	}
	o.ingest(st, j.Artifact, j.Workstream.ID, out)
	return nil
}

var depthTargets = map[config.Depth]string{
	config.DepthQuick:    "Aim for 6–10 strong findings drawn from at least 6 distinct sources. Be efficient: stop once the core questions are answered.",
	config.DepthStandard: "Aim for 12–20 strong findings drawn from at least 10 distinct sources of at least three types, covering every workstream question.",
	config.DepthDeep:     "Aim for 20–35 strong findings drawn from at least 18 distinct sources of at least four types. Pursue primary sources for every important figure, corroborate headline numbers, and cover secondary questions.",
}

func (o *Orchestrator) researchSpec(st *state, j researchJob) taskSpec {
	var ctxb strings.Builder
	p := st.plan
	fmt.Fprintf(&ctxb, "Report objective: %s\n\n", p.Objective)
	if len(p.ResearchQuestions) > 0 {
		ctxb.WriteString("Research questions for the whole report:\n")
		for _, q := range p.ResearchQuestions {
			fmt.Fprintf(&ctxb, "- %s\n", q)
		}
		ctxb.WriteString("\n")
	}
	var others []string
	for _, w := range p.Workstreams {
		if w.ID != j.Workstream.ID {
			others = append(others, fmt.Sprintf("- %s (%s): %s", w.ID, w.Role, w.Objective))
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&ctxb, "Other agents are covering these workstreams; do not duplicate them:\n%s\n\n", strings.Join(others, "\n"))
	}
	if j.Known != "" {
		fmt.Fprintf(&ctxb, "Already known (do not re-research; add only new or better-sourced evidence):\n%s\n", j.Known)
	}

	var obj strings.Builder
	fmt.Fprintf(&obj, "Workstream %q (%s): %s\n", j.Workstream.ID, j.Workstream.Role, j.Workstream.Objective)
	if j.Reason != "" {
		fmt.Fprintf(&obj, "\nThis is follow-up research requested by the fact checker: %s\n", j.Reason)
	}
	if len(j.Workstream.Questions) > 0 {
		obj.WriteString("\nQuestions:\n")
		for _, q := range j.Workstream.Questions {
			fmt.Fprintf(&obj, "- %s\n", q)
		}
	}
	if len(j.Workstream.PrioritySources) > 0 {
		fmt.Fprintf(&obj, "\nLikely primary sources: %s\n", strings.Join(j.Workstream.PrioritySources, "; "))
	}

	cutoff := st.asOf.AddDate(0, 0, -o.Config.Research.FreshnessDays)
	cons := []string{
		depthTargets[o.Config.Research.Depth],
		fmt.Sprintf("Information dated before %s is historical: include it only with its date, and look for newer data for anything time-sensitive.", cutoff.Format("2 January 2006")),
		"Cite only pages you opened. Every non-inference finding needs at least one source_ref.",
	}
	if o.Config.Research.Codebase != "" {
		cons[1] = "Code describes the repository as it is now; web material about external dependencies follows the usual freshness rules."
		cons[2] = "Cite only files and pages you opened. Every non-inference finding needs at least one source_ref."
		fmt.Fprintf(&ctxb, "\nRepository overview:\n%s\n", o.repoOverview(context.Background(), st))
	}
	if len(o.Config.Research.Sources) > 0 {
		cons = append(cons, "Requested sources or source types: "+strings.Join(o.Config.Research.Sources, "; ")+".")
	}
	return taskSpec{
		ID: j.ID, Role: j.Workstream.Role, Stage: run.StageResearch, Schema: "research",
		Objective: obj.String(), Context: ctxb.String(), Constraints: cons, Tools: o.tools(), Artifact: j.Artifact,
		SearchQueries: searchQueries(j.Workstream, p.Title),
	}
}

// searchQueries are the web searches Boku runs for providers without web
// tools: the workstream's questions, then its objective. A question that
// does not name the subject ("What are the trade-offs of each approach?")
// gets the report title appended, or the search drifts off topic.
func searchQueries(w Workstream, title string) []string {
	var qs []string
	add := func(q string) {
		if q = strings.TrimSpace(q); q != "" && len(qs) < 5 {
			if title != "" && sharedWords(q, title) < 2 {
				q += " " + title
			}
			qs = append(qs, q)
		}
	}
	for _, q := range w.Questions {
		add(q)
	}
	add(w.Objective)
	return qs
}

var stopWords = map[string]bool{
	"the": true, "and": true, "are": true, "how": true, "what": true, "for": true, "with": true, "does": true,
	"which": true, "why": true, "who": true, "its": true, "their": true, "from": true, "this": true, "that": true,
	"each": true, "has": true, "have": true, "was": true, "were": true, "can": true, "into": true, "between": true,
}

// sharedWords counts distinct significant words that appear in both strings.
func sharedWords(a, b string) int {
	words := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) {
			if len(w) >= 3 && !stopWords[w] {
				m[w] = true
			}
		}
		return m
	}
	wb, n := words(b), 0
	for w := range words(a) {
		if wb[w] {
			n++
		}
	}
	return n
}

// decode unmarshals an artifact written by runTask.
func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, errors.New("empty output")
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// roleOrDefault maps unknown follow-up roles to primary research.
func roleOrDefault(role string) string {
	if agent.IsResearchRole(role) {
		return role
	}
	return agent.RolePrimary
}
