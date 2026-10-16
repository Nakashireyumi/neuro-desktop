package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func delegatedTestAdmin(t *testing.T) (*AdminServer, *httptest.Server) {
	t.Helper()
	n := testIntegration(t)
	n.stop = newStopSwitch()
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", filepath.Join(t.TempDir(), "extension-permissions.json"))
	a := NewAdminServer(n, "127.0.0.1:0", credentialFromToken(testDashboardToken, "test"))
	server := httptest.NewServer(a.routes())
	t.Cleanup(server.Close)
	return a, server
}

func issueDelegatedTestToken(t *testing.T, server *httptest.Server, body string) string {
	t.Helper()
	status, payload := sendJSON(t, http.MethodPost, server.URL+"/api/delegation/token", body, map[string]string{"X-ND-Operator-Token": testDashboardToken})
	if status != http.StatusOK {
		t.Fatalf("mint: got %d %v", status, payload)
	}
	token, _ := payload["token"].(string)
	if !strings.HasPrefix(token, "ndd_") || len(token) < 40 {
		t.Fatalf("unexpected credential format")
	}
	if payload["role"] != "neuro" {
		t.Fatalf("expected restricted role: %v", payload)
	}
	return token
}

func TestDelegatedManagerTokenRequiresOperatorReentryAndHasBoundedLifetime(t *testing.T) {
	a, server := delegatedTestAdmin(t)
	for _, headers := range []map[string]string{nil, {"X-ND-Operator-Token": "wrong-secret"}} {
		status, _ := sendJSON(t, http.MethodPost, server.URL+"/api/delegation/token", `{}`, headers)
		if status != http.StatusForbidden {
			t.Fatalf("mint without fresh operator secret: got %d", status)
		}
	}
	for _, body := range []string{`{"minutes":-1}`, `{"minutes":1441}`, `{"minutes":1.5}`, `{"persona":"operator"}`, `{"unknown":true}`, `{} {}`} {
		status, _ := sendJSON(t, http.MethodPost, server.URL+"/api/delegation/token", body, map[string]string{"X-ND-Operator-Token": testDashboardToken})
		if status != http.StatusBadRequest {
			t.Fatalf("mint %s: got %d", body, status)
		}
	}
	token := issueDelegatedTestToken(t, server, `{"persona":"evil"}`)
	valid, expires, persona := a.delegation.session(token)
	if !valid || persona != "evil" || time.Until(expires) > time.Hour || time.Until(expires) < 59*time.Minute {
		t.Fatalf("unexpected default session expiry/persona")
	}
	status, payload := getJSON(t, server.URL+"/api/delegation/session", map[string]string{"X-ND-Token": token})
	if status != http.StatusOK || payload["role"] != "neuro" || payload["persona"] != "evil" || payload["can_edit_permissions"] != false || payload["can_resume"] != false {
		t.Fatalf("unexpected delegated session metadata: %d %v", status, payload)
	}
	if _, exists := payload["token"]; exists {
		t.Fatal("session introspection must never return the bearer token")
	}
	status, payload = getJSON(t, server.URL+"/api/session", map[string]string{"X-ND-Token": token})
	if status != http.StatusOK || payload["role"] != "neuro" || payload["expires_at"] == "" {
		t.Fatalf("normal manager session must expose the delegated role: %d %v", status, payload)
	}
}

func TestDelegatedManagerRotationExpiryAndRevocationReturnUnauthorized(t *testing.T) {
	a, server := delegatedTestAdmin(t)
	first := issueDelegatedTestToken(t, server, `{"minutes":1440}`)
	second := issueDelegatedTestToken(t, server, `{"minutes":1}`)
	if first == second {
		t.Fatal("mint must use fresh randomness")
	}
	status, _ := getJSON(t, server.URL+"/api/session", map[string]string{"X-ND-Token": first})
	if status != http.StatusUnauthorized {
		t.Fatalf("rotated credential: got %d", status)
	}
	a.delegation.mu.Lock()
	a.delegation.expiresAt = time.Now().Add(-time.Second)
	a.delegation.mu.Unlock()
	status, _ = getJSON(t, server.URL+"/api/session", map[string]string{"X-ND-Token": second})
	if status != http.StatusUnauthorized {
		t.Fatalf("expired credential: got %d", status)
	}
	third := issueDelegatedTestToken(t, server, `{}`)
	status, _ = sendJSON(t, http.MethodDelete, server.URL+"/api/delegation/token", "", nil)
	if status != http.StatusForbidden {
		t.Fatalf("revoke without operator reentry: got %d", status)
	}
	status, _ = sendJSON(t, http.MethodDelete, server.URL+"/api/delegation/token", "", map[string]string{"X-ND-Operator-Token": testDashboardToken})
	if status != http.StatusOK {
		t.Fatalf("revoke: got %d", status)
	}
	status, _ = getJSON(t, server.URL+"/api/session", map[string]string{"X-ND-Token": third})
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked credential: got %d", status)
	}
}

func TestDelegatedManagerCannotGrantResumeMintOrReadUnknownRoutes(t *testing.T) {
	a, server := delegatedTestAdmin(t)
	token := issueDelegatedTestToken(t, server, `{}`)
	headers := map[string]string{"X-ND-Token": token, "X-ND-Operator-Token": testDashboardToken, "Authorization": "Bearer " + testDashboardToken}
	policyBefore, err := os.ReadFile(a.integration.permissionsPath)
	if err != nil {
		t.Fatal(err)
	}
	a.integration.stop.setPaused(true)
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/permissions"},
		{http.MethodPost, "/api/permission-requests/example/approve"},
		{http.MethodPost, "/api/permissions/grants/extensions/revoke"},
		{http.MethodPut, "/api/extensions/memory/permissions"},
		{http.MethodGet, "/api/extensions/memory/permissions"},
		{http.MethodPost, "/api/delegation/token"},
		{http.MethodDelete, "/api/delegation/token"},
		{http.MethodPost, "/api/control/resume"},
		{http.MethodGet, "/api/audit"},
		{http.MethodGet, "/api/runtime"},
		{http.MethodGet, "/api/not-yet-defined"},
	} {
		status, _ := sendJSON(t, route.method, server.URL+route.path, `{}`, headers)
		if status != http.StatusForbidden && route.path != "/api/not-yet-defined" {
			t.Fatalf("%s %s: got %d", route.method, route.path, status)
		}
	}
	if !a.integration.stop.pausedNow() {
		t.Fatal("delegated session resumed operator pause")
	}
	policyAfter, err := os.ReadFile(a.integration.permissionsPath)
	if err != nil || string(policyBefore) != string(policyAfter) {
		t.Fatal("delegated session changed operator policy")
	}
	if valid, _, _ := a.delegation.session(token); !valid {
		t.Fatal("denied token-management route must not rotate or revoke the token")
	}
}

func TestDelegatedManagerExtensionMutationsRespectGlobalAndPerItemGrants(t *testing.T) {
	a, _ := delegatedTestAdmin(t)
	token, _, err := a.delegation.issue(60, "neuro")
	if err != nil {
		t.Fatal(err)
	}
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true}
	a.integration.setPolicy(policy)
	file := extensionPermissionFile{Version: 1, Extensions: map[string]ExtensionPermissionPolicy{
		"memory": {ManageAllowed: true, Grants: map[string]bool{}, Settings: map[string]interface{}{}},
	}}
	if err := saveExtensionPermissions(extensionPermissionsPath(), file); err != nil {
		t.Fatal(err)
	}
	handler := a.guard(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, map[string]interface{}{"ok": true}) })
	check := func(method, path, body string, expected int) {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("X-ND-Token", token)
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != expected {
			t.Fatalf("%s %s: got %d, want %d (%s)", method, path, response.Code, expected, response.Body.String())
		}
	}
	for _, operation := range []string{"install", "enable", "disable", "uninstall"} {
		check(http.MethodPost, "/api/extensions/memory/"+operation, "", http.StatusOK)
	}
	check(http.MethodPost, "/api/marketplace/memory/stage", "", http.StatusOK)
	check(http.MethodPost, "/api/extensions/other/enable", "", http.StatusForbidden)
	check(http.MethodPost, "/api/extensions/memory/permissions", `{}`, http.StatusForbidden)
	check(http.MethodPost, "/api/extensions/memory/enable/extra", "", http.StatusForbidden)
	check(http.MethodGet, "/api/future-operator-only", "", http.StatusForbidden)
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: false}
	check(http.MethodPost, "/api/extensions/memory/enable", "", http.StatusForbidden)
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true}
	policy.denied[string(CmdEnableExtension)] = struct{}{}
	check(http.MethodPost, "/api/extensions/memory/enable", "", http.StatusForbidden)
	delete(policy.denied, string(CmdEnableExtension))
	policy.denied[string(CmdStageIntegration)] = struct{}{}
	check(http.MethodPost, "/api/marketplace/memory/stage", "", http.StatusForbidden)
	delete(policy.denied, string(CmdStageIntegration))
	a.integration.stop.setPaused(true)
	check(http.MethodPost, "/api/extensions/memory/enable", "", http.StatusForbidden)
	check(http.MethodPost, "/api/games/release", "", http.StatusOK)
	check(http.MethodPost, "/api/input/release", "", http.StatusOK)
	check(http.MethodPost, "/api/control/pause", "", http.StatusOK)
	check(http.MethodPost, "/api/control/resume", "", http.StatusForbidden)
	a.integration.stop.setPaused(false)
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true, Limits: ScopeLimits{MaxActionsPerMinute: 1}}
	check(http.MethodPost, "/api/extensions/memory/enable", "", http.StatusOK)
	check(http.MethodPost, "/api/extensions/memory/enable", "", http.StatusForbidden)
}

func TestDelegatedManagerGameLaunchAndVisionNeedTheirOwnScopes(t *testing.T) {
	a, _ := delegatedTestAdmin(t)
	token, _, err := a.delegation.issue(60, "neuro")
	if err != nil {
		t.Fatal(err)
	}
	policy := defaultPermissionPolicy()
	a.integration.setPolicy(policy)
	var receivedBody string
	handler := a.guard(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedBody, _ = body["profile_id"].(string)
		writeJSON(w, map[string]interface{}{"ok": true})
	})
	check := func(method, path, body string, expected int) {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != expected {
			t.Fatalf("%s %s: got %d want %d (%s)", method, path, response.Code, expected, response.Body.String())
		}
	}
	check(http.MethodPost, "/api/games/session", `{"profile_id":"minecraft","launch":false}`, http.StatusOK)
	if receivedBody != "minecraft" {
		t.Fatal("authorization did not restore game session body")
	}
	check(http.MethodPost, "/api/games/session", `{"profile_id":"minecraft","launch":true}`, http.StatusForbidden)
	policy.scopes[ScopeSystem] = ScopeConfig{Allowed: true}
	check(http.MethodPost, "/api/games/session", `{"profile_id":"minecraft","launch":true}`, http.StatusOK)
	policy.scopes[ScopeVision] = ScopeConfig{Allowed: false}
	check(http.MethodPost, "/api/games/observe", `{}`, http.StatusForbidden)
	policy.scopes[ScopeGame] = ScopeConfig{Allowed: false}
	check(http.MethodPost, "/api/games/session", `{}`, http.StatusForbidden)
	check(http.MethodDelete, "/api/games/session", ``, http.StatusForbidden)
	request := httptest.NewRequest(http.MethodGet, "/api/session?token="+token, nil)
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("query token must not authenticate: got %d", response.Code)
	}
}

func TestDelegatedManagerAuditDoesNotContainCredentials(t *testing.T) {
	a, server := delegatedTestAdmin(t)
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a.integration.audit = &auditor{path: path}
	token := issueDelegatedTestToken(t, server, `{}`)
	_, _ = sendJSON(t, http.MethodPost, server.URL+"/api/control/resume?token=query-secret", "", map[string]string{"X-ND-Token": token})
	_, _ = sendJSON(t, http.MethodDelete, server.URL+"/api/delegation/token", "", map[string]string{"X-ND-Operator-Token": testDashboardToken})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, testDashboardToken, "query-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("audit contains credential data")
		}
	}
	if !strings.Contains(string(data), `"decision":"issued"`) || !strings.Contains(string(data), `"decision":"denied"`) || !strings.Contains(string(data), `"decision":"revoked"`) {
		t.Fatalf("missing lifecycle audit decisions: %s", data)
	}
}

func TestDelegatedManagerStoreConcurrentRotationAndRevoke(t *testing.T) {
	var store delegatedManagerStore
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				token, _, err := store.issue(1, "neuro")
				if err != nil {
					t.Errorf("mint: %v", err)
					return
				}
				store.session(token)
				store.revoke()
			}
		}()
	}
	workers.Wait()
	store.revoke()
	last, _, err := store.issue(1, "evil")
	if err != nil {
		t.Fatal(err)
	}
	if valid, _, persona := store.session(last); !valid || persona != "evil" {
		t.Fatal("final session was not installed atomically")
	}
	store.revoke()
	if valid, _, _ := store.session(last); valid {
		t.Fatal("revoked concurrent session remains valid")
	}
}

func TestDelegatedManagerAdmittedRequestNeverBecomesOperatorWhenRevoked(t *testing.T) {
	a, _ := delegatedTestAdmin(t)
	token, _, err := a.delegation.issue(1, "neuro")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.Header.Set("X-ND-Token", token)
	response := httptest.NewRecorder()
	a.guard(func(w http.ResponseWriter, r *http.Request) {
		a.delegation.revoke()
		role, expires := a.managerSessionRole(r)
		if role != "neuro" || expires == "" {
			t.Errorf("revoking an admitted request changed its authority to %s", role)
		}
		writeJSON(w, map[string]interface{}{"ok": true})
	})(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admitted request: got %d", response.Code)
	}
}
