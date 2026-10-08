// Package agent defines the contract between Boku and the agent runtimes that
// perform research and writing. The orchestrator only depends on the Agent
// interface; Claude Code is one implementation of it.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Roles known to Boku. Each role has a prompt in prompts/<role>.md.
const (
	RolePlanner     = "planner"
	RolePrimary     = "primary"
	RoleMarket      = "market"
	RoleTechnical   = "technical"
	RoleFinancial   = "financial"
	RoleCompetitive = "competitive"
	RoleCaseStudy   = "case-study"
	RoleFactChecker = "fact-checker"
	RoleSynthesizer = "synthesizer"
	RoleEditorial   = "editorial"
	// RoleFormatter rewrites flagged passages for style on the local model.
	RoleFormatter = "formatter"
)

// AllRoles lists every role, in pipeline order.
var AllRoles = []string{RolePlanner, RolePrimary, RoleMarket, RoleTechnical, RoleFinancial, RoleCompetitive, RoleCaseStudy, RoleFactChecker, RoleSynthesizer, RoleEditorial, RoleFormatter}

// ResearchRoles are the roles the planner may assign to workstreams.
var ResearchRoles = []string{RolePrimary, RoleMarket, RoleTechnical, RoleFinancial, RoleCompetitive, RoleCaseStudy}

// IsResearchRole reports whether role is a research workstream role.
func IsResearchRole(role string) bool {
	for _, r := range ResearchRoles {
		if r == role {
			return true
		}
	}
	return false
}

// Tool names an agent may be granted. Agents never get shell or file-write
// tools; the file tools are read-only and used to explain a local codebase.
const (
	ToolWebSearch = "WebSearch"
	ToolWebFetch  = "WebFetch"
	ToolRead      = "Read"
	ToolGlob      = "Glob"
	ToolGrep      = "Grep"
)

// Artifact is a named piece of input or output content.
type Artifact struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Task is one unit of work for an agent.
type Task struct {
	ID          string
	Role        string
	Objective   string
	Context     string
	Inputs      []Artifact
	Constraints []string
	// Instructions is the role's system prompt.
	Instructions string
	// Schema is the JSON Schema the structured result must satisfy.
	Schema json.RawMessage
	// Tools the agent may use (see Tool* constants). Empty means none.
	Tools []string
	// SearchQueries seed Boku's own retrieval for runtimes without web tools
	// (see ChatModel). Claude Code ignores them and searches by itself.
	SearchQueries []string
	// SearchPages caps the pages fetched for SearchQueries; 0 = runtime default.
	SearchPages int
	// Model overrides the runtime default when non-empty.
	Model string
	// Timeout bounds this task; zero means the runtime default.
	Timeout time.Duration
	// BudgetUSD caps the spend of this task when the runtime supports it; 0 = none.
	BudgetUSD float64
	// WorkDir is the directory the agent runs in: a scratch directory, or
	// the repository being explained when the task has file tools.
	WorkDir string
}

// Usage is what a task cost.
type Usage struct {
	CostUSD  float64       `json:"cost_usd"`
	Duration time.Duration `json:"duration"`
	Turns    int           `json:"turns"`
	Attempts int           `json:"attempts"`
}

// Result is the outcome of a task. Output holds the structured JSON result,
// which callers decode into role-specific types.
type Result struct {
	TaskID string          `json:"task_id"`
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	// Raw is the provider's raw response, kept for debugging.
	Raw    []byte   `json:"-"`
	Usage  Usage    `json:"usage"`
	Errors []string `json:"errors,omitempty"`
}

const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// Agent runs tasks.
type Agent interface {
	Run(ctx context.Context, task Task) (Result, error)
}

// Func adapts a function to the Agent interface. Useful for tests and for
// wrapping agents.
type Func func(ctx context.Context, task Task) (Result, error)

func (f Func) Run(ctx context.Context, task Task) (Result, error) { return f(ctx, task) }

// Error classifies agent failures so callers can decide whether to retry.
type Error struct {
	Kind      ErrorKind
	Msg       string
	Err       error
	Retryable bool
}

type ErrorKind string

const (
	ErrUnavailable ErrorKind = "unavailable"      // runtime missing or not authenticated
	ErrTimeout     ErrorKind = "timeout"          // task exceeded its timeout
	ErrMalformed   ErrorKind = "malformed_output" // output missing or not valid JSON
	ErrRuntime     ErrorKind = "runtime"          // provider reported an error
	ErrBudget      ErrorKind = "budget"           // cost budget exhausted
)

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

// IsRetryable reports whether err is worth retrying. Unknown errors are
// retried; context cancellation, missing runtimes and budget errors are not.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Retryable
	}
	return true
}
