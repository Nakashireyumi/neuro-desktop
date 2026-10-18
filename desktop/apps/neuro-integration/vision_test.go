package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthURLForDerivesTheHealthEndpoint(t *testing.T) {
	got, err := healthURLFor("http://127.0.0.1:8610/describe?x=1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:8610/health" {
		t.Fatalf("health URL = %q", got)
	}
	if _, err := healthURLFor("file:///etc/passwd"); err == nil {
		t.Fatal("non-http vision URLs must be rejected")
	}
}

func TestVisionURLPrefersTheCanonicalName(t *testing.T) {
	t.Setenv("NEURO_VISION_URL", "http://canonical:1/describe")
	t.Setenv("NEURO_VISION_SERVER_URL", "http://legacy:2/describe")
	if got := visionServerURL(); got != "http://canonical:1/describe" {
		t.Fatalf("visionServerURL() = %q", got)
	}
	t.Setenv("NEURO_VISION_URL", "")
	if got := visionServerURL(); got != "http://legacy:2/describe" {
		t.Fatalf("legacy alias not honoured: %q", got)
	}
}

func TestProbeReportsReachableBackendAndSendsToken(t *testing.T) {
	var sawToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.Header.Get("Authorization")
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "backend": "stats"})
	}))
	defer server.Close()

	t.Setenv("NEURO_VISION_URL", server.URL+"/describe")
	t.Setenv("NEURO_VISION_TOKEN", "abc")
	status := probeVisionServer(true)
	if !status.Configured || !status.Reachable || status.Backend != "stats" {
		t.Fatalf("probe = %+v", status)
	}
	if sawToken != "Bearer abc" {
		t.Fatalf("token header = %q", sawToken)
	}
}

func TestProbeReportsUnreachableWithoutCrashing(t *testing.T) {
	t.Setenv("NEURO_VISION_URL", "http://127.0.0.1:9/describe")
	status := probeVisionServer(true)
	if !status.Configured || status.Reachable || !strings.Contains(status.Error, "not reachable") {
		t.Fatalf("probe = %+v", status)
	}
}

func TestProbeWithNothingConfigured(t *testing.T) {
	t.Setenv("NEURO_VISION_URL", "")
	t.Setenv("NEURO_VISION_SERVER_URL", "")
	status := probeVisionServer(true)
	if status.Configured || status.Reachable {
		t.Fatalf("probe = %+v", status)
	}
}

func TestSummarizeSendsImageAndExtractsSummary(t *testing.T) {
	var body visionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]string{"summary": "a red square"})
	}))
	defer server.Close()

	shot := append([]byte("\x89PNG\r\n\x1a\n"), []byte("PNGDATA")...)
	got, err := summarizeWithVisionServer(server.URL, shot, "")
	if err != nil {
		t.Fatalf("summarize failed: %v", err)
	}
	if got != "a red square" {
		t.Fatalf("summary = %q", got)
	}
	if body.ImageBase64 == "" {
		t.Fatal("a small screenshot must be sent inline as image_base64")
	}
}
