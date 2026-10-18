package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPermissionPolicyMissingFileUsesSafeDefaults(t *testing.T) {
	tempDir := t.TempDir()
	missingPath := filepath.Join(tempDir, "missing-permissions.json")

	policy, err := loadPermissionPolicy(missingPath)
	if err != nil {
		t.Fatalf("expected missing file to default, got error: %v", err)
	}

	// Input scope allowed by default → run_script permitted via scope.
	if !policy.IsAllowed(string(CmdRunScript)) {
		t.Fatal("expected input-scoped actions to be allowed by default scopes")
	}

	if policy.IsAllowed(string(CmdShutdownImmediately)) {
		t.Fatal("expected system-scoped shutdown to be denied by scope")
	}

	if policy.IsAllowed(string(CmdInstallExtension)) {
		t.Fatal("expected filesystem-scoped install to be denied by default scopes")
	}
}

func TestLoadPermissionPolicyRespectsAllowDenyRules(t *testing.T) {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "permissions.json")

	content := `{
  "default_allow": false,
  "allowed_actions": ["run_script", "execute_queue"],
  "denied_actions": ["shutdown_immediately"],
  "scopes": {
    "input": { "allowed": true },
    "system": { "allowed": false }
  }
}`

	if err := os.WriteFile(policyPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write policy fixture: %v", err)
	}

	policy, err := loadPermissionPolicy(policyPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	if !policy.IsAllowed("run_script") {
		t.Fatal("expected explicitly allowed action to be permitted")
	}

	if policy.IsAllowed("shutdown_immediately") {
		t.Fatal("expected explicitly denied action to be blocked")
	}

	// mouse_click is input-scoped and scopes.input.allowed=true → allowed via scope
	if !policy.IsAllowed("mouse_click") {
		t.Fatal("expected input-scoped mouse_click to be allowed via scopes")
	}
}

func TestPermissionScopeBlocksSystemActions(t *testing.T) {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "permissions.json")

	content := `{
  "default_allow": true,
  "allowed_actions": [],
  "denied_actions": [],
  "scopes": {
    "system": { "allowed": false },
    "input": { "allowed": true }
  }
}`

	if err := os.WriteFile(policyPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write policy fixture: %v", err)
	}

	policy, err := loadPermissionPolicy(policyPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	if !policy.IsAllowed("mouse_click") {
		t.Fatal("expected input action allowed via default_allow")
	}
	if policy.IsAllowed("lock_workstation") {
		t.Fatal("expected system scope to block lock_workstation")
	}
}
