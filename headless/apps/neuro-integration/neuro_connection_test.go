package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardReportsNeuroConnectionSeparately(t *testing.T) {
	integration := newTestIntegrationForAdmin(t)
	server := newTestAdmin(t, integration, testDashboardToken)
	for _, path := range []string{"/api/status", "/api/runtime"} {
		_, status := getJSON(t, server.URL+path, nil)
		neuro := status["neuro"].(map[string]interface{})
		if neuro["connected"] != false || neuro["state"] != "waiting" {
			t.Fatalf("a running HTTP server must not imply a Neuro connection: %v", status)
		}
	}
	integration.neuroConnection.connecting()
	integration.neuroConnection.disconnected()
	integration.neuroConnection.connecting()
	integration.neuroConnection.connected()
	_, status := getJSON(t, server.URL+"/api/status", nil)
	neuro := status["neuro"].(map[string]interface{})
	if neuro["connected"] != true || neuro["attempts"] != float64(2) || neuro["last_error"] != "" || neuro["connected_at"] == "" {
		t.Fatalf("connection recovery was not reflected in status: %v", neuro)
	}
}

func TestDashboardRejectedTokenIsAuditedWithoutSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	t.Setenv("NEURO_AUDIT_LOG", path)
	integration := newTestIntegrationForAdmin(t)
	integration.audit = newAuditor()
	server := newTestAdmin(t, integration, testDashboardToken)
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/session?token=query-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ND-Token", "rejected-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("HTTP %d", resp.StatusCode)
	}
	entries, _, err := tailJSONLines(path, 10)
	if err != nil || len(entries) != 1 || entries[0]["event"] != "dashboard_auth" || entries[0]["decision"] != "denied" {
		t.Fatalf("missing failed sign-in audit: %v, %v", entries, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"query-secret", "rejected-secret", testDashboardToken} {
		if strings.Contains(string(data), secret) {
			t.Fatal("the audit log contains a dashboard credential")
		}
	}
}
