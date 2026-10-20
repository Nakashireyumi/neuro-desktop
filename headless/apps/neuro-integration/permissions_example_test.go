package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The example policy is what most operators copy first. It must load, it must
// request the scopes the docs promise, and every action it names must exist, or
// the policy quietly does nothing.
func TestExamplePolicyLoadsWithTheDocumentedFlags(t *testing.T) {
	policy, err := loadPermissionPolicy("permissions.example.json")
	if err != nil {
		t.Fatalf("the example policy does not load: %v", err)
	}
	configs := policy.ScopeConfigs()
	for _, scope := range []PermissionScope{ScopeFilesystem, ScopeExtensions} {
		cfg, ok := configs[scope]
		if !ok || cfg.Allowed || !cfg.Requestable {
			t.Errorf("%s should be off and requestable in the example, got %+v", scope, cfg)
		}
	}
	for _, scope := range []PermissionScope{ScopeShell, ScopeSystem} {
		cfg, ok := configs[scope]
		if ok && (cfg.Allowed || cfg.Requestable) {
			t.Errorf("%s must be off and not requestable in the example, got %+v", scope, cfg)
		}
	}
	if policy.IsAllowed("shell_command") {
		t.Error("the example policy must not allow the shell")
	}
	if !policy.IsAllowed("reset_controls") || !policy.IsAllowed("request_permission") {
		t.Error("the escape hatch and permission requests must work under the example policy")
	}
}

func TestExamplePolicyNamesOnlyRealActions(t *testing.T) {
	raw, err := os.ReadFile("permissions.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		AllowedActions []string `json:"allowed_actions"`
		DeniedActions  []string `json:"denied_actions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(data)
	}
	code := source.String()
	for _, name := range append(append([]string{}, doc.AllowedActions...), doc.DeniedActions...) {
		if !strings.Contains(code, `"`+name+`"`) {
			t.Errorf("the example policy names %q, which no action in the bridge uses", name)
		}
	}
}
