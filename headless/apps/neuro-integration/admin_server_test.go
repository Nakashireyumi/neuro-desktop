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

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// testIntegration builds an integration with a real policy file and game
// registry, matching how the bridge is wired in production.
func testIntegration(t *testing.T) *NDIntegration {
	t.Helper()

	permissionsPath := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(permissionsPath, []byte(allowEverythingPolicy), 0644); err != nil {
		t.Fatalf("failed to write policy: %v", err)
	}

	profilesDir := t.TempDir()
	if err := writeProfileFixtures(profilesDir); err != nil {
		t.Fatalf("failed to write profiles: %v", err)
	}
	t.Setenv("NEURO_GAME_PROFILES_DIR", profilesDir)
	t.Setenv("NEURO_GAME_PROFILES_FILE", "")

	policy, err := loadPermissionPolicy(permissionsPath)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	return &NDIntegration{
		ipcFilePath:     filepath.Join(t.TempDir(), "ipc.json"),
		permissionsPath: permissionsPath,
		permissions:     policy,
		games:           newGameRuntime(loadGameRegistry()),
		relay:           newRelayState(RelayConfig{}),
		stats:           newBridgeStats(),
	}
}

// testDashboardToken is the token the admin tests sign in with. getJSON and
// sendJSON send it unless the test sets its own auth header. An empty
// X-ND-Token sends no token, which is how a test asks for an anonymous request.
const testDashboardToken = "dashboard-test-token-0123456789"

// newTestAdmin serves the admin routes on an httptest server (which is a
// loopback server). An empty token means the dashboard is not set up.
func newTestAdmin(t *testing.T, integration *NDIntegration, token string) *httptest.Server {
	t.Helper()
	cred := dashboardCredential{}
	if token != "" {
		cred = credentialFromToken(token, "env")
	}
	admin := NewAdminServer(integration, "127.0.0.1:0", cred)
	server := httptest.NewServer(admin.routes())
	t.Cleanup(server.Close)
	return server
}

// withDefaultToken copies headers and adds the test token when the caller has
// set neither X-ND-Token nor Authorization.
func withDefaultToken(headers map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range headers {
		out[name] = value
	}
	_, hasToken := out["X-ND-Token"]
	_, hasAuth := out["Authorization"]
	if !hasToken && !hasAuth {
		out["X-ND-Token"] = testDashboardToken
	}
	return out
}

func getJSON(t *testing.T, url string, headers map[string]string) (int, map[string]interface{}) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	for name, value := range withDefaultToken(headers) {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	var payload map[string]interface{}
	_ = json.NewDecoder(response.Body).Decode(&payload)
	return response.StatusCode, payload
}

func sendJSON(t *testing.T, method string, url string, body string, headers map[string]string) (int, map[string]interface{}) {
	t.Helper()

	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	// These helpers represent an explicitly approving operator. Tests for an
	// ordinary signed-in session set this header to an empty string instead.
	if _, explicit := headers["X-ND-Operator-Token"]; !explicit && (strings.Contains(url, "/api/permissions") || strings.Contains(url, "/api/permission-requests/")) {
		request.Header.Set("X-ND-Operator-Token", testDashboardToken)
	}
	for name, value := range withDefaultToken(headers) {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	var payload map[string]interface{}
	_ = json.NewDecoder(response.Body).Decode(&payload)
	return response.StatusCode, payload
}

func TestAdminStatusReportsBridgeState(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/status", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok, got %v", payload)
	}
	if version, _ := payload["version"].(string); version != Version {
		t.Fatalf("expected version %s, got %v", Version, payload["version"])
	}

	executor, _ := payload["executor"].(map[string]interface{})
	if connected, _ := executor["connected"].(bool); connected {
		t.Fatal("no executor is connected in this test")
	}

	games, _ := payload["game"].(map[string]interface{})
	if profiles, _ := games["profiles"].(float64); profiles < 1 {
		t.Fatalf("expected the game registry to be reported, got %v", games)
	}
}

func TestAdminPermissionSchemaCoversGameScope(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/permissions/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	scopes, _ := payload["scopes"].([]interface{})
	foundGame := false
	for _, raw := range scopes {
		if scope, ok := raw.(map[string]interface{}); ok {
			if scope["name"] == "game" {
				foundGame = true
			}
		}
	}
	if !foundGame {
		t.Fatal("the game scope should be offered to the dashboard")
	}

	actions, _ := payload["actions"].([]interface{})
	found := false
	for _, raw := range actions {
		entry, _ := raw.(map[string]interface{})
		if entry["name"] == "game_move" && entry["scope"] == "game" {
			found = true
		}
	}
	if !found {
		t.Fatal("game_move should be listed with the game scope")
	}
}

func TestAdminPermissionUpdateAppliesLive(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	policy := `{
	  "version": "1.0.0",
	  "default_allow": false,
	  "denied_actions": ["type_text"],
	  "scopes": {"input": false, "game": true, "vision": true}
	}`

	status, payload := sendJSON(t, http.MethodPut, server.URL+"/api/permissions", policy, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, payload)
	}
	if applied, _ := payload["applied_live"].(bool); !applied {
		t.Fatalf("expected the policy to be applied live, got %v", payload)
	}

	// The running bridge must use the new policy immediately.
	if integration.policy().IsAllowed("type_text") {
		t.Fatal("type_text is denied by the new policy")
	}
	if !integration.policy().IsAllowed("game_move") {
		t.Fatal("the game scope is allowed by the new policy")
	}

	// And the file on disk is the normalised policy.
	data, err := os.ReadFile(integration.permissionsPath)
	if err != nil {
		t.Fatalf("policy file missing: %v", err)
	}
	var reloaded permissionPolicyFile
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("saved policy is not valid JSON: %v", err)
	}
	if reloaded.DefaultAllow == nil || bool(*reloaded.DefaultAllow) {
		t.Fatalf("expected default_allow=false to be persisted, got %v", reloaded.DefaultAllow)
	}
}

func TestAdminRejectsBrokenPolicyWithoutBreakingTheBridge(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	before := integration.policy()

	status, _ := sendJSON(t, http.MethodPut, server.URL+"/api/permissions", `{"scopes": [`, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", status)
	}
	if integration.policy() != before {
		t.Fatal("a rejected policy must not change the running policy")
	}

	// A scope object of the wrong shape is also refused rather than silently
	// granting or dropping a capability.
	status, _ = sendJSON(t, http.MethodPut, server.URL+"/api/permissions",
		`{"scopes": {"game": {"allowed": "sometimes"}}}`, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed scope, got %d", status)
	}
}

func TestAdminTokenGuardsWrites(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, "letmein-long-enough")

	policy := `{"version": "1.0.0", "default_allow": true}`
	anonymous := map[string]string{"X-ND-Token": ""}

	// The same token opens reads and writes alike.
	if status, _ := getJSON(t, server.URL+"/api/status", map[string]string{"X-ND-Token": "letmein-long-enough"}); status != http.StatusOK {
		t.Fatalf("expected status to be readable with the token, got %d", status)
	}

	status, _ := sendJSON(t, http.MethodPut, server.URL+"/api/permissions", policy, anonymous)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", status)
	}

	// Bearer is accepted as well.
	status, _ = sendJSON(t, http.MethodPut, server.URL+"/api/permissions", policy,
		map[string]string{"Authorization": "Bearer letmein-long-enough", "X-ND-Operator-Token": "letmein-long-enough"})
	if status != http.StatusOK {
		t.Fatalf("expected the token to be accepted, got %d", status)
	}

	// X-ND-Token is the header the dashboard sends.
	status, _ = sendJSON(t, http.MethodPost, server.URL+"/api/extensions/some-extension/enable", "",
		map[string]string{"X-ND-Token": "letmein-long-enough"})
	if status == http.StatusUnauthorized {
		t.Fatal("the X-ND-Token header should be accepted")
	}

	// A wrong token is rejected just like a missing one.
	status, _ = sendJSON(t, http.MethodPost, server.URL+"/api/extensions/some-extension/enable", "",
		map[string]string{"X-ND-Token": "nope"})
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong token, got %d", status)
	}
}

// Loopback is not a credential. Every guarded route refuses an anonymous caller
// even when the request comes from 127.0.0.1, which is where the httptest
// server (and a preview proxy) appear to come from.
func TestAdminDoesNotTrustLoopback(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)
	anonymous := map[string]string{"X-ND-Token": ""}

	for _, path := range []string{
		"/api/session", "/api/status", "/api/runtime", "/api/config", "/api/actions",
		"/api/catalog", "/api/permissions", "/api/permissions/schema", "/api/extensions",
		"/api/games", "/api/relay", "/api/audit",
	} {
		status, _ := getJSON(t, server.URL+path, anonymous)
		if status != http.StatusUnauthorized {
			t.Errorf("%s answered %d to an anonymous loopback caller, want 401", path, status)
		}
	}
}

func TestAdminHealthStaysPublic(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/health should stay public, got %d", response.StatusCode)
	}
}

// Before setup there is no token to check, so the API refuses everything rather
// than falling back to open access.
func TestAdminWithoutATokenRefusesEverything(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, "")

	if status, _ := getJSON(t, server.URL+"/api/status", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("an unconfigured dashboard should answer 503, got %d", status)
	}
	if status, _ := sendJSON(t, http.MethodPut, server.URL+"/api/permissions", `{"default_allow": true}`, nil); status != http.StatusServiceUnavailable {
		t.Fatalf("an unconfigured dashboard should refuse writes with 503, got %d", status)
	}
}

// When the token cannot be loaded, the 503 says why, so the sign-in page can
// tell the operator what to fix.
func TestAdminSaysWhyTheTokenIsUnusable(t *testing.T) {
	integration := testIntegration(t)
	admin := NewAdminServer(integration, "127.0.0.1:0", dashboardCredential{})
	admin.credentialProblem = "NEURO_ADMIN_TOKEN does not match the dashboard token in ./dashboard-token"
	server := httptest.NewServer(admin.routes())
	t.Cleanup(server.Close)

	status, payload := getJSON(t, server.URL+"/api/status", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", status)
	}
	if message, _ := payload["error"].(string); !strings.Contains(message, "does not match") {
		t.Fatalf("the 503 should carry the reason, got %v", payload)
	}
}

// A token in the query string would end up in logs and history, so it is not read.
func TestAdminIgnoresTokenInURL(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, _ := getJSON(t, server.URL+"/api/status?token="+testDashboardToken, map[string]string{"X-ND-Token": ""})
	if status != http.StatusUnauthorized {
		t.Fatalf("a token in the URL must not be accepted, got %d", status)
	}
}

// The shell is served to anyone who asks, so it must never carry the secret.
func TestAdminShellCarriesNoToken(t *testing.T) {
	integration := testIntegration(t)
	uiDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<html><head></head><body>dashboard</body></html>"), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	t.Setenv("NEURO_UI_DIR", uiDir)
	server := newTestAdmin(t, integration, testDashboardToken)

	response, err := http.Get(server.URL + "/ui/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(response.Body)
	if strings.Contains(body.String(), testDashboardToken) || strings.Contains(body.String(), "__ND_BOOTSTRAP") {
		t.Fatalf("the shell must not carry a token: %q", body.String())
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("the shell must not be cached, got Cache-Control %q", got)
	}
}

func TestAdminSessionChecksTheToken(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, "a-dashboard-token-0001")

	status, payload := getJSON(t, server.URL+"/api/session", map[string]string{"Authorization": "Bearer a-dashboard-token-0001"})
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("a good token should open the session, got %d %v", status, payload)
	}
	if source, _ := payload["token_source"].(string); source != "env" {
		t.Fatalf("expected token_source env, got %v", payload["token_source"])
	}
	if status, _ := getJSON(t, server.URL+"/api/session", map[string]string{"Authorization": "Bearer wrong-token-000000"}); status != http.StatusUnauthorized {
		t.Fatalf("a wrong token must be refused, got %d", status)
	}
}

func TestAdminExtensionManagement(t *testing.T) {
	integration := testIntegration(t)
	installFixture(t, `[{"id":"nd-vision-server","name":"Vision"}]`)
	integration.policy().scopes[ScopeExtensions] = ScopeConfig{Allowed: true}

	statePath := filepath.Join(t.TempDir(), "extensions-state.json")
	state := `{"installed": {"nd-vision-server": {"enabled": false, "installed_at": "2026-01-01T00:00:00Z", "source": "test"}}}`
	if err := os.WriteFile(statePath, []byte(state), 0644); err != nil {
		t.Fatalf("failed to write extension state: %v", err)
	}
	t.Setenv("NEURO_EXTENSIONS_STATE_FILE", statePath)
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", statePath+".permissions")
	grantExtensionFixture(t, "nd-vision-server", ExtensionPermissionPolicy{RunAllowed: true})
	approval := map[string]string{"X-ND-Operator-Token": testDashboardToken}

	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/extensions", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	installed, _ := payload["installed"].([]interface{})
	if len(installed) != 1 {
		t.Fatalf("expected one installed extension, got %v", payload["installed"])
	}

	status, payload = sendJSON(t, http.MethodPost, server.URL+"/api/extensions/nd-vision-server/enable", "", approval)
	if status != http.StatusOK {
		t.Fatalf("enable failed: %d %v", status, payload)
	}

	reloaded, err := loadExtensionState(statePath)
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}
	if !reloaded.Installed["nd-vision-server"].Enabled {
		t.Fatal("the extension should be enabled in the state file")
	}

	status, _ = sendJSON(t, http.MethodPost, server.URL+"/api/extensions/nd-vision-server/uninstall", "", approval)
	if status != http.StatusOK {
		t.Fatalf("uninstall failed: %d", status)
	}
	reloaded, _ = loadExtensionState(statePath)
	if _, stillThere := reloaded.Installed["nd-vision-server"]; stillThere {
		t.Fatal("the extension should be gone after uninstall")
	}

	// Unknown actions are refused, not silently ignored.
	status, _ = sendJSON(t, http.MethodPost, server.URL+"/api/extensions/nd-vision-server/frobnicate", "", approval)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown action, got %d", status)
	}
}

func TestAdminGamesPanel(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/games", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	profiles, _ := payload["profiles"].([]interface{})
	if len(profiles) != 2 {
		t.Fatalf("expected the two fixture profiles, got %v", payload["profiles"])
	}

	found := map[string]bool{}
	for _, raw := range profiles {
		entry, _ := raw.(map[string]interface{})
		name, _ := entry["id"].(string)
		found[name] = true
		if entry["keys"] == nil {
			t.Fatalf("profile %s should expose its keybinds to the UI", name)
		}
	}
	if !found["minecraft"] || !found["stardew-valley"] {
		t.Fatalf("expected minecraft and stardew-valley, got %v", found)
	}
}

func TestAdminServesDashboardAssets(t *testing.T) {
	integration := testIntegration(t)
	uiDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<html>dashboard</html>"), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	t.Setenv("NEURO_UI_DIR", uiDir)

	server := newTestAdmin(t, integration, testDashboardToken)

	response, err := http.Get(server.URL + "/ui/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	buffer := new(bytes.Buffer)
	_, _ = buffer.ReadFrom(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if !strings.Contains(buffer.String(), "dashboard") {
		t.Fatalf("expected the dashboard shell, got %q", buffer.String())
	}

	// A deep link falls back to the shell (single page app routing).
	response, err = http.Get(server.URL + "/ui/games/minecraft")
	if err != nil {
		t.Fatalf("deep link request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("deep links should serve the shell, got %d", response.StatusCode)
	}
}

func TestAdminReleaseWithoutExecutorReportsFailure(t *testing.T) {
	// With no executor attached the bridge waits for the file IPC timeout
	// before giving up; shrink it so the test stays fast.
	t.Setenv("NEURO_IPC_TIMEOUT_SECONDS", "1")

	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := sendJSON(t, http.MethodPost, server.URL+"/api/games/release", "", nil)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 when nothing is attached, got %d (%v)", status, payload)
	}
	if message, _ := payload["message"].(string); !strings.Contains(message, "release input") {
		t.Fatalf("expected a readable failure, got %v", payload)
	}
}

func TestAdminRelayStatusExposed(t *testing.T) {
	integration := testIntegration(t)
	integration.relay.notePeer("minecraft-integration", "integration")
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/relay", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	relay, _ := payload["relay"].(map[string]interface{})
	if peers, _ := relay["peer_count"].(float64); peers != 1 {
		t.Fatalf("expected one relay peer, got %v", relay)
	}
}

func TestWrapExecutionResult(t *testing.T) {
	wrapped := wrapExecutionResult(neuro.NewSuccessResult("done"))
	if !wrapped.OK || wrapped.Message != "done" {
		t.Fatalf("unexpected wrap: %+v", wrapped)
	}
	wrapped = wrapExecutionResult(neuro.NewFailureResult("nope"))
	if wrapped.OK {
		t.Fatalf("failures should not be reported as ok: %+v", wrapped)
	}
}

// TestAllActionSpecsCoversEverySurface keeps the dashboard in step with what
// the bridge actually registers: the shell, the guide and the game interface
// are all grantable, so all three must be listed.
func TestAllActionSpecsCoversEverySurface(t *testing.T) {
	names := map[string]bool{}
	for _, spec := range allActionSpecs() {
		names[string(spec.Name)] = true
	}

	for _, want := range []string{
		string(CmdShellCommand),
		string(CmdDesktopGuide),
		string(CmdGameMove),
		string(CmdMouseMove),
		string(CmdSendDesktopContext),
	} {
		if !names[want] {
			t.Fatalf("allActionSpecs() is missing %q (dashboard would hide it)", want)
		}
	}

	// Every listed action must map to a known scope, otherwise the dashboard
	// shows "unknown" and the operator cannot reason about the gate.
	for _, spec := range allActionSpecs() {
		scope := actionScopeName(string(spec.Name))
		if scope == "unknown" {
			t.Fatalf("action %q has no permission scope", spec.Name)
		}
	}
}

// TestScopeRequiresExplicitConsent pins the dangerous scopes.
func TestScopeRequiresExplicitConsent(t *testing.T) {
	for _, scope := range []PermissionScope{ScopeShell, ScopeSystem} {
		if !scopeRequiresExplicitConsent(scope) {
			t.Fatalf("%s must require explicit consent", scope)
		}
	}
	for _, scope := range []PermissionScope{ScopeInput, ScopeGame, ScopeVision, ScopeProcess, ScopeNetwork, ScopeFilesystem} {
		if scopeRequiresExplicitConsent(scope) {
			t.Fatalf("%s should be toggleable without being named in the policy", scope)
		}
	}
}

// A weak model cannot guess parameter names. /api/actions must therefore expose
// the schema and a plain-language rendering of it, and every action must be
// described well enough to be used without extra prompting.
func TestActionsExposeLLMFriendlyMetadata(t *testing.T) {
	integration := testIntegration(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	status, payload := getJSON(t, server.URL+"/api/actions", nil)
	if status != http.StatusOK {
		t.Fatalf("/api/actions = %d, want 200", status)
	}

	rawActions, _ := payload["actions"].([]interface{})
	if len(rawActions) < 30 {
		t.Fatalf("expected the full action catalogue, got %d entries", len(rawActions))
	}

	seenGameMove := false
	for _, raw := range rawActions {
		action, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := action["name"].(string)
		description, _ := action["description"].(string)
		params, _ := action["params"].(string)
		schema, _ := action["schema"].(map[string]interface{})
		required := stringSlice(action["required"])

		if len(strings.TrimSpace(description)) < 20 {
			t.Errorf("%s has an unusable description (%q)", name, description)
		}
		if strings.TrimSpace(params) == "" {
			t.Errorf("%s does not describe its parameters", name)
		}
		if params == "none" {
			if len(schema) != 0 {
				t.Errorf("%s takes no parameters but ships a schema", name)
			}
			continue
		}
		if len(schema) == 0 {
			t.Errorf("%s has parameters but no schema", name)
		}
		if strings.Contains(params, "required") && len(required) == 0 {
			t.Errorf("%s mentions a required parameter but lists none", name)
		}

		if name == "game_move" {
			seenGameMove = true
			for _, want := range []string{"direction", "seconds", "steps", "right"} {
				if !strings.Contains(params, want) {
					t.Errorf("game_move params should mention %q, got %q", want, params)
				}
			}
			if len(required) != 1 || required[0] != "direction" {
				t.Errorf("game_move required = %v, want [direction]", required)
			}
		}
	}
	if !seenGameMove {
		t.Fatal("game_move is missing from /api/actions")
	}
}

// stringSlice reads a JSON array of strings regardless of how it was typed.
func stringSlice(raw interface{}) []string {
	switch value := raw.(type) {
	case []string:
		return value
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
