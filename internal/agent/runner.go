package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Budget tracks spend across all agents in a run. It is safe for concurrent use.
type Budget struct {
	mu    sync.Mutex
	limit float64
	spent float64
}

// NewBudget creates a budget; limit 0 means unlimited. spent seeds prior spend
// (for resumed runs).
func NewBudget(limit, spent float64) *Budget { return &Budget{limit: limit, spent: spent} }

func (b *Budget) Add(usd float64) {
	b.mu.Lock()
	b.spent += usd
	b.mu.Unlock()
}

func (b *Budget) Spent() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent
}

// Remaining returns the unspent budget; limited is false when there is no limit.
func (b *Budget) Remaining() (remaining float64, limited bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		return 0, false
	}
	return b.limit - b.spent, true
}

// Runner wraps an Agent with retries, backoff and budget enforcement.
type Runner struct {
	Agent      Agent
	MaxRetries int
	Budget     *Budget
	// Backoff returns the wait before retry n (1-based). Defaults to 2s·n.
	Backoff func(n int) time.Duration
	// OnRetry is called before each retry; optional.
	OnRetry func(task Task, attempt int, err error)
}

// Run executes the task, retrying retryable failures. Usage.Attempts and
// Usage.CostUSD accumulate across attempts.
func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
	var total Usage
	var lastErr error
	for attempt := 0; attempt <= r.MaxRetries; attempt++ {
		if attempt > 0 {
			if r.OnRetry != nil {
				r.OnRetry(task, attempt, lastErr)
			}
			wait := 2 * time.Second * time.Duration(attempt)
			if r.Backoff != nil {
				wait = r.Backoff(attempt)
			}
			select {
			case <-ctx.Done():
				return Result{TaskID: task.ID, Status: StatusFailed, Usage: total}, ctx.Err()
			case <-time.After(wait):
			}
		}
		if r.Budget != nil {
			rem, limited := r.Budget.Remaining()
			if limited && rem <= 0.01 {
				return Result{TaskID: task.ID, Status: StatusFailed, Usage: total},
					&Error{Kind: ErrBudget, Msg: fmt.Sprintf("run budget exhausted ($%.2f spent)", r.Budget.Spent())}
			}
			if limited && (task.BudgetUSD == 0 || task.BudgetUSD > rem) {
				task.BudgetUSD = rem
			}
		}

		res, err := r.Agent.Run(ctx, task)
		total.Attempts++
		total.CostUSD += res.Usage.CostUSD
		total.Duration += res.Usage.Duration
		total.Turns += res.Usage.Turns
		if r.Budget != nil {
			r.Budget.Add(res.Usage.CostUSD)
		}
		if err == nil {
			res.Usage = total
			return res, nil
		}
		lastErr = err
		if !IsRetryable(err) || ctx.Err() != nil {
			break
		}
	}
	res := Result{TaskID: task.ID, Status: StatusFailed, Usage: total}
	if lastErr != nil {
		res.Errors = []string{lastErr.Error()}
	}
	if total.Attempts > 1 {
		return res, fmt.Errorf("task %s failed after %d attempts: %w", task.ID, total.Attempts, lastErr)
	}
	return res, fmt.Errorf("task %s: %w", task.ID, lastErr)
}

// IsBudgetError reports whether err came from budget exhaustion.
func IsBudgetError(err error) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Kind == ErrBudget
}
