package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/scope"
	"github.com/riteshsonawane1372/boku/prompts"
)

// saveInterview stores a scoping interview in its run and books its cost.
func saveInterview(r *run.Run, rec scope.Record) error {
	if err := r.WriteJSON(scope.File, rec); err != nil {
		return err
	}
	if err := r.WriteFile("scope.md", []byte(rec.Markdown())); err != nil {
		return err
	}
	r.RecordTask(run.Task{ID: "scoper", Role: agent.RoleScoper, Stage: run.StagePlan, Status: agent.StatusOK, Attempts: 1, CostUSD: rec.CostUSD, Artifact: scope.File, Finished: time.Now()})
	return nil
}

// newExpert builds the scoping expert on the configured agent runtime.
func newExpert(ctx context.Context, cfg config.Config, log *logx.Logger) (*scope.Expert, error) {
	lib, err := prompts.Load(cfg.Output.PromptsDir)
	if err != nil {
		return nil, err
	}
	main, _, err := newAgents(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	return &scope.Expert{Agent: main, Prompts: lib, Config: cfg}, nil
}

// refineTopic interviews the user in the terminal: the expert asks, the user
// answers, and the rewritten topic is used only once the user confirms it.
func refineTopic(ctx context.Context, cfg config.Config, topic string, log *logx.Logger) (*scope.Record, error) {
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil, errors.New("--refine asks questions and needs an interactive terminal; in scripts, pass the full topic instead")
	}
	expert, err := newExpert(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt string) (string, error) {
		fmt.Print(prompt)
		type line struct {
			s   string
			err error
		}
		ch := make(chan line, 1)
		go func() { s, err := in.ReadString('\n'); ch <- line{s, err} }()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case l := <-ch:
			if l.err != nil && l.s == "" {
				return "", fmt.Errorf("no answer: %w", l.err)
			}
			return strings.TrimSpace(l.s), nil
		}
	}

	fmt.Println("An expert is reading your request…")
	iv, cost, err := expert.Ask(ctx, topic)
	if err != nil {
		return nil, err
	}
	rec := &scope.Record{OriginalTopic: topic, Understanding: iv.Understanding, Questions: iv.Questions, CostUSD: cost}
	fmt.Printf("\n%s\n\nAnswer with a number, several numbers (1,3), your own words, or Enter to let the expert decide.\n", iv.Understanding)
	for i, q := range iv.Questions {
		fmt.Printf("\n%d/%d  %s\n", i+1, len(iv.Questions), q.Question)
		if q.Why != "" {
			fmt.Printf("     (%s)\n", q.Why)
		}
		for j, opt := range q.Options {
			fmt.Printf("     %d. %s\n", j+1, opt)
		}
		reply, err := ask("   > ")
		if err != nil {
			return nil, err
		}
		rec.Answers = append(rec.Answers, scope.Answer{ID: q.ID, Answer: pickOptions(reply, q.Options)})
	}

	fmt.Println("\nWriting the brief…")
	brief, cost, err := expert.Refine(ctx, topic, iv.Questions, rec.Answers)
	rec.CostUSD += cost
	if err != nil {
		return nil, err
	}
	rec.Brief, rec.Topic = brief, brief.Topic
	fmt.Printf("\nRefined topic\n-------------\n%s\n", brief.Topic)
	for _, part := range []struct {
		title string
		items []string
	}{{"In scope", brief.InScope}, {"Out of scope", brief.OutOfScope}, {"Assumptions", brief.Assumptions}} {
		if len(part.items) > 0 {
			fmt.Printf("\n%s\n", part.title)
			for _, it := range part.items {
				fmt.Printf("  - %s\n", it)
			}
		}
	}
	for {
		reply, err := ask(fmt.Sprintf("\nUse this as the topic? [Y]es / [e]dit / [o]riginal / [q]uit  (scoping cost $%.2f) > ", rec.CostUSD))
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(reply) {
		case "", "y", "yes":
			return rec, nil
		case "o", "original":
			rec.Topic = topic
			return rec, nil
		case "e", "edit":
			edited, err := ask("Topic > ")
			if err != nil {
				return nil, err
			}
			if edited != "" {
				rec.Topic = edited
				return rec, nil
			}
		case "q", "quit", "n", "no":
			return nil, errors.New("stopped before the run; nothing was researched")
		}
	}
}

// pickOptions turns a reply of option numbers ("2" or "1,3") into the
// options' text; anything else is the user's own answer.
func pickOptions(reply string, options []string) string {
	if reply == "" {
		return ""
	}
	var picked []string
	for _, p := range strings.FieldsFunc(reply, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > len(options) {
			return reply
		}
		picked = append(picked, options[n-1])
	}
	return strings.Join(picked, "; ")
}
