package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeEndpointReportsEachComponent(t *testing.T) {
	t.Setenv("NEURO_VISION_URL", "")
	t.Setenv("NEURO_VISION_SERVER_URL", "")
	integration := newTestIntegrationForAdmin(t)
	server := newTestAdmin(t, integration, testDashboardToken)

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/runtime", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	request.Header.Set("X-ND-Token", testDashboardToken)
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	body := map[string]map[string]interface{}{}
	for _, key := range []string{"bridge", "executor", "relay", "vision", "mcp"} {
		section, ok := raw[key].(map[string]interface{})
		if !ok {
			t.Fatalf("runtime is missing %q: %v", key, raw)
		}
		body[key] = section
	}
	if body["bridge"]["state"] != "running" {
		t.Fatalf("bridge = %v", body["bridge"])
	}
	if body["relay"]["state"] != "disabled" {
		t.Fatalf("relay = %v", body["relay"])
	}
	if body["vision"]["state"] != "not_configured" {
		t.Fatalf("vision = %v", body["vision"])
	}
	if body["mcp"]["state"] != "off" {
		t.Fatalf("mcp = %v", body["mcp"])
	}
}

func TestRelayStateMapping(t *testing.T) {
	cases := []struct {
		status RelayStatus
		want   string
	}{
		{RelayStatus{Enabled: false}, "disabled"},
		{RelayStatus{Enabled: true, Connected: false}, "disconnected"},
		{RelayStatus{Enabled: true, Connected: true, Registered: false}, "connected"},
		{RelayStatus{Enabled: true, Connected: true, Registered: true, PeerCount: 0}, "idle"},
		{RelayStatus{Enabled: true, Connected: true, Registered: true, PeerCount: 2}, "registered"},
	}
	for _, tc := range cases {
		if got := relayRuntimeState(tc.status); got != tc.want {
			t.Errorf("relayRuntimeState(%+v) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestMCPStateMapping(t *testing.T) {
	if got := mcpRuntimeState(nil); got != "off" {
		t.Errorf("no servers = %q", got)
	}
	if got := mcpRuntimeState([]mcpServerStatus{{State: "failed"}}); got != "failed" {
		t.Errorf("only failed = %q", got)
	}
	if got := mcpRuntimeState([]mcpServerStatus{{State: "failed"}, {State: "running"}}); got != "running" {
		t.Errorf("one running = %q", got)
	}
}

func TestRuntimeURLsLoseCredentialsAndQueries(t *testing.T) {
	got := redactURL("http://user:hunter2@127.0.0.1:8610/describe?token=abc")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "token=abc") {
		t.Fatalf("credentials leaked: %s", got)
	}
	if got != "http://127.0.0.1:8610/describe" {
		t.Fatalf("redactURL = %q", got)
	}
}

// newTestIntegrationForAdmin mirrors the helper the admin tests use, so the
// runtime endpoint can be exercised without a Neuro connection.
func newTestIntegrationForAdmin(t *testing.T) *NDIntegration {
	t.Helper()
	permissionsPath := filepath.Join(t.TempDir(), "permissions.json")
	policy := defaultPermissionPolicy()
	return &NDIntegration{
		ipcFilePath:     filepath.Join(t.TempDir(), "ipc.json"),
		permissionsPath: permissionsPath,
		permissions:     policy,
		games:           newGameRuntime(loadGameRegistry()),
		relay:           newRelayState(RelayConfig{}),
		stats:           newBridgeStats(),
		stop:            newStopSwitch(),
		mcp:             newMCPManager(),
		requests:        newRequestBook(),
		startedAt:       time.Now(),
	}
}
