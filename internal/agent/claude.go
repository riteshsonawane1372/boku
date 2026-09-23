package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ClaudeCode runs tasks through the Claude Code CLI in non-interactive mode.
//
// Each task is a fresh, isolated `claude -p` process:
//   - restricted mode: no shell or code-execution tools, user/project settings ignored
//   - only the tools listed on the task (web search / fetch) are available
//   - no MCP servers, no slash commands, no session persistence
//   - a filtered environment (see FilterEnv)
//   - the structured result is validated by Claude Code against the task's JSON Schema
type ClaudeCode struct {
	// Command is the executable, "claude" by default.
	Command string
	// DefaultTimeout applies when a task has none.
	DefaultTimeout time.Duration
	// EnvPassthrough names extra environment variables to expose.
	EnvPassthrough []string
	// ExtraArgs are appended to every invocation.
	ExtraArgs []string
}

// claudeResponse is the subset of `claude -p --output-format json` we use.
type claudeResponse struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	NumTurns         int             `json:"num_turns"`
	DurationMS       int64           `json:"duration_ms"`
}

// Args builds the CLI arguments for a task. Exposed for tests and docs.
func (c *ClaudeCode) Args(task Task) []string {
	tools := strings.Join(task.Tools, ",")
	args := []string{
		"-p",
		"--output-format", "json",
		"--restricted",
		"--tools", tools,
		"--strict-mcp-config",
		"--no-session-persistence",
		"--disable-slash-commands",
		"--permission-mode", "dontAsk",
	}
	if tools != "" {
		args = append(args, "--allowedTools", tools)
	}
	if task.Instructions != "" {
		args = append(args, "--system-prompt", task.Instructions)
	}
	if len(task.Schema) > 0 {
		args = append(args, "--json-schema", string(task.Schema))
	}
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}
	if task.BudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(task.BudgetUSD, 'f', 2, 64))
	}
	return append(args, c.ExtraArgs...)
}

func (c *ClaudeCode) Run(ctx context.Context, task Task) (Result, error) {
	res := Result{TaskID: task.ID, Status: StatusFailed}
	cmdName := c.Command
	if cmdName == "" {
		cmdName = "claude"
	}
	path, err := exec.LookPath(cmdName)
	if err != nil {
		return res, &Error{Kind: ErrUnavailable, Msg: fmt.Sprintf("Claude Code CLI %q not found in PATH (install: https://docs.claude.com/claude-code)", cmdName), Err: err}
	}

	timeout := task.Timeout
	if timeout == 0 {
		timeout = c.DefaultTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, path, c.Args(task)...)
	cmd.Dir = task.WorkDir
	cmd.Env = FilterEnv(os.Environ(), c.EnvPassthrough)
	cmd.Stdin = strings.NewReader(RenderPrompt(task))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &limitedBuffer{max: 8 << 10, buf: &stderr}
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	res.Usage.Duration = time.Since(start)
	res.Raw = stdout.Bytes()

	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return res, &Error{Kind: ErrTimeout, Msg: fmt.Sprintf("task %s exceeded %s", task.ID, timeout), Retryable: true}
		}
		return res, ctx.Err()
	}

	var resp claudeResponse
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = truncate(stdout.String(), 500)
		}
		if runErr != nil {
			return res, &Error{Kind: ErrRuntime, Msg: "claude exited with error: " + msg, Err: runErr, Retryable: true}
		}
		return res, &Error{Kind: ErrMalformed, Msg: "unparseable CLI response: " + truncate(stdout.String(), 300), Err: err, Retryable: true}
	}
	res.Usage.CostUSD = resp.TotalCostUSD
	res.Usage.Turns = resp.NumTurns

	if resp.IsError || runErr != nil || strings.HasPrefix(resp.Subtype, "error") {
		return res, classifyClaudeError(resp, runErr, stderr.String())
	}

	out, err := extractJSON(resp.StructuredOutput, resp.Result)
	if err != nil {
		return res, &Error{Kind: ErrMalformed, Msg: "agent did not return valid JSON", Err: err, Retryable: true}
	}
	res.Output = out
	res.Status = StatusOK
	return res, nil
}

func classifyClaudeError(resp claudeResponse, runErr error, stderr string) error {
	detail := strings.TrimSpace(resp.Result)
	if detail == "" {
		detail = strings.TrimSpace(stderr)
	}
	if detail == "" && runErr != nil {
		detail = runErr.Error()
	}
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(resp.Subtype, "budget"):
		return &Error{Kind: ErrBudget, Msg: "task budget exhausted: " + truncate(detail, 300)}
	case strings.Contains(lower, "invalid api key"), strings.Contains(lower, "/login"), strings.Contains(lower, "not logged in"), strings.Contains(lower, "authentication"):
		return &Error{Kind: ErrUnavailable, Msg: "Claude Code is not authenticated: " + truncate(detail, 300)}
	}
	return &Error{Kind: ErrRuntime, Msg: fmt.Sprintf("claude reported %s: %s", nonEmpty(resp.Subtype, "error"), truncate(detail, 500)), Err: runErr, Retryable: true}
}

// extractJSON prefers the validated structured output and falls back to
// parsing JSON out of the free-text result (tolerating markdown fences).
func extractJSON(structured json.RawMessage, text string) (json.RawMessage, error) {
	if len(structured) > 0 && string(structured) != "null" {
		return structured, nil
	}
	s := strings.TrimSpace(text)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil, errors.New("no JSON object in result")
	}
	s = s[start:]
	dec := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// allowedEnv are variables agents always receive. Everything else — cloud
// credentials, tokens, SSH agent sockets — is dropped unless passed through.
var allowedEnv = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "TERM", "TMPDIR", "TZ",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
}

var allowedEnvPrefixes = []string{"LC_", "XDG_", "ANTHROPIC_", "CLAUDE_CODE_", "CLAUDE_CONFIG_DIR"}

// FilterEnv returns the subset of env that agents may see.
func FilterEnv(env []string, passthrough []string) []string {
	keep := func(k string) bool {
		for _, a := range allowedEnv {
			if k == a {
				return true
			}
		}
		for _, a := range passthrough {
			if k == a {
				return true
			}
		}
		for _, p := range allowedEnvPrefixes {
			if strings.HasPrefix(k, p) {
				return true
			}
		}
		return false
	}
	var out []string
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && keep(k) {
			out = append(out, kv)
		}
	}
	return out
}

// RenderPrompt turns a task into the user message sent to the agent.
func RenderPrompt(t Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task %s (%s)\n\n## Objective\n\n%s\n", t.ID, t.Role, strings.TrimSpace(t.Objective))
	if s := strings.TrimSpace(t.Context); s != "" {
		fmt.Fprintf(&b, "\n## Context\n\n%s\n", s)
	}
	if len(t.Constraints) > 0 {
		b.WriteString("\n## Constraints\n\n")
		for _, c := range t.Constraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	for _, in := range t.Inputs {
		fmt.Fprintf(&b, "\n## Input: %s\n\n<input name=%q>\n%s\n</input>\n", in.Name, in.Name, strings.TrimSpace(in.Content))
	}
	b.WriteString("\nReturn only the JSON object required by the output schema.\n")
	return b.String()
}

type limitedBuffer struct {
	max int
	buf *bytes.Buffer
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
		} else {
			l.buf.Write(p)
		}
	}
	return len(p), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
