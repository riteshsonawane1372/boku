package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/validation"
)

// stageFactCheck runs critic rounds. After each round needing revision, the
// critic's recommended research runs as targeted follow-up tasks, then the
// critic reviews again — up to research.max_iterations follow-up rounds.
// It ends with the research and fact-check gates.
func (o *Orchestrator) stageFactCheck(ctx context.Context, st *state) (string, error) {
	if !o.Config.Research.FactCheck {
		return o.skipFactCheck(st)
	}
	maxRounds := o.Config.Research.MaxIterations + 1
	for round := 1; round <= maxRounds; round++ {
		rel := fmt.Sprintf("factcheck/round-%d.json", round)
		if !st.run.Exists(rel) {
			if _, err := o.runTask(ctx, st, o.factCheckSpec(st, round, rel)); err != nil {
				return "", fmt.Errorf("fact check round %d: %w", round, err)
			}
		}
		var fc validation.FactCheck
		if _, err := st.run.ReadJSON(rel, &fc); err != nil {
			return "", err
		}
		if !st.ingested[rel] {
			if len(fc.Corrections.Findings) > 0 {
				o.ingest(st, rel+"#corrections", "fact-check", fc.Corrections)
			}
			changed, warns := validation.ApplyVerdicts(st.store, fc, round)
			for _, w := range warns {
				o.Log.Debug(w)
			}
			st.ingested[rel] = true
			if err := o.saveEvidence(st); err != nil {
				return "", err
			}
			o.Log.Info(fmt.Sprintf("fact checker round %d: %s", round, fc.Status),
				"verdicts", changed, "issues", len(fc.Issues), "critical", len(fc.CriticalIssues()))
		}
		st.lastFC, st.fcRounds = &fc, round

		if fc.Status != validation.FactNeedsRevision || round == maxRounds || len(fc.RecommendedResearch) == 0 {
			break
		}
		jobs := o.followUps(st, fc, round)
		if len(jobs) == 0 {
			break
		}
		o.Log.Info(fmt.Sprintf("fact checker requested %d follow-up research task(s)", len(jobs)))
		o.runResearch(ctx, st, jobs)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for _, j := range jobs {
			if err := o.ingestResearch(st, j); err != nil {
				o.Log.Warn("could not ingest follow-up", "artifact", j.Artifact, "error", err)
			}
		}
		if err := o.saveEvidence(st); err != nil {
			return "", err
		}
	}
	validation.AcceptPending(st.store)
	if err := o.saveEvidence(st); err != nil {
		return "", err
	}

	rg := validation.ResearchGate(st.store, o.Config.MinSourcesFor())
	fg := validation.FactGate(st.lastFC, st.fcRounds)
	for _, g := range []validation.Gate{rg, fg} {
		o.reportGate(st, g)
	}
	if !rg.Passed {
		return "", &ErrBlocked{Gate: rg.Name, Errors: rg.Errors}
	}
	if !fg.Passed {
		return "", &ErrBlocked{Gate: fg.Name, Errors: fg.Errors}
	}
	stats := st.store.Stats()
	return fmt.Sprintf("%d round(s), final status %s; %d usable findings, %d rejected", st.fcRounds, st.lastFC.Status, stats.Usable, stats.Rejected), nil
}

// skipFactCheck accepts the corpus unreviewed (quick mode) and still runs the
// research gate.
func (o *Orchestrator) skipFactCheck(st *state) (string, error) {
	o.Log.Warn("fact-checking is off for this run; claims are not independently verified")
	validation.AcceptPending(st.store)
	if err := o.saveEvidence(st); err != nil {
		return "", err
	}
	rg := validation.ResearchGate(st.store, o.Config.MinSourcesFor())
	o.reportGate(st, rg)
	if !rg.Passed {
		return "", &ErrBlocked{Gate: rg.Name, Errors: rg.Errors}
	}
	return fmt.Sprintf("skipped; %d usable findings", st.store.Stats().Usable), nil
}

func (o *Orchestrator) factCheckSpec(st *state, round int, rel string) taskSpec {
	var ctxb strings.Builder
	fmt.Fprintf(&ctxb, "Report objective: %s\n\nResearch questions:\n", st.plan.Objective)
	for _, q := range st.plan.ResearchQuestions {
		fmt.Fprintf(&ctxb, "- %s\n", q)
	}
	if len(st.failed) > 0 {
		fmt.Fprintf(&ctxb, "\nThese research workstreams failed and produced no findings: %s\n", strings.Join(st.failed, ", "))
	}
	inputs := []agentArtifact{{"research corpus", st.store.CorpusJSON(true)}}
	if round > 1 && st.lastFC != nil {
		inputs = append(inputs, agentArtifact{"previous fact-check round", mustJSON(map[string]any{
			"status": st.lastFC.Status, "issues": st.lastFC.Issues, "claims_to_verify": st.lastFC.ClaimsToVerify,
		})})
	}
	cons := []string{
		"Verify the most consequential claims first; you cannot check everything.",
		fmt.Sprintf("This is round %d of at most %d. Recommend follow-up research only for critical or major gaps.", round, o.Config.Research.MaxIterations+1),
	}
	if round > 1 {
		cons = append(cons, "Focus on findings added since the previous round and on whether previous issues are now resolved. Do not re-raise resolved issues.")
	}
	return taskSpec{
		ID: fmt.Sprintf("factcheck-%d", round), Role: agent.RoleFactChecker, Stage: run.StageFactCheck, Schema: "factcheck",
		Objective: "Review the research corpus for accuracy, sourcing, freshness and completeness before the report is written.",
		Context:   ctxb.String(), Constraints: cons, Inputs: toArtifacts(inputs), Tools: o.tools(), Artifact: rel,
	}
}

func (o *Orchestrator) followUps(st *state, fc validation.FactCheck, round int) []researchJob {
	max := o.Config.Agents.MaxAgents
	var jobs []researchJob
	for i, fu := range fc.RecommendedResearch {
		if i == max {
			o.Log.Warn("follow-up research capped", "requested", len(fc.RecommendedResearch), "max_agents", max)
			break
		}
		id := fmt.Sprintf("followup-r%d-%02d", round, i+1)
		reason := fu.Objective
		if len(fu.FindingIDs) > 0 {
			reason += " (concerning " + strings.Join(fu.FindingIDs, ", ") + ")"
		}
		jobs = append(jobs, researchJob{
			ID: id, Artifact: "research/" + id + ".json", Known: st.store.ClaimList(80), Reason: reason,
			Workstream: Workstream{ID: id, Role: roleOrDefault(fu.Role), Objective: fu.Objective, Questions: fu.Questions},
		})
	}
	return jobs
}

func (o *Orchestrator) reportGate(st *state, g validation.Gate) {
	st.gates = append(st.gates, g)
	_ = st.run.WriteJSON("report/gates.json", st.gates)
	if g.Passed {
		o.Log.Info(fmt.Sprintf("%s gate passed", g.Name), "warnings", len(g.Warnings))
	} else {
		o.Log.Error(fmt.Sprintf("%s gate failed", g.Name), "errors", len(g.Errors))
	}
	for _, e := range g.Errors {
		o.Log.Error("  " + e)
	}
	for _, w := range g.Warnings {
		o.Log.Warn("  " + w)
	}
	_ = st.run.Update(func(m *run.Manifest) {
		for _, w := range g.Warnings {
			m.Warnings = appendUnique(m.Warnings, g.Name+": "+w)
		}
	})
}

type agentArtifact struct{ name, content string }

func toArtifacts(in []agentArtifact) []agent.Artifact {
	out := make([]agent.Artifact, len(in))
	for i, a := range in {
		out[i] = agent.Artifact{Name: a.name, Content: a.content}
	}
	return out
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
