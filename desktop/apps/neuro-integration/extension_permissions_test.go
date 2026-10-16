package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extensionPermissionFixture(t *testing.T) *NDIntegration {
	t.Helper()
	dir := installFixture(t, `[{"id":"demo","name":"Demo","permissions":[{"id":"input","description":"Control the game window","required":true}],"settings_schema":{"type":"object","properties":{"visible":{"type":"boolean"}},"additionalProperties":false}}]`)
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", filepath.Join(dir, "extension-permissions.json"))
	t.Setenv("NEURO_EXTENSION_INSTALL_MODE", "metadata_only")
	n := &NDIntegration{stats: newBridgeStats(), stop: newStopSwitch()}
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true}
	policy.scopes[ScopeInput] = ScopeConfig{Allowed: true}
	n.setPolicy(policy)
	return n
}

func grantExtensionFixture(t *testing.T, id string, policy ExtensionPermissionPolicy) {
	t.Helper()
	file, err := loadExtensionPermissions(extensionPermissionsPath())
	if err != nil {
		t.Fatal(err)
	}
	file.Extensions[id] = policy
	if err := saveExtensionPermissions(extensionPermissionsPath(), file); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionPermissionsDenyByDefaultAndCannotWidenGlobalPolicy(t *testing.T) {
	n := extensionPermissionFixture(t)
	declarations := []ExtensionPermissionDeclaration{{ID: "input", Description: "Control the game", Required: true}}
	if err := n.checkExtensionManagement("demo"); err == nil {
		t.Fatal("global scope alone must not delegate an individual extension")
	}
	if err := n.extensionRuntimeAllowed("demo", declarations); err == nil {
		t.Fatal("a new extension must not run by default")
	}
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{ManageAllowed: true, RunAllowed: true, Grants: map[string]bool{"input": true}})
	if err := n.checkExtensionManagement("demo"); err != nil {
		t.Fatal(err)
	}
	if err := n.extensionRuntimeAllowed("demo", declarations); err != nil {
		t.Fatal(err)
	}
	n.policy().scopes[ScopeInput] = ScopeConfig{Allowed: false}
	if err := n.extensionRuntimeAllowed("demo", declarations); err == nil || !strings.Contains(err.Error(), "global input") {
		t.Fatalf("per-item grants cannot override a denied global scope: %v", err)
	}
	n.policy().scopes[ScopeExtensions] = ScopeConfig{Allowed: false}
	if err := n.checkExtensionManagement("demo"); err == nil {
		t.Fatal("per-item delegation cannot override the global scope")
	}
	n.policy().DefaultAllow = true
	delete(n.policy().scopes, ScopeExtensions)
	if err := n.checkExtensionManagement("demo"); err == nil {
		t.Fatal("default_allow must not implicitly grant the global extensions scope")
	}
}

func TestExtensionNewRequiredRightsNeedSeparateApproval(t *testing.T) {
	n := extensionPermissionFixture(t)
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{"input": true}})
	declarations := []ExtensionPermissionDeclaration{{ID: "input", Description: "Control", Required: true}, {ID: "vision", Description: "See", Required: true}}
	if err := n.extensionRuntimeAllowed("demo", declarations); err == nil || !strings.Contains(err.Error(), "vision grant") {
		t.Fatalf("an update must not inherit a newly requested permission: %v", err)
	}
	declarations[1].Required = false
	if err := n.extensionRuntimeAllowed("demo", declarations); err != nil {
		t.Fatalf("an optional ungranted capability must not become required: %v", err)
	}
	declarations[1].ID = "future.right"
	if err := n.extensionRuntimeAllowed("demo", declarations); err == nil {
		t.Fatal("unknown capabilities must not be implicitly granted")
	}
}

func TestExtensionShellGrantNeedsExplicitGlobalConsent(t *testing.T) {
	n := extensionPermissionFixture(t)
	n.policy().DefaultAllow = true
	delete(n.policy().scopes, ScopeShell)
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{"shell": true}})
	declarations := []ExtensionPermissionDeclaration{{ID: "shell", Description: "Run a game helper", Required: true}}
	if err := n.extensionRuntimeAllowed("demo", declarations); err == nil || !strings.Contains(err.Error(), "global shell") {
		t.Fatalf("default_allow must not make a per-item grant into shell consent: %v", err)
	}
}

func TestExtensionApprovalAPIRequiresOperatorReentryAndValidatesSettings(t *testing.T) {
	n := extensionPermissionFixture(t)
	server := NewAdminServer(n, "", credentialFromToken("operator-test-secret", "test"))
	do := func(method, path, body, approval string, signedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if signedIn {
			r.Header.Set("X-ND-Token", "operator-test-secret")
		}
		r.Header.Set("X-ND-Operator-Token", approval)
		w := httptest.NewRecorder()
		server.routes().ServeHTTP(w, r)
		return w
	}
	path := "/api/extensions/demo/permissions"
	valid := `{"manage_allowed":true,"run_allowed":true,"grants":{"input":true},"settings":{"visible":true}}`
	if w := do(http.MethodGet, path, "", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned read = %d: %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPut, path, valid, "", true); w.Code != http.StatusForbidden {
		t.Fatalf("a session token alone must not approve rights: %d: %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPut, path, valid, "wrong", true); w.Code != http.StatusForbidden {
		t.Fatalf("wrong approval = %d", w.Code)
	}
	for _, body := range []string{
		`null`, valid + `{}`,
		`{"run_allowed":true,"grants":{"shell":true}}`,
		`{"settings":{"visible":"yes"}}`,
		`{"settings":{"password":"do-not-store-this"}}`,
		`{"settings":{"unknown":true}}`,
		`{"manage_allowed":true,"new_unknown_field":true}`,
	} {
		if w := do(http.MethodPut, path, body, "operator-test-secret", true); w.Code != http.StatusBadRequest {
			t.Errorf("invalid body %s = %d: %s", body, w.Code, w.Body.String())
		}
	}
	if w := do(http.MethodPut, path, valid, "operator-test-secret", true); w.Code != http.StatusOK {
		t.Fatalf("approval = %d: %s", w.Code, w.Body.String())
	}
	w := do(http.MethodGet, path, "", "", true)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "operator-test-secret") {
		t.Fatalf("read must return approved policy without credentials: %d: %s", w.Code, w.Body.String())
	}
	policy, _, err := n.extensionPermissionSnapshot("demo")
	if err != nil || !policy.ManageAllowed || !policy.RunAllowed || policy.Settings["visible"] != true {
		t.Fatalf("saved policy = %+v, %v", policy, err)
	}
	if w := do(http.MethodPut, path, `{"manage_allowed":false,"run_allowed":false}`, "operator-test-secret", true); w.Code != http.StatusOK {
		t.Fatalf("revocation = %d", w.Code)
	}
	if err := n.extensionItemRuntimeAllowed("demo"); err == nil {
		t.Fatal("revocation must take effect without a restart")
	}
}

func TestExtensionManagementAPIUsesDelegationOrOperatorReentry(t *testing.T) {
	n := extensionPermissionFixture(t)
	server := NewAdminServer(n, "", credentialFromToken("operator-test-secret", "test"))
	do := func(approval string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/extensions/demo/install", nil)
		r.Header.Set("X-ND-Token", "operator-test-secret")
		r.Header.Set("X-ND-Operator-Token", approval)
		w := httptest.NewRecorder()
		server.routes().ServeHTTP(w, r)
		return w
	}
	if w := do(""); w.Code != http.StatusForbidden {
		t.Fatalf("ordinary signed-in UI cannot bypass delegation: %d", w.Code)
	}
	if w := do("operator-test-secret"); w.Code != http.StatusOK {
		t.Fatalf("operator may directly install an undelegated item: %d: %s", w.Code, w.Body.String())
	}
	state, err := loadExtensionState(extensionStatePath())
	if err != nil || state.Installed["demo"].Enabled {
		t.Fatal("installing must never grant run permission or enable the item")
	}
}

func TestModelExtensionEnableAndDeferredInstallRecheckApprovals(t *testing.T) {
	n := extensionPermissionFixture(t)
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{ManageAllowed: true})
	if result := n.installExtension("demo"); !result.Successful {
		t.Fatal(result.Message)
	}
	action := &IPCProxyAction{integration: n, spec: actionSpec{Name: CmdEnableExtension}}
	if _, result := action.Validate(json.RawMessage(`{"item_id":"demo"}`)); result.Successful {
		t.Fatal("delegated management cannot self-grant run permission")
	}
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{ManageAllowed: true, RunAllowed: true, Grants: map[string]bool{"input": true}})
	if _, result := action.Validate(json.RawMessage(`{"item_id":"demo"}`)); !result.Successful {
		t.Fatal(result.Message)
	}
	if result := n.uninstallExtension("demo"); !result.Successful {
		t.Fatal(result.Message)
	}
	install := &IPCProxyAction{integration: n, spec: actionSpec{Name: CmdInstallExtension}}
	work, result := install.Validate(json.RawMessage(`{"item_id":"demo"}`))
	if !result.Successful {
		t.Fatal(result.Message)
	}
	grantExtensionFixture(t, "demo", ExtensionPermissionPolicy{})
	install.Execute(work)
	state, err := loadExtensionState(extensionStatePath())
	if err != nil || len(state.Installed) != 0 {
		t.Fatalf("revoked queued install still ran: %+v, %v", state, err)
	}
}

func TestExtensionPolicyCorruptionFailsClosed(t *testing.T) {
	n := extensionPermissionFixture(t)
	if err := os.WriteFile(extensionPermissionsPath(), []byte(`{"version":999,"extensions":{"demo":{"manage_allowed":true,"run_allowed":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.checkExtensionManagement("demo"); err == nil {
		t.Fatal("corrupt policy must not permit management")
	}
	if err := n.extensionRuntimeAllowed("demo", nil); err == nil {
		t.Fatal("corrupt policy must not permit runtime")
	}
}

func TestLegacySignedEnabledMemoryMigrationNeverDelegatesManagement(t *testing.T) {
	dir := t.TempDir()
	key, indexPath, publishers := signedCatalogFixture(t, dir)
	writeJSONFile(t, indexPath, CatalogIndex{Items: []CatalogItem{{ID: "memory", Name: "Memory", Publisher: "acme", MCP: &CatalogMCP{Command: "npx"}}}})
	var output bytes.Buffer
	if code := catalogSign([]string{"--key", key, "--index", indexPath}, &output, &output); code != 0 {
		t.Fatal(output.String())
	}
	t.Setenv("NEURO_CATALOG_FILE", indexPath)
	t.Setenv("NEURO_PUBLISHERS_FILE", publishers)
	t.Setenv("NEURO_EXTENSIONS_STATE_FILE", filepath.Join(dir, "state.json"))
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", filepath.Join(dir, "permissions.json"))
	if err := saveExtensionState(extensionStatePath(), ExtensionStateFile{Installed: map[string]ExtensionInstallState{"memory": {Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	n := &NDIntegration{}
	policy, legacy, err := n.extensionPermissionSnapshot("memory")
	if err != nil || !legacy || !policy.RunAllowed || policy.ManageAllowed {
		t.Fatalf("migration = %+v, legacy=%v, err=%v", policy, legacy, err)
	}
	grantExtensionFixture(t, "memory", ExtensionPermissionPolicy{})
	policy, legacy, err = n.extensionPermissionSnapshot("memory")
	if err != nil || legacy || policy.RunAllowed {
		t.Fatalf("explicit revocation must override migration: %+v, %v, %v", policy, legacy, err)
	}
}

func TestExtensionSettingsAcceptOnlyReferencesForSecrets(t *testing.T) {
	for _, settings := range []map[string]interface{}{
		{"api_token": "plaintext"}, {"nested": map[string]interface{}{"password": "plaintext"}},
		{"token_env": "NEURO_ADMIN_TOKEN"}, {"token_env": "not a variable"},
	} {
		if err := validateExtensionSettings(settings, nil, 0); err == nil {
			t.Errorf("secret-like setting accepted: %+v", settings)
		}
	}
	if err := validateExtensionSettings(map[string]interface{}{"discord_token_env": "DISCORD_TOKEN"}, nil, 0); err != nil {
		t.Fatal(err)
	}
}

func TestMCPRuntimeRevocationBlocksAlreadyRegisteredTools(t *testing.T) {
	n := newMCPTestIntegration(t, `{"scopes":{"extensions":true}}`)
	action := attachFakeServer(t, n, "memory")
	if _, result := action.Validate(json.RawMessage(`{"text":"before"}`)); !result.Successful {
		t.Fatal(result.Message)
	}
	grantExtensionFixture(t, "memory", ExtensionPermissionPolicy{})
	if _, result := action.Validate(json.RawMessage(`{"text":"after"}`)); result.Successful || !strings.Contains(result.Message, "no permission to run") {
		t.Fatalf("already registered tool ignored revocation: %+v", result)
	}
}

func TestRunGitIgnoresInheritedGlobalAndInjectedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "global-config")
	if err := os.WriteFile(path, []byte("[ndtest]\n inherited = unsafe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "ndtest.injected")
	t.Setenv("GIT_CONFIG_VALUE_0", "unsafe")
	for _, name := range []string{"ndtest.inherited", "ndtest.injected"} {
		if output, err := runGit("config", "--get", name); err == nil || output != "" {
			t.Errorf("source installer inherited %s: %q, %v", name, output, err)
		}
	}
}

func TestMemoryPresentationPreservesSignedCatalog(t *testing.T) {
	dir := t.TempDir()
	key, path, publishersPath := signedCatalogFixture(t, dir)
	writeJSONFile(t, path, CatalogIndex{Items: []CatalogItem{{
		ID: "memory", Name: "Memory", Publisher: "acme",
		Description: "Its tools appear to Neuro once Vedal enables this extension.",
		LaunchHints: []string{"Installs disabled. Vedal turns it on in the Extensions page; Neuro cannot start it herself."},
	}}})
	var output bytes.Buffer
	if code := catalogSign([]string{"--key", key, "--index", path}, &output, &output); code != 0 {
		t.Fatal(output.String())
	}
	t.Setenv("NEURO_CATALOG_FILE", path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index, err := loadCatalogIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	pubs, err := loadPublishers(publishersPath)
	if err != nil {
		t.Fatal(err)
	}
	item := index.Items[0]
	view := catalogItemView(item, pubs)
	if view["signature_state"] != "verified" || strings.Contains(view["description"].(string), "Vedal") {
		t.Fatalf("operator presentation = %+v", view)
	}
	if !strings.Contains(view["description"].(string), "when you enable") {
		t.Fatal("operator wording must address the signed-in user")
	}
	model := (&NDIntegration{}).getCatalogItem("memory")
	if !model.Successful || strings.Contains(model.Message, "Neuro cannot start") || !strings.Contains(model.Message, "delegated to you") {
		t.Fatalf("model hint = %+v", model)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || verifyCatalogItem(item, pubs).State != "verified" || !strings.Contains(item.LaunchHints[0], "Neuro cannot start") {
		t.Fatal("presentation must preserve original signed bytes and item metadata")
	}
}
