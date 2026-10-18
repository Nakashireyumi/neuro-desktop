package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func gameDetectionResponse() (*IPCResponse, bool) {
	return &IPCResponse{Success: true, Data: map[string]interface{}{
		"active_window":     "Private Minecraft fixture",
		"running_processes": []interface{}{"javaw.exe"},
	}}, true
}

func detectionFixture(t *testing.T, handler func(IPCCommand) (*IPCResponse, bool)) (*NDIntegration, *fakeExecutor) {
	t.Helper()
	n, executor := observationFixture(t, handler)
	n.games = newGameRuntime(&GameRegistry{profiles: []GameProfile{{
		ID: "minecraft", Name: "Minecraft", Match: GameMatch{Processes: []string{"javaw.exe"}},
	}}})
	t.Cleanup(func() { _ = executor.conn.Close() })
	return n, executor
}

func TestGameDetectionDoesNotReuseMetadataAfterVisionRevocation(t *testing.T) {
	n, executor := detectionFixture(t, func(IPCCommand) (*IPCResponse, bool) { return gameDetectionResponse() })
	detected, err := n.detectGame(true)
	if err != nil || detected.Profile == nil || detected.Profile.ID != "minecraft" {
		t.Fatalf("approved detection failed: %+v %v", detected, err)
	}
	refuseObservation(n)
	if detected, err := n.detectGame(false); err == nil || detected.ActiveTitle != "" {
		t.Fatalf("cached private metadata bypassed revocation: %+v %v", detected, err)
	}
	if len(executor.seenCommands()) != 1 {
		t.Fatal("denied detection still inspected the controlled computer")
	}
	n.setPolicy(defaultPermissionPolicy())
	if _, err := n.detectGame(false); err != nil {
		t.Fatal(err)
	}
	if len(executor.seenCommands()) != 2 {
		t.Fatal("renewed vision permission reused an observation from the revoked policy")
	}
}

func TestGameDetectionDoesNotReuseAReplacedTemporaryApproval(t *testing.T) {
	n, executor := detectionFixture(t, func(IPCCommand) (*IPCResponse, bool) { return gameDetectionResponse() })
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeVision] = ScopeConfig{Requestable: true}
	n.setPolicy(policy)
	now := time.Now()
	policy.grants.put(ScopeGrant{Scope: ScopeVision, GrantedAt: now, RequestID: "original"})
	if _, err := n.detectGame(true); err != nil {
		t.Fatal(err)
	}
	policy.grants.put(ScopeGrant{Scope: ScopeVision, GrantedAt: now.Add(time.Millisecond), RequestID: "replacement"})
	if _, err := n.detectGame(false); err != nil {
		t.Fatal(err)
	}
	if len(executor.seenCommands()) != 2 {
		t.Fatal("temporary approval replacement inherited the previous observation cache")
	}
}

func TestGamesDashboardDropsInFlightDetectionAfterVisionRevocation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	n, executor := detectionFixture(t, func(IPCCommand) (*IPCResponse, bool) {
		close(entered)
		<-release
		return gameDetectionResponse()
	})
	a := NewAdminServer(n, "", credentialFromToken(testDashboardToken, "test"))
	finished := make(chan map[string]interface{}, 1)
	go func() {
		_, response := adapterRequest(t, a, http.MethodGet, "/api/games", nil, map[string]string{"X-ND-Token": testDashboardToken})
		finished <- response
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("dashboard did not request detection")
	}
	refuseObservation(n)
	close(release)
	select {
	case response := <-finished:
		if response["detected"] != nil {
			t.Fatalf("dashboard disclosed revoked window/process metadata: %+v", response["detected"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoked dashboard request did not finish")
	}
	if len(executor.seenCommands()) != 1 {
		t.Fatal("dashboard unexpectedly retried after approval changed")
	}
	if status := n.describeGameStatus(); strings.Contains(status, "Private Minecraft") || strings.Contains(status, "Detected Minecraft") {
		t.Fatalf("game status leaked the revoked detection: %s", status)
	}
}
