package scope

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/prompts"
)

func expert(t *testing.T, tasks *[]agent.Task, out map[string]any) *Expert {
	t.Helper()
	lib, err := prompts.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return &Expert{Prompts: lib, Config: config.Default(), Agent: agent.Func(func(_ context.Context, task agent.Task) (agent.Result, error) {
		*tasks = append(*tasks, task)
		b, _ := json.Marshal(out[task.ID])
		return agent.Result{TaskID: task.ID, Output: b, Usage: agent.Usage{CostUSD: 0.05}}, nil
	})}
}

func TestAskCleansQuestions(t *testing.T) {
	var tasks []agent.Task
	e := expert(t, &tasks, map[string]any{"scoper-questions": map[string]any{
		"understanding": " You want a market view. ",
		"questions": []map[string]any{
			{"id": "q1", "question": "Which region?", "options": []string{"EU", " ", "US"}},
			{"id": "q1", "question": "Which segment?", "options": []string{"SMB", "Enterprise"}, "multi": true},
			{"id": "q9", "question": "  "},
		},
	}})
	iv, cost, err := e.Ask(context.Background(), "  EV charging market  ")
	if err != nil {
		t.Fatal(err)
	}
	if cost != 0.05 || iv.Understanding != "You want a market view." || len(iv.Questions) != 2 {
		t.Fatalf("interview = %+v, cost %v", iv, cost)
	}
	if iv.Questions[0].ID == iv.Questions[1].ID || len(iv.Questions[0].Options) != 2 || !iv.Questions[1].Multi {
		t.Errorf("questions not cleaned: %+v", iv.Questions)
	}
	task := tasks[0]
	if task.Role != agent.RoleScoper || !strings.Contains(task.Objective, "EV charging market") || !strings.Contains(task.Instructions, "scoping expert") {
		t.Errorf("task = %+v", task)
	}
	if len(task.Tools) != 1 || task.Tools[0] != agent.ToolWebSearch || len(task.Schema) == 0 {
		t.Errorf("ask should search the web and carry a schema: tools %v", task.Tools)
	}
	if _, _, err := e.Ask(context.Background(), "   "); err == nil {
		t.Error("empty topic accepted")
	}
}

func TestRefineUsesAnswersAndMarksSkipped(t *testing.T) {
	var tasks []agent.Task
	e := expert(t, &tasks, map[string]any{"scoper-brief": map[string]any{
		"topic": " Assess the EU public EV charging market, 2023–2026. ", "title": "EU EV charging", "in_scope": []string{"Public AC and DC"},
	}})
	qs := []Question{{ID: "q1", Question: "Which region?", Options: []string{"EU", "US"}}, {ID: "q2", Question: "Which segment?"}}
	brief, _, err := e.Refine(context.Background(), "EV charging market", qs, []Answer{{ID: "q1", Answer: "EU"}, {ID: "q2", Answer: " "}})
	if err != nil {
		t.Fatal(err)
	}
	if brief.Topic != "Assess the EU public EV charging market, 2023–2026." {
		t.Errorf("topic = %q", brief.Topic)
	}
	in := tasks[0].Inputs[0].Content
	if !strings.Contains(in, "A: EU") || !strings.Contains(in, "skipped") || len(tasks[0].Tools) != 0 {
		t.Errorf("interview input = %q, tools %v", in, tasks[0].Tools)
	}
	rec := Record{OriginalTopic: "EV charging market", Questions: qs, Answers: []Answer{{ID: "q1", Answer: "EU"}}, Brief: brief, Topic: brief.Topic}
	md := rec.Markdown()
	if !strings.Contains(md, "**Which region?** EU") || !strings.Contains(md, "_skipped_") || !strings.Contains(md, "## In scope") {
		t.Errorf("markdown = %s", md)
	}
}
