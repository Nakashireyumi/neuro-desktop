package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func grantExpiryTestIntegration(t *testing.T) *NDIntegration {
	t.Helper()
	n := &NDIntegration{}
	policy := defaultPermissionPolicy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: false, Requestable: true}
	n.setPolicy(policy)
	t.Cleanup(n.stopGrantExpiryTimers)
	return n
}

func waitGrantExpiry(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("grant expiry did not complete")
}

func grantTimerCount(n *NDIntegration) int {
	n.grantExpiry.mu.Lock()
	defer n.grantExpiry.mu.Unlock()
	return len(n.grantExpiry.timers)
}

func setGrantViewerFixture(n *NDIntegration) {
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.presence["fixture"] = &adapterPresence{VisibleUntil: time.Now().Add(time.Hour), Items: []viewerActivity{{Summary: "current session"}}}
}

func TestGrantExpiryClearsViewerHistoryWithoutPollingOrReads(t *testing.T) {
	n := grantExpiryTestIntegration(t)
	setGrantViewerFixture(n)
	now := time.Now()
	if !n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(150 * time.Millisecond), RequestID: "short"}) {
		t.Fatal("grant failed")
	}
	// Do not read the grant while waiting: the timer must trigger cleanup on
	// an idle server, without relying on lazy expiry in a permission check.
	// It may already have fired if a loaded runner descheduled this goroutine.
	waitGrantExpiry(t, func() bool { return grantTimerCount(n) == 0 })
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if p := rt.presence["fixture"]; !p.VisibleUntil.IsZero() || len(p.Items) != 0 {
		t.Fatal("expired grant retained viewer visibility or history")
	}
	if n.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatal("expired grant remains active")
	}
}

func TestGrantExpiryReplacementAndPermanentApprovalIgnoreStaleCallback(t *testing.T) {
	n := grantExpiryTestIntegration(t)
	now := time.Now()
	old := ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(time.Hour), RequestID: "old"}
	n.putScopeGrant(n.policy().grants, old)
	n.grantExpiry.mu.Lock()
	stale := n.grantExpiry.timers[ScopeExtensions]
	n.grantExpiry.mu.Unlock()
	for i := 0; i < 50; i++ {
		n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(2 * time.Hour), RequestID: "replacement"})
		if grantTimerCount(n) != 1 {
			t.Fatal("renewal accumulated timers for the same scope")
		}
	}
	n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, RequestID: "permanent"})
	setGrantViewerFixture(n)
	n.expireScopeGrant(stale) // Simulate a callback already queued before Stop.
	if grantTimerCount(n) != 0 || !n.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatal("stale timer revoked a permanent replacement or remained scheduled")
	}
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.presence["fixture"].Items) != 1 {
		t.Fatal("stale callback cleared the replacement's activity")
	}
}

func TestGrantExpirySerializesCleanupBeforeConcurrentRenewal(t *testing.T) {
	n := grantExpiryTestIntegration(t)
	rt := n.adapterRuntime()
	rt.mu.Lock()
	locked := true
	defer func() {
		if locked {
			rt.mu.Unlock()
		}
	}()
	now := time.Now()
	n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(30 * time.Millisecond), RequestID: "old"})
	// Hold the adapter lock so the actual expiry callback reaches cleanup but
	// cannot finish it. A renewal must wait until that old cleanup completes.
	waitGrantExpiry(t, func() bool {
		if n.grantExpiry.mu.TryLock() {
			n.grantExpiry.mu.Unlock()
			return false
		}
		return true
	})
	renewed := make(chan bool, 1)
	go func() {
		renewed <- n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: time.Now(), RequestID: "new"})
	}()
	select {
	case <-renewed:
		t.Fatal("renewal raced ahead of in-progress expiry cleanup")
	case <-time.After(10 * time.Millisecond):
	}
	rt.mu.Unlock()
	locked = false
	select {
	case ok := <-renewed:
		if !ok {
			t.Fatal("renewal failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expiry and renewal deadlocked")
	}
	setGrantViewerFixture(n)
	if !n.policy().ScopeAllowed(ScopeExtensions) || grantTimerCount(n) != 0 {
		t.Fatal("renewed permanent approval did not survive expiry cleanup")
	}
}

func TestGrantExpiryRevokeCancelsQueuedCallback(t *testing.T) {
	n := grantExpiryTestIntegration(t)
	now := time.Now()
	n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(time.Hour), RequestID: "revoke"})
	n.grantExpiry.mu.Lock()
	stale := n.grantExpiry.timers[ScopeExtensions]
	n.grantExpiry.mu.Unlock()
	if !n.revokeGrant(ScopeExtensions) || grantTimerCount(n) != 0 {
		t.Fatal("revocation must remove the grant and its timer")
	}
	setGrantViewerFixture(n)
	n.expireScopeGrant(stale)
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.presence["fixture"].Items) != 1 {
		t.Fatal("canceled expiry callback still performed cleanup")
	}
}

func TestGrantExpiryStopsAnIdleRealMCPProcess(t *testing.T) {
	n := newMCPTestIntegration(t, `{"scopes":{"extensions":{"allowed":false,"requestable":true}}}`)
	attachFakeServer(t, n, "memory")
	t.Cleanup(n.stopGrantExpiryTimers)
	client := n.mcp.client("memory")
	dir := t.TempDir()
	key, indexPath, publishers := signedCatalogFixture(t, dir)
	writeJSONFile(t, indexPath, CatalogIndex{Items: []CatalogItem{{ID: "memory", Name: "Memory", Publisher: "acme", MCP: func() *CatalogMCP { spec := fakeMCPSpec(t); return &spec }()}}})
	var output bytes.Buffer
	if code := catalogSign([]string{"--key", key, "--index", indexPath}, &output, &output); code != 0 {
		t.Fatal(output.String())
	}
	t.Setenv("NEURO_CATALOG_FILE", indexPath)
	t.Setenv("NEURO_PUBLISHERS_FILE", publishers)
	if err := saveExtensionState(extensionStatePath(), ExtensionStateFile{Installed: map[string]ExtensionInstallState{"memory": {Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now, ExpiresAt: now.Add(500 * time.Millisecond), RequestID: "mcp"})
	if _, wanted := n.desiredMCPServers()["memory"]; !wanted {
		t.Fatal("fixture must permit its running MCP server before expiry")
	}
	select {
	case <-client.waitDone:
		t.Fatal("fixture MCP process was already stopped")
	default:
	}
	select {
	case <-client.waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("temporary approval expired but idle MCP process remained alive")
	}
	if n.mcp.client("memory") != nil {
		t.Fatal("expired MCP server remains registered")
	}
}

func TestGrantExpiryCloseStopsAllTimersAndRejectsNewApprovals(t *testing.T) {
	n, err := NewNDIntegration(IntegrationOptions{WSURL: "ws://127.0.0.1:1", GameName: "grant-expiry-test", PermissionsPath: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, scope := range []PermissionScope{ScopeExtensions, ScopeVision} {
		if !n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: scope, GrantedAt: now, ExpiresAt: now.Add(time.Hour)}) {
			t.Fatal("grant failed")
		}
	}
	n.grantExpiry.mu.Lock()
	stale := n.grantExpiry.timers[ScopeExtensions]
	n.grantExpiry.mu.Unlock()
	currentActionListMu.Lock()
	previousActions := currentActionList
	currentActionList = nil
	currentActionListMu.Unlock()
	defer func() {
		currentActionListMu.Lock()
		currentActionList = previousActions
		currentActionListMu.Unlock()
	}()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if grantTimerCount(n) != 0 {
		t.Fatal("shutdown retained scheduled timers")
	}
	if n.putScopeGrant(n.policy().grants, ScopeGrant{Scope: ScopeExtensions, GrantedAt: now}) {
		t.Fatal("shutdown accepted a new approval")
	}
	setGrantViewerFixture(n)
	n.expireScopeGrant(stale)
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.presence["fixture"].Items) != 1 {
		t.Fatal("a callback queued before shutdown still ran cleanup afterward")
	}
}
