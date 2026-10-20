package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPermissionPolicyAcceptsBooleanShorthandAndLimits(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "permissions.json")

	// Hand-written policies are a normal thing for Vedal to do, so both the
	// documented shape and the "quick and dirty" one must load.
	content := `{
  "version": 1,
  "default_allow": "false",
  "scopes": {
    "input": true,
    "game": {"allowed": true, "limits": {"max_actions_per_minute": 30}},
    "system": false
  }
}`

	if err := os.WriteFile(policyPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write policy fixture: %v", err)
	}

	policy, err := loadPermissionPolicy(policyPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	if policy.DefaultAllow {
		t.Fatal(`default_allow: "false" should not enable default allow`)
	}
	if !policy.ScopeAllowed(ScopeInput) {
		t.Fatal(`"input": true should allow the input scope`)
	}
	if policy.ScopeAllowed(ScopeSystem) {
		t.Fatal(`"system": false should deny the system scope`)
	}
	if limit := policy.ScopeRateLimit(ScopeGame); limit != 30 {
		t.Fatalf("expected the game scope limit to be 30, got %d", limit)
	}
	if limit := policy.ScopeRateLimit(ScopeInput); limit != 0 {
		t.Fatalf("a scope without limits must be unlimited, got %d", limit)
	}
}

func TestLoadPermissionPolicyRejectsUnknownScopeValues(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(policyPath, []byte(`{"scopes": {"game": {"allowed": "sometimes"}}}`), 0644); err != nil {
		t.Fatalf("failed to write policy fixture: %v", err)
	}

	if _, err := loadPermissionPolicy(policyPath); err == nil {
		t.Fatal("a scope that is neither true/false nor {allowed: ...} must be rejected")
	}
}

func TestActionRateLimiterSlidingWindow(t *testing.T) {
	var limiter actionRateLimiter
	start := time.Now()

	// The action that uses up the last slot is allowed; the next one is not.
	for i := 0; i < 3; i++ {
		if allowed, _ := limiter.allow(ScopeInput, 3, start); !allowed {
			t.Fatalf("hit %d should be within the budget", i+1)
		}
	}

	allowed, retryAfter := limiter.allow(ScopeInput, 3, start)
	if allowed {
		t.Fatal("the fourth action in the window must be refused")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Fatalf("retry hint should be inside the window, got %s", retryAfter)
	}

	// Scopes have separate budgets.
	if allowed, _ := limiter.allow(ScopeGame, 3, start); !allowed {
		t.Fatal("the game scope has its own budget")
	}

	// Once the window slides past the oldest hit, the budget frees up again.
	later := start.Add(time.Minute + time.Second)
	if allowed, _ := limiter.allow(ScopeInput, 3, later); !allowed {
		t.Fatal("the budget should be available again after the window passes")
	}

	// A limit of zero means unlimited, and must not accumulate state.
	for i := 0; i < 100; i++ {
		if allowed, _ := limiter.allow(ScopeVision, 0, start); !allowed {
			t.Fatal("a limit of zero must be unlimited")
		}
	}
}

func TestRateLimitDenialMessageIsActionable(t *testing.T) {
	message := rateLimitDenial(ScopeGame, 120, 4*time.Second)
	for _, want := range []string{"game", "120", "dashboard"} {
		if !strings.Contains(message, want) {
			t.Fatalf("denial %q should mention %q", message, want)
		}
	}
}
