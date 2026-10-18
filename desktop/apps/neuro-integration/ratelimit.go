package main

import (
	"fmt"
	"sync"
	"time"
)

// actionRateLimiter enforces the dashboard's per-scope "max actions per minute".
//
// It is a sliding window rather than a fixed counter, so a burst at the end of
// one minute and the start of the next still counts as 2x the budget. Failing
// closed (denial with a retry hint) is the right default for an agent driving a
// real desktop, and Vedal can raise the limit in the dashboard.
type actionRateLimiter struct {
	mu      sync.Mutex
	windows map[PermissionScope][]time.Time
}

func (l *actionRateLimiter) allow(scope PermissionScope, maxPerMinute int, now time.Time) (bool, time.Duration) {
	if maxPerMinute <= 0 || scope == "" {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.windows == nil {
		l.windows = map[PermissionScope][]time.Time{}
	}

	cutoff := now.Add(-time.Minute)
	recent := l.windows[scope][:0]
	for _, stamp := range l.windows[scope] {
		if stamp.After(cutoff) {
			recent = append(recent, stamp)
		}
	}

	if len(recent) >= maxPerMinute {
		// The budget frees up when the oldest hit in the window expires.
		l.windows[scope] = recent
		retryAfter := recent[0].Sub(cutoff)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}

	l.windows[scope] = append(recent, now)
	return true, 0
}

func (l *actionRateLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.windows = nil
}

// rateLimitDenial builds the message Neuro sees when a scope is over budget.
func rateLimitDenial(scope PermissionScope, limit int, retryAfter time.Duration) string {
	seconds := int(retryAfter.Seconds()) + 1
	return fmt.Sprintf(
		"Rate limit reached for the %s scope (%d actions per minute). Try again in about %ds; "+
			"Vedal can raise the limit in the dashboard under Permissions.",
		scope, limit, seconds)
}
