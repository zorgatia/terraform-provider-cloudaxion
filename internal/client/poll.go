package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The CloudAxion API has no task or job endpoint because it does not need one:
// write operations block until they finish and return the final state. Verified
// on 2026-08-26 — a VM create returned status "running" after 33 seconds, and
// stop returned "stopped" after 22, with no intermediate state ever observed.
//
// These helpers are therefore a safety net rather than the primary mechanism.
// They cost one extra GET when the resource is already settled, and they cover
// the case where a future API version turns an operation asynchronous.
const (
	defaultPollInterval = 3 * time.Second
	defaultPollTimeout  = 20 * time.Minute
)

// ErrPollTimeout is returned when a resource does not reach its target state in
// time. It is wrapped, so callers can test with errors.Is.
var ErrPollTimeout = errors.New("cloudaxion: timed out waiting for resource state")

// Poll describes one wait-for-state loop.
type Poll struct {
	// Fetch reads the current state. Returning a *APIError with status 404 is
	// meaningful: it satisfies GoneIsDone, and otherwise fails the wait.
	Fetch func(ctx context.Context) (state string, err error)

	// Target lists the states that end the wait successfully.
	Target []string

	// Pending lists the states that are expected while waiting. Any state that
	// is in neither Target nor Pending is treated as terminal and fails fast,
	// which surfaces states like "error" instead of waiting out the timeout.
	Pending []string

	// GoneIsDone makes a 404 a successful outcome, as when waiting for a delete.
	GoneIsDone bool

	Interval time.Duration
	Timeout  time.Duration
}

// Wait blocks until the resource reaches a target state, the context is
// cancelled, or the timeout elapses.
func (p Poll) Wait(ctx context.Context) error {
	interval := p.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastState string
	for {
		state, err := p.Fetch(ctx)
		switch {
		case err == nil:
			lastState = state
			if matchesState(state, p.Target) {
				return nil
			}
			if !matchesState(state, p.Pending) {
				return fmt.Errorf(
					"cloudaxion: unexpected state %q while waiting for %s",
					state, strings.Join(p.Target, " or "),
				)
			}
		case IsNotFound(err) && p.GoneIsDone:
			return nil
		case IsNotFound(err):
			return err
		default:
			return err
		}

		select {
		case <-ctx.Done():
			// Distinguish our own deadline from a cancelled parent context; only
			// the former is a poll timeout worth reporting as such.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%w: wanted %s, last saw %q after %s",
					ErrPollTimeout, strings.Join(p.Target, " or "), lastState, timeout)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// matchesState compares case-insensitively: the API is inconsistent about
// capitalisation (VM status "running" against disk status "Active").
func matchesState(state string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(state, candidate) {
			return true
		}
	}
	return false
}
