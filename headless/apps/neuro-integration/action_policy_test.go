package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempPolicy writes a policy file for a test and returns its path.
func writeTempPolicy(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}
	return path
}

// newPolicyAction builds an action the way registerActions does, so policy,
// rate limiting and validation are exercised through the real entry point.
func newPolicyAction(t *testing.T, spec actionSpec, policyJSON string) (*IPCProxyAction, *NDIntegration) {
	t.Helper()

	integration := &NDIntegration{stats: newBridgeStats(), relay: newRelayState(RelayConfig{})}

	if policyJSON != "" {
		path := writeTempPolicy(t, policyJSON)
		policy, err := loadPermissionPolicy(path)
		if err != nil {
			t.Fatalf("failed to load policy: %v", err)
		}
		integration.setPolicy(policy)
	}

	return &IPCProxyAction{integration: integration, spec: spec}, integration
}

func TestValidateCountsActionsAndDenials(t *testing.T) {
	spec := actionSpec{Name: CmdMouseClick, Description: "click"}
	action, integration := newPolicyAction(t, spec, `{
	  "default_allow": false,
	  "scopes": {"input": false}
	}`)

	if _, result := action.Validate(json.RawMessage(`{"x": 1}`)); result.Successful {
		t.Fatal("a disabled scope must deny the action")
	}

	snapshot := integration.stats.snapshot()
	if seen, _ := snapshot["actions_seen"].(int); seen != 1 {
		t.Fatalf("expected the action to be counted as seen, got %v", snapshot["actions_seen"])
	}
	if denied, _ := snapshot["actions_denied"].(int); denied != 1 {
		t.Fatalf("expected the denial to be counted, got %v", snapshot["actions_denied"])
	}
	if failed, _ := snapshot["actions_failed"].(int); failed != 0 {
		t.Fatalf("a policy denial is not a runtime failure, got %v", snapshot["actions_failed"])
	}
}

func TestValidateEnforcesScopeRateLimit(t *testing.T) {
	spec := actionSpec{Name: CmdMouseClick, Description: "click"}
	action, integration := newPolicyAction(t, spec, `{
	  "default_allow": false,
	  "scopes": {"input": {"allowed": true, "limits": {"max_actions_per_minute": 2}}}
	}`)

	for i := 0; i < 2; i++ {
		if _, result := action.Validate(json.RawMessage(`{}`)); !result.Successful {
			t.Fatalf("action %d should be inside the budget: %s", i+1, result.Message)
		}
	}

	_, result := action.Validate(json.RawMessage(`{}`))
	if result.Successful {
		t.Fatal("the third action must be refused by the rate limit")
	}
	if !strings.Contains(result.Message, "dashboard") {
		t.Fatalf("the denial should tell the operator how to fix it, got %q", result.Message)
	}
	if denied, _ := integration.stats.snapshot()["actions_denied"].(int); denied != 1 {
		t.Fatalf("rate-limit refusals should show up as denials, got %d", denied)
	}
}

func TestValidateRequiresSystemScopeForLaunchScripts(t *testing.T) {
	spec := actionSpec{Name: CmdRunScript, Description: "script"}

	action, _ := newPolicyAction(t, spec, `{
	  "default_allow": true,
	  "scopes": {"input": true, "system": false}
	}`)

	payload, err := json.Marshal(map[string]interface{}{"script": "LAUNCH steam://run/400"})
	if err != nil {
		t.Fatalf("failed to build payload: %v", err)
	}

	_, result := action.Validate(payload)
	if result.Successful {
		t.Fatal("LAUNCH must be denied while the system scope is closed")
	}
	if !strings.Contains(result.Message, "system") {
		t.Fatalf("the denial should name the missing scope, got %q", result.Message)
	}

	// A plain input script is unaffected by the system scope.
	payload, _ = json.Marshal(map[string]interface{}{"script": "TYPE \"hello\"\nENTER"})
	if _, result := action.Validate(payload); !result.Successful {
		t.Fatalf("typing must not need the system scope: %s", result.Message)
	}
}

func TestValidateAllowsLaunchWhenSystemScopeIsOpen(t *testing.T) {
	spec := actionSpec{Name: CmdRunScript, Description: "script"}
	action, _ := newPolicyAction(t, spec, `{
	  "default_allow": false,
	  "scopes": {"input": true, "system": true}
	}`)

	payload, _ := json.Marshal(map[string]interface{}{"script": "launch notepad  # lower case works too"})
	if _, result := action.Validate(payload); !result.Successful {
		t.Fatalf("an open system scope should allow LAUNCH: %s", result.Message)
	}
}

func TestGameActionsAreScopedToTheGameInterface(t *testing.T) {
	for _, name := range []string{
		string(CmdGameMove), string(CmdGameLook), string(CmdGamePress),
		string(CmdGameAction), string(CmdGameStartSession), string(CmdGameEndSession),
	} {
		scope, ok := actionScope[name]
		if !ok {
			t.Fatalf("%s has no scope, so the dashboard cannot show it", name)
		}
		if scope != ScopeGame {
			t.Fatalf("%s should need the game scope, got %q", name, scope)
		}
	}
}
