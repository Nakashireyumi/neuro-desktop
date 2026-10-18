package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func observationFixture(t *testing.T, handler func(IPCCommand) (*IPCResponse, bool)) (*NDIntegration, *fakeExecutor) {
	t.Helper()
	hub := newHub(t, "")
	executor := startFakeExecutor(t, hub, "", handler)
	n := &NDIntegration{executorHub: hub, games: newGameRuntime(nil), stats: newBridgeStats()}
	n.setPolicy(defaultPermissionPolicy())
	t.Setenv("NEURO_VISION_URL", "")
	t.Setenv("NEURO_VISION_SERVER_URL", "")
	return n, executor
}

func observationResponse() (*IPCResponse, bool) {
	return &IPCResponse{Success: true, Data: map[string]interface{}{
		"active_window":      "Private fixture window",
		"screenshot_png_b64": base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfixture")),
	}}, true
}

func refuseObservation(n *NDIntegration) {
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeVision] = ScopeConfig{Allowed: false}
	n.setPolicy(policy)
}

func TestObservationDenialCollectsNothing(t *testing.T) {
	n, executor := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) { return observationResponse() })
	refuseObservation(n)
	if _, err := n.buildGameObservation(true, ""); err == nil {
		t.Fatal("game observation ignored live vision permission")
	}
	if result := n.sendDesktopContext(true, true); result.Successful {
		t.Fatal("periodic context bypassed live vision permission")
	}
	if len(executor.seenCommands()) != 0 {
		t.Fatal("denied observation reached the controlled computer")
	}
}

func TestExplicitFalseNeverCapturesOrUploads(t *testing.T) {
	n, executor := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) { return observationResponse() })
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads.Add(1)
		_, _ = fmt.Fprint(w, `{"summary":"fixture"}`)
	}))
	defer server.Close()
	t.Setenv("NEURO_VISION_URL", server.URL)
	a := NewAdminServer(n, "", credentialFromToken(testDashboardToken, "test"))
	code, body := adapterRequest(t, a, "POST", "/api/games/observe", map[string]interface{}{"vision": false}, map[string]string{"X-ND-Token": testDashboardToken})
	if code != 200 {
		t.Fatalf("metadata observation failed: %d %v", code, body)
	}
	if _, err := n.getDesktopContext(false); err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 0 {
		t.Fatal("explicit false still uploaded a screenshot")
	}
	commands := executor.seenCommands()
	if len(commands) != 2 {
		t.Fatalf("wanted two metadata requests, got %d", len(commands))
	}
	for _, command := range commands {
		if command.Params["capture_screenshot"] != false {
			t.Fatal("explicit false still requested capture")
		}
	}
}

func TestObservationRechecksPermissionAfterWaitingForExecutor(t *testing.T) {
	n, executor := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) { return observationResponse() })
	permit, err := n.observationPermit(CmdGameObserve)
	if err != nil {
		t.Fatal(err)
	}
	n.ipcMu.Lock()
	finished := make(chan error, 1)
	go func() { _, err := n.fetchDesktopStatusGuarded(true, permit.check); finished <- err }()
	refuseObservation(n)
	n.ipcMu.Unlock()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("approval changed while queued, but capture was dispatched")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued observation did not finish")
	}
	if len(executor.seenCommands()) != 0 {
		t.Fatal("revoked queued capture reached the controlled computer")
	}
}

func TestObservationDropsCaptureAndInferenceAfterRevocation(t *testing.T) {
	for _, phase := range []string{"capture", "inference"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			n, _ := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) {
				if phase == "capture" {
					close(entered)
					<-release
				}
				return observationResponse()
			})
			var uploads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploads.Add(1)
				if phase == "inference" {
					close(entered)
					<-release
				}
				_, _ = fmt.Fprint(w, `{"summary":"Private fixture analysis"}`)
			}))
			defer server.Close()
			t.Setenv("NEURO_VISION_URL", server.URL)
			finished := make(chan error, 1)
			go func() { _, err := n.buildGameObservation(true, ""); finished <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("observation did not reach controlled phase")
			}
			refuseObservation(n)
			close(release)
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("revoked observation disclosed its result")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("revoked observation did not finish")
			}
			if phase == "capture" && uploads.Load() != 0 {
				t.Fatal("capture completed after revocation but was uploaded")
			}
		})
	}
}

func TestObservationCannotReuseAReplacementTemporaryApproval(t *testing.T) {
	n := &NDIntegration{}
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeVision] = ScopeConfig{Requestable: true}
	n.setPolicy(policy)
	now := time.Now()
	policy.grants.put(ScopeGrant{Scope: ScopeVision, GrantedAt: now, RequestID: "first"})
	permit, err := n.observationPermit(CmdGameObserve)
	if err != nil {
		t.Fatal(err)
	}
	policy.grants.put(ScopeGrant{Scope: ScopeVision, GrantedAt: now.Add(time.Second), RequestID: "replacement"})
	if err := permit.check(); err == nil {
		t.Fatal("an older observation reused a newly approved session")
	}
}

func TestVisionResponseReadIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", maxVisionResponseBytes+1))
	}))
	defer server.Close()
	if _, err := summarizeWithVisionServer(server.URL, []byte("fixture"), ""); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized inference response was not bounded: %v", err)
	}
}
