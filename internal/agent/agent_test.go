package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func noBackoff(int) time.Duration { return 0 }

func TestRunnerRetriesRetryableErrors(t *testing.T) {
	var calls atomic.Int32
	a := Func(func(ctx context.Context, task Task) (Result, error) {
		n := calls.Add(1)
		if n < 3 {
			return Result{Usage: Usage{CostUSD: 0.1}}, &Error{Kind: ErrMalformed, Msg: "bad json", Retryable: true}
		}
		return Result{TaskID: task.ID, Status: StatusOK, Output: json.RawMessage(`{}`), Usage: Usage{CostUSD: 0.2}}, nil
	})
	b := NewBudget(0, 0)
	r := &Runner{Agent: a, MaxRetries: 2, Budget: b, Backoff: noBackoff}
	res, err := r.Run(context.Background(), Task{ID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Usage.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", res.Usage.Attempts)
	}
	if got := b.Spent(); got < 0.39 || got > 0.41 {
		t.Errorf("spent = %v, want 0.4 (failed attempts count too)", got)
	}
}

func TestRunnerDoesNotRetryUnavailable(t *testing.T) {
	var calls atomic.Int32
	a := Func(func(ctx context.Context, task Task) (Result, error) {
		calls.Add(1)
		return Result{}, &Error{Kind: ErrUnavailable, Msg: "not installed"}
	})
	r := &Runner{Agent: a, MaxRetries: 5, Backoff: noBackoff}
	_, err := r.Run(context.Background(), Task{ID: "t"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("want 1 call and an error, got %d calls, err=%v", calls.Load(), err)
	}
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != ErrUnavailable {
		t.Errorf("error kind not preserved: %v", err)
	}
}

func TestRunnerGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	a := Func(func(ctx context.Context, task Task) (Result, error) {
		calls.Add(1)
		return Result{}, errors.New("boom")
	})
	r := &Runner{Agent: a, MaxRetries: 2, Backoff: noBackoff}
	_, err := r.Run(context.Background(), Task{ID: "t"})
	if err == nil || calls.Load() != 3 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error should mention attempts: %v", err)
	}
}

func TestRunnerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := Func(func(ctx context.Context, task Task) (Result, error) {
		cancel()
		return Result{}, &Error{Kind: ErrRuntime, Msg: "x", Retryable: true}
	})
	r := &Runner{Agent: a, MaxRetries: 3, Backoff: func(int) time.Duration { return time.Hour }}
	done := make(chan error, 1)
	go func() { _, err := r.Run(ctx, Task{ID: "t"}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}

func TestRunnerEnforcesBudget(t *testing.T) {
	var gotBudget float64
	a := Func(func(ctx context.Context, task Task) (Result, error) {
		gotBudget = task.BudgetUSD
		return Result{Status: StatusOK, Usage: Usage{CostUSD: 1}}, nil
	})
	b := NewBudget(1.5, 0)
	r := &Runner{Agent: a, Budget: b}
	if _, err := r.Run(context.Background(), Task{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if gotBudget != 1.5 {
		t.Errorf("task budget = %v, want remaining 1.5", gotBudget)
	}
	if _, err := r.Run(context.Background(), Task{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	_, err := r.Run(context.Background(), Task{ID: "c"})
	if !IsBudgetError(err) {
		t.Fatalf("want budget error, got %v", err)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		"structured": "",
		"fenced":     "Here you go:\n```json\n{\"a\":1}\n```\n",
		"bare":       "{\"a\":1} trailing words",
	}
	for name, text := range cases {
		var structured json.RawMessage
		if name == "structured" {
			structured = json.RawMessage(`{"a":1}`)
		}
		out, err := extractJSON(structured, text)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var v map[string]int
		if err := json.Unmarshal(out, &v); err != nil || v["a"] != 1 {
			t.Errorf("%s: got %s", name, out)
		}
	}
	if _, err := extractJSON(nil, "no json here"); err == nil {
		t.Error("expected error for missing JSON")
	}
	if _, err := extractJSON(nil, "{\"a\": "); err == nil {
		t.Error("expected error for truncated JSON")
	}
}

func TestFilterEnvDropsSecrets(t *testing.T) {
	env := []string{
		"PATH=/bin", "HOME=/h", "AWS_SECRET_ACCESS_KEY=x", "GITHUB_TOKEN=y",
		"SSH_AUTH_SOCK=/tmp/s", "ANTHROPIC_API_KEY=k", "LC_ALL=C", "AWS_PROFILE=p",
	}
	got := strings.Join(FilterEnv(env, []string{"AWS_PROFILE"}), " ")
	for _, want := range []string{"PATH=/bin", "HOME=/h", "ANTHROPIC_API_KEY=k", "LC_ALL=C", "AWS_PROFILE=p"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, bad := range []string{"AWS_SECRET", "GITHUB_TOKEN", "SSH_AUTH_SOCK"} {
		if strings.Contains(got, bad) {
			t.Errorf("leaked %s", bad)
		}
	}
}

func TestClaudeArgsRestrictTools(t *testing.T) {
	c := &ClaudeCode{}
	args := strings.Join(c.Args(Task{Tools: []string{ToolWebSearch, ToolWebFetch}, Model: "opus", BudgetUSD: 1.234}), " ")
	for _, want := range []string{"--restricted", "--tools WebSearch,WebFetch", "--strict-mcp-config", "--permission-mode dontAsk", "--model opus", "--max-budget-usd 1.23"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	noTools := c.Args(Task{})
	for i, a := range noTools {
		if a == "--tools" && noTools[i+1] != "" {
			t.Errorf("tool-less task must pass empty --tools, got %q", noTools[i+1])
		}
		if a == "--allowedTools" {
			t.Error("tool-less task must not pass --allowedTools")
		}
	}
}

func TestClassifyClaudeError(t *testing.T) {
	err := classifyClaudeError(claudeResponse{IsError: true, Result: "Invalid API key · Please run /login"}, nil, "")
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != ErrUnavailable || ae.Retryable {
		t.Errorf("auth failure should be non-retryable unavailable: %v", err)
	}
	err = classifyClaudeError(claudeResponse{IsError: true, Subtype: "error_max_budget_usd"}, nil, "")
	if !IsBudgetError(err) {
		t.Errorf("want budget error: %v", err)
	}
	err = classifyClaudeError(claudeResponse{IsError: true, Subtype: "error_during_execution", Result: "overloaded"}, nil, "")
	if !IsRetryable(err) {
		t.Errorf("runtime errors should be retryable: %v", err)
	}
}
