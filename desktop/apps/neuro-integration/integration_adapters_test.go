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
	"time"
)

func adapterFixture(t *testing.T) (*NDIntegration, *AdminServer, IntegrationManifest) {
	t.Helper()
	n := extensionPermissionFixture(t)
	t.Setenv("NEURO_INTEGRATIONS_FILE", filepath.Join(t.TempDir(), "integrations.json"))
	a := NewAdminServer(n, "", credentialFromToken(testDashboardToken, "test"))
	manifest := IntegrationManifest{SchemaVersion: 1, ID: "vencord-discord", Name: "Discord activity", Version: "0.1.0", Kind: "discord", GameIDs: []string{"minecraft"}, Permissions: []ExtensionPermissionDeclaration{{ID: "discord.viewer", Description: "Show curated activity to viewers", Required: false}}}
	code, _ := adapterRequest(t, a, http.MethodPut, "/api/integrations/"+manifest.ID, map[string]interface{}{"manifest": manifest}, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != 200 {
		t.Fatalf("operator manifest setup: %d", code)
	}
	grantExtensionFixture(t, manifest.ID, ExtensionPermissionPolicy{ManageAllowed: true, RunAllowed: true, Grants: map[string]bool{"discord.viewer": true}})
	return n, a, manifest
}
func adapterRequest(t *testing.T, a *AdminServer, method, path string, body interface{}, headers map[string]string) (int, map[string]interface{}) {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unexpected API response: %d %s", w.Code, w.Body.String())
	}
	return w.Code, result
}
func adapterCredential(t *testing.T, a *AdminServer, id, kind string) string {
	t.Helper()
	code, body := adapterRequest(t, a, "POST", "/api/integrations/"+id+"/token", map[string]string{"kind": kind}, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != 200 {
		t.Fatalf("token creation failed: %d %v", code, body)
	}
	return body["token"].(string)
}
func TestIntegrationCredentialsSeparateAdminPublisherAndViewer(t *testing.T) {
	n, a, m := adapterFixture(t)
	code, _ := adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/token", map[string]string{"kind": "integration"}, map[string]string{"X-ND-Token": testDashboardToken})
	if code != 403 {
		t.Fatal("minting a scoped credential needs fresh operator approval")
	}
	token := adapterCredential(t, a, m.ID, "integration")
	viewer := adapterCredential(t, a, m.ID, "viewer")
	for _, secret := range []string{token, viewer} {
		code, _ := adapterRequest(t, a, "GET", "/api/status", nil, map[string]string{"X-ND-Token": secret})
		if code != 401 {
			t.Fatal("scoped tokens must not enter the dashboard")
		}
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, map[string]string{"X-ND-Integration-Token": viewer})
	if code != 401 {
		t.Fatal("viewer cannot publish")
	}
	code, _ = adapterRequest(t, a, "GET", "/api/viewer/"+m.ID, nil, map[string]string{"X-ND-Viewer-Token": token})
	if code != 401 {
		t.Fatal("publisher is not a viewer token")
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/other/heartbeat", map[string]string{"state": "ready"}, map[string]string{"X-ND-Integration-Token": token})
	if code != 401 {
		t.Fatal("credential must be bound to one integration")
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, map[string]string{"X-ND-Integration-Token": token, "Origin": "https://example.com"})
	if code != 403 {
		t.Fatal("publisher is a native adapter, not a web credential")
	}
	data, _ := os.ReadFile(adapterConfigPath())
	for _, secret := range []string{token, viewer} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("runtime configuration leaked plaintext token")
		}
	}
	_ = n
}
func TestIntegrationGameOwnershipVisibilityConsentAndRevocation(t *testing.T) {
	n, a, m := adapterFixture(t)
	token := adapterCredential(t, a, m.ID, "integration")
	viewer := adapterCredential(t, a, m.ID, "viewer")
	publish := map[string]string{"X-ND-Integration-Token": token}
	read := map[string]string{"X-ND-Viewer-Token": viewer}
	code, _ := adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, publish)
	if code != 200 {
		t.Fatal("approved integration must connect")
	}
	profile := &GameProfile{ID: "minecraft", Control: GameControl{Mode: ControlModeAuto}}
	if n.resolveAutoMode(profile) != ControlModeExternal {
		t.Fatal("a live SDK adapter must own its declared game")
	}
	event := map[string]string{"actor": "neuro", "kind": "discord.message", "summary": "Curated viewer-safe activity"}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/activity", event, publish)
	if code != 403 {
		t.Fatal("activity starts hidden")
	}
	if err := n.setViewerVisibility(m.ID, "neuro", true, 15); err != nil {
		t.Fatal(err)
	}
	event["actor"] = "evil"
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/activity", event, publish)
	if code != 403 {
		t.Fatal("Neuro's visibility must not expose Evil's events")
	}
	event["actor"] = "neuro"
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/activity", event, publish)
	if code != 200 {
		t.Fatal("consented curated event should publish")
	}
	code, body := adapterRequest(t, a, "GET", "/api/viewer/"+m.ID, nil, read)
	if code != 200 || body["visible"] != true || len(body["items"].([]interface{})) != 1 {
		t.Fatalf("visible feed: %v", body)
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/activity", event, publish)
	if code != 429 {
		t.Fatal("activity floods must be bounded")
	}
	grantExtensionFixture(t, m.ID, ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{}})
	_, body = adapterRequest(t, a, "GET", "/api/viewer/"+m.ID, nil, read)
	if body["visible"] != false || len(body["items"].([]interface{})) != 0 {
		t.Fatal("revocation must immediately hide and clear viewer history")
	}
	grantExtensionFixture(t, m.ID, ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{"discord.viewer": true}})
	_, body = adapterRequest(t, a, "GET", "/api/viewer/"+m.ID, nil, read)
	if body["visible"] != false {
		t.Fatal("restoring a grant must not restore expired visibility")
	}
	if err := n.setViewerVisibility(m.ID, "evil", true, 15); err != nil {
		t.Fatal(err)
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/hide", nil, publish)
	if code != 200 {
		t.Fatal("scoped sender must always be able to stop sharing")
	}
	rt := n.adapterRuntime()
	rt.mu.Lock()
	rt.presence[m.ID].LastSeen = time.Now().Add(-adapterLease - time.Second)
	rt.mu.Unlock()
	if n.resolveAutoMode(profile) != ControlModeND {
		t.Fatal("an expired lease must release dedicated-game ownership")
	}
	if err := n.setViewerVisibility(m.ID, "neuro", true, 15); err == nil {
		t.Fatal("offline sources cannot become visible")
	}
}
func TestIntegrationInputBoundsAndPauseGate(t *testing.T) {
	n, a, m := adapterFixture(t)
	token := adapterCredential(t, a, m.ID, "integration")
	headers := map[string]string{"X-ND-Integration-Token": token}
	for _, body := range []map[string]string{{"state": "unknown"}, {"state": "ready", "message": "bad\nmessage"}, {"state": "ready", "message": strings.Repeat("a", 501)}} {
		code, _ := adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", body, headers)
		if code != 400 {
			t.Fatalf("invalid heartbeat admitted: %v", body)
		}
	}
	r := httptest.NewRequest("POST", "/api/integrations/"+m.ID+"/heartbeat", strings.NewReader(strings.Repeat(" ", 65537)))
	r.Header.Set("X-ND-Integration-Token", token)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("oversized JSON should fail")
	}
	n.stop.setPaused(true)
	code, _ := adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, headers)
	if code != 409 {
		t.Fatal("pause must apply to SDK runtime updates")
	}
	code, _ = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/hide", nil, headers)
	if code != 200 {
		t.Fatal("pause must not prevent opting out")
	}
}

func TestScopedIntegrationConfigurationUsesEffectiveOptionalGrants(t *testing.T) {
	n, a, m := adapterFixture(t)
	m.Permissions = append(m.Permissions, ExtensionPermissionDeclaration{ID: "shell", Description: "Optional terminal feature", Required: false})
	m.SettingsSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"enabled": map[string]interface{}{"type": "boolean"}}}
	code, _ := adapterRequest(t, a, "PUT", "/api/integrations/"+m.ID, map[string]interface{}{"manifest": m}, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != 200 {
		t.Fatal("manifest update failed")
	}
	grantExtensionFixture(t, m.ID, ExtensionPermissionPolicy{RunAllowed: true, Grants: map[string]bool{"shell": true, "discord.viewer": true}, Settings: map[string]interface{}{"enabled": false}})
	n.policy().scopes[ScopeShell] = ScopeConfig{Allowed: false}
	token := adapterCredential(t, a, m.ID, "integration")
	headers := map[string]string{"X-ND-Integration-Token": token}
	code, body := adapterRequest(t, a, "GET", "/api/integrations/"+m.ID+"/config", nil, headers)
	if code != 200 || body["settings"].(map[string]interface{})["enabled"] != false || body["grants"].(map[string]interface{})["shell"] != false || body["grants"].(map[string]interface{})["discord.viewer"] != true {
		t.Fatalf("optional settings/grants must reflect global denial: %v", body)
	}
	_, body = adapterRequest(t, a, "POST", "/api/integrations/"+m.ID+"/heartbeat", map[string]string{"state": "ready"}, headers)
	if body["lease_seconds"] != float64(45) || body["grants"].(map[string]interface{})["shell"] != false {
		t.Fatal("heartbeat must return fresh effective grants")
	}
	code, _ = adapterRequest(t, a, "PUT", "/api/integrations/"+m.ID+"/config", map[string]interface{}{"grants": map[string]bool{"shell": true}}, headers)
	if code == 200 {
		t.Fatal("runtime credential must not write its configuration")
	}
}
func TestViewerOverlayUsesCuratedTextAndFragmentCredentials(t *testing.T) {
	_, a, _ := adapterFixture(t)
	r := httptest.NewRequest("GET", "/viewer/", nil)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "textContent") || strings.Contains(w.Body.String(), "innerHTML") || !strings.Contains(w.Body.String(), "history.replaceState") || !strings.Contains(w.Header().Get("Content-Security-Policy"), viewerScriptHash()) {
		t.Fatal("overlay must escape text, remove fragment credentials and pin its script")
	}
}
