package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setEnv sets environment variables for one test and restores them afterwards.
func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		prev, had := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
		k, p, h := key, prev, had
		t.Cleanup(func() {
			if h {
				_ = os.Setenv(k, p)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

func TestPathInsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	cases := []struct {
		target string
		want   bool
	}{
		{filepath.Join(root, "mcp-bridge"), true},
		{root, false}, // the root itself is not an extension
		{filepath.Join(root, "..", "evil"), false},      // traversal out
		{root + "-evil", false},                         // a string prefix is not containment
		{filepath.Join(root, "..", "plugins-2"), false}, // sibling with the same prefix
	}
	for _, tc := range cases {
		if got := pathInsideRoot(root, tc.target); got != tc.want {
			t.Errorf("pathInsideRoot(%q, %q) = %v, want %v", root, tc.target, got, tc.want)
		}
	}
}

func TestNormalizeExtensionIDRejectsPathsAndTraversal(t *testing.T) {
	for _, bad := range []string{"", "..", "../x", "a/b", `a\b`, "a b", strings.Repeat("a", 65), ".hidden", "-flag"} {
		if _, err := normalizeExtensionID(bad); err == nil {
			t.Errorf("normalizeExtensionID(%q) should be refused", bad)
		}
	}
	if id, err := normalizeExtensionID("  MCP-Bridge "); err != nil || id != "mcp-bridge" {
		t.Errorf("a plain id should normalise to lowercase, got %q %v", id, err)
	}
}

// installFixture points the catalog, state, and extension directory at a temp
// dir and writes a catalog with the given items.
func installFixture(t *testing.T, items string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	catalogPath := filepath.Join(dir, "index.json")
	if err := os.WriteFile(catalogPath, []byte(`{"items": `+items+`}`), 0644); err != nil {
		t.Fatal(err)
	}
	setEnv(t, map[string]string{
		"NEURO_CATALOG_FILE":              catalogPath,
		"NEURO_EXTENSIONS_STATE_FILE":     filepath.Join(dir, "state.json"),
		"NEURO_EXTENSION_DIR":             filepath.Join(dir, "plugins"),
		"NEURO_PUBLISHERS_FILE":           filepath.Join(dir, "publishers.json"),
		"NEURO_EXTENSIONS_ALLOW_UNSIGNED": "",
	})
	return dir
}

const commitA = "0123456789abcdef0123456789abcdef01234567"

func TestInstallRefusesTamperedSignature(t *testing.T) {
	dir := installFixture(t, `[{"id":"mcp-bridge","name":"MCP","description":"x","repository":"https://example.com/m","publisher":"acme","commit":"`+commitA+`","signature":"`+strings.Repeat("A", 86)+`=="}]`)
	writeJSONFile(t, filepath.Join(dir, "publishers.json"), publisherFile{Publishers: []publisherKey{{ID: "acme", PublicKey: strings.Repeat("A", 43) + "="}}})
	setEnv(t, map[string]string{"NEURO_EXTENSION_INSTALL_MODE": "metadata_only"})

	result := (&NDIntegration{}).installExtension("mcp-bridge")
	if result.Successful {
		t.Fatalf("an item with a bad signature must not install, even in metadata mode")
	}
	if !strings.Contains(result.Message, "invalid") {
		t.Fatalf("expected the refusal to say the signature is invalid, got: %s", result.Message)
	}
}

func TestGitCloneRefusesUnsignedItem(t *testing.T) {
	installFixture(t, `[{"id":"mcp-bridge","name":"MCP","description":"x","repository":"https://example.com/m","commit":"`+commitA+`"}]`)
	setEnv(t, map[string]string{"NEURO_EXTENSION_INSTALL_MODE": "git_clone"})

	result := (&NDIntegration{}).installExtension("mcp-bridge")
	if result.Successful || !strings.Contains(result.Message, "unsigned") {
		t.Fatalf("git_clone must refuse an unsigned item, got: %+v", result)
	}
}

func TestGitCloneRefusesHTTPAndUnpinnedEvenWhenAllowed(t *testing.T) {
	// In a dev build the lab escape hatch lets an unsigned item through the trust
	// gate, but the transport and pinning rules still apply. A release build
	// refuses at the trust gate first, so there only the refusal is checked.
	installFixture(t, `[
  {"id":"plain-http","name":"P","description":"x","repository":"http://example.com/p","commit":"`+commitA+`"},
  {"id":"unpinned","name":"U","description":"x","repository":"https://example.com/u"}
]`)
	setEnv(t, map[string]string{
		"NEURO_EXTENSION_INSTALL_MODE":    "git_clone",
		"NEURO_EXTENSIONS_ALLOW_UNSIGNED": "1",
	})

	result := (&NDIntegration{}).installExtension("plain-http")
	if result.Successful {
		t.Fatalf("http:// repositories must be refused, got: %+v", result)
	}
	if extensionsDevBuild && !strings.Contains(result.Message, "https") {
		t.Fatalf("a dev build should refuse on the transport rule, got: %+v", result)
	}
	result = (&NDIntegration{}).installExtension("unpinned")
	if result.Successful {
		t.Fatalf("an unpinned item must be refused, got: %+v", result)
	}
	if extensionsDevBuild && !strings.Contains(result.Message, "pin") {
		t.Fatalf("a dev build should refuse on the pinning rule, got: %+v", result)
	}
}

func TestRequireHTTPSRepoRejectsCredentialsAndOtherSchemes(t *testing.T) {
	for _, bad := range []string{"http://x/y", "file:///tmp/x", "git@github.com:a/b.git", "https://user:pass@example.com/x", "https://"} {
		if _, err := requireHTTPSRepo(bad); err == nil {
			t.Errorf("requireHTTPSRepo(%q) should be refused", bad)
		}
	}
	if _, err := requireHTTPSRepo("https://github.com/cassitly/neuro-integration-sdk"); err != nil {
		t.Errorf("a plain https repository should be accepted: %v", err)
	}
}

func TestMetadataInstallRecordsSignatureState(t *testing.T) {
	dir := installFixture(t, `[{"id":"notes","name":"Notes","description":"x","publisher":"acme","commit":"`+commitA+`"}]`)
	writeJSONFile(t, filepath.Join(dir, "publishers.json"), publisherFile{Publishers: []publisherKey{{ID: "acme", PublicKey: strings.Repeat("A", 43) + "="}}})
	setEnv(t, map[string]string{"NEURO_EXTENSION_INSTALL_MODE": "metadata_only"})

	// Unsigned, so the state records "unsigned": metadata installs fetch nothing.
	result := (&NDIntegration{}).installExtension("notes")
	if !result.Successful {
		t.Fatalf("a metadata install of an unsigned item should succeed: %s", result.Message)
	}
	list := (&NDIntegration{}).listInstalledExtensions()
	if !strings.Contains(list.Message, "notes (disabled) signature=unsigned") {
		t.Fatalf("expected the signature state in the listing, got: %s", list.Message)
	}
}

func TestUninstallRefusesStatePathOutsideRoot(t *testing.T) {
	dir := installFixture(t, `[]`)
	state := ExtensionStateFile{Installed: map[string]ExtensionInstallState{
		"evil": {Enabled: true, Path: filepath.Join(dir, "elsewhere")},
	}}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "elsewhere")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	result := (&NDIntegration{}).uninstallExtension("evil")
	if result.Successful {
		t.Fatalf("uninstall must refuse to delete outside the extension directory")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("the directory outside the root must still exist: %v", err)
	}
}

func TestFetchPinnedCommitFromLocalRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	src := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", src}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, stderr.String())
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	// A disposable fixture must not use the developer's signing key or hooks.
	run("config", "commit.gpgsign", "false")
	run("config", "core.hooksPath", t.TempDir())
	run("config", "uploadpack.allowAnySHA1InWant", "true")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "first")
	sha := run("rev-parse", "HEAD")

	dest := filepath.Join(t.TempDir(), "checkout")
	if err := fetchPinnedCommit(src, sha, dest); err != nil {
		t.Fatalf("fetch at the pinned commit failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("expected the pinned tree to be checked out: %v", err)
	}

	// A commit that does not exist must leave nothing behind.
	bad := filepath.Join(t.TempDir(), "bad")
	if err := fetchPinnedCommit(src, strings.Repeat("f", 40), bad); err == nil {
		t.Fatalf("a missing commit must fail")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("a failed fetch must not leave a partial checkout behind")
	}
}

func TestInstallFromNeuroIsQueuedNotRunInline(t *testing.T) {
	installFixture(t, `[{"id":"mcp-bridge","name":"MCP","description":"x","repository":"https://example.com/m","commit":"`+commitA+`"}]`)
	setEnv(t, map[string]string{"NEURO_EXTENSION_INSTALL_MODE": "git_clone"})

	integration := &NDIntegration{stop: newStopSwitch(), stats: newBridgeStats()}
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true}
	integration.setPolicy(policy)
	grantExtensionFixture(t, "mcp-bridge", ExtensionPermissionPolicy{ManageAllowed: true})

	var spec actionSpec
	for _, candidate := range allActionSpecs() {
		if candidate.Name == CmdInstallExtension {
			spec = candidate
		}
	}
	action := &IPCProxyAction{integration: integration, spec: spec}

	state, result := action.Validate(json.RawMessage(`{"item_id":"mcp-bridge"}`))
	if !result.Successful {
		t.Fatalf("Validate should acknowledge the install, got: %s", result.Message)
	}
	work, ok := state.(pendingWork)
	if !ok || work.extension == nil || work.extension.id != "mcp-bridge" {
		t.Fatalf("install must be queued as an extension job, got %#v", state)
	}
	// Validate must not have cloned anything: the extension directory is still empty.
	if entries, err := os.ReadDir(extensionRootPath()); err == nil && len(entries) > 0 {
		t.Fatalf("Validate must not fetch the extension inline; found %d entries", len(entries))
	}
}

func TestInstallExtensionScopeIsExplicitConsent(t *testing.T) {
	policy := defaultPermissionPolicy()
	policy.allowed[string(CmdInstallExtension)] = struct{}{}
	if policy.IsAllowed(string(CmdInstallExtension)) {
		t.Fatalf("listing install_extension in allowed_actions must not enable the extensions scope")
	}
}
