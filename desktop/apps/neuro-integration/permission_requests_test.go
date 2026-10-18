package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// requestTestIntegration builds an integration with a policy where the
// extensions scope is off and requestable, and a controllable clock.
func requestTestIntegration(t *testing.T, requestable bool) (*NDIntegration, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }

	policy := defaultPermissionPolicy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: false, Requestable: requestable}
	policy.grants.now = now

	book := newRequestBook()
	book.now = now

	integration := &NDIntegration{requests: book}
	integration.setPolicy(policy)
	return integration, &clock
}

func TestRequestPermissionFilesPendingAndNeverGrants(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)

	result := integration.requestPermission("extensions", "install the MCP bridge", 0, "neuro")
	if !result.Successful {
		t.Fatalf("expected the request to be filed, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "req-001") {
		t.Fatalf("expected the request id in the reply, got: %s", result.Message)
	}
	if integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("filing a request must not switch the scope on")
	}

	pending, _ := integration.requests.list()
	if len(pending) != 1 || pending[0].Status != "pending" || pending[0].Minutes != requestDefaultMinutes {
		t.Fatalf("expected one pending request with the default duration, got %+v", pending)
	}
}

func TestRequestPermissionRefusedWhenNotRequestable(t *testing.T) {
	integration, _ := requestTestIntegration(t, false)

	result := integration.requestPermission("extensions", "please", 10, "neuro")
	if result.Successful {
		t.Fatalf("a scope that is not requestable must refuse the request")
	}
	if !strings.Contains(result.Message, "not allowed to ask") && !strings.Contains(result.Message, "may not ask") {
		t.Fatalf("expected the refusal to say requests are off, got: %s", result.Message)
	}
}

func TestRequestPermissionValidatesInput(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)

	if result := integration.requestPermission("nonsense", "why", 10, "neuro"); result.Successful {
		t.Fatalf("unknown scope must fail")
	}
	if result := integration.requestPermission("extensions", "   ", 10, "neuro"); result.Successful {
		t.Fatalf("an empty reason must fail")
	}
	if result := integration.requestPermission("extensions", "why", 999, "neuro"); result.Successful {
		t.Fatalf("minutes above the maximum must fail")
	}
}

func TestRequestPermissionDoesNotDuplicateOpenRequest(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)

	first := integration.requestPermission("extensions", "reason one", 10, "neuro")
	second := integration.requestPermission("extensions", "reason two", 10, "neuro")
	if !first.Successful || !second.Successful {
		t.Fatalf("both calls should succeed; the second says it is already waiting")
	}
	if !strings.Contains(second.Message, "still waiting") {
		t.Fatalf("expected the second call to point at the open request, got: %s", second.Message)
	}
	pending, _ := integration.requests.list()
	if len(pending) != 1 {
		t.Fatalf("expected a single open request, got %d", len(pending))
	}
}

func TestApproveCreatesGrantThatExpires(t *testing.T) {
	integration, clock := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "install the MCP bridge", 30, "neuro")

	decided, err := integration.decideRequest("req-001", true, requestDurationAsked, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if decided.Status != "approved" || decided.Minutes != 30 {
		t.Fatalf("expected an approved 30-minute request, got %+v", decided)
	}
	if !integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("an approved request must lift the scope while the grant is in force")
	}
	if !integration.policy().IsAllowed(string(CmdInstallExtension)) {
		t.Fatalf("an approved extensions scope must allow install_extension")
	}

	*clock = clock.Add(29 * time.Minute)
	if !integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("grant should still be in force at 29 minutes")
	}
	*clock = clock.Add(2 * time.Minute)
	if integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("grant must expire after its duration")
	}
}

func TestApproveRejectsBadDurationAndDecidedRequests(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "reason", 10, "neuro")

	if _, err := integration.decideRequest("req-001", true, 5000, ""); err == nil {
		t.Fatalf("a grant longer than the maximum must be refused")
	}
	if _, err := integration.decideRequest("req-001", true, 15, ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if _, err := integration.decideRequest("req-001", false, 0, ""); err == nil {
		t.Fatalf("a decided request must not be decided again")
	}
	if _, err := integration.decideRequest("req-999", true, 15, ""); err == nil {
		t.Fatalf("an unknown request must be refused")
	}
}

func TestDenyStartsCooldown(t *testing.T) {
	integration, clock := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "reason", 10, "neuro")

	if _, err := integration.decideRequest("req-001", false, 0, "not today"); err != nil {
		t.Fatalf("deny failed: %v", err)
	}
	if integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("a denied request must not switch the scope on")
	}

	again := integration.requestPermission("extensions", "reason", 10, "neuro")
	if again.Successful || !strings.Contains(again.Message, "denied") {
		t.Fatalf("a request right after a denial must be refused with a cooldown, got: %+v", again)
	}

	*clock = clock.Add(requestCooldown + time.Minute)
	later := integration.requestPermission("extensions", "reason", 10, "neuro")
	if !later.Successful {
		t.Fatalf("after the cooldown a new request should be filed, got: %s", later.Message)
	}
}

func TestRequestRateLimit(t *testing.T) {
	integration, clock := requestTestIntegration(t, true)
	for i := 0; i < requestMaxPerWindow; i++ {
		integration.requests.filed = append(integration.requests.filed, *clock)
	}
	result := integration.requestPermission("extensions", "reason", 10, "neuro")
	if result.Successful || !strings.Contains(result.Message, "Too many") {
		t.Fatalf("expected the rate limit to refuse, got: %+v", result)
	}
}

func TestGrantSurvivesPolicySave(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "reason", 10, "neuro")
	if _, err := integration.decideRequest("req-001", true, requestDurationAsked, ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	// A dashboard save swaps in a freshly loaded policy with the same switches.
	fresh := defaultPermissionPolicy()
	fresh.scopes[ScopeExtensions] = ScopeConfig{Allowed: false, Requestable: true}
	integration.setPolicy(fresh)

	if !integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("an approval must survive a policy save")
	}
}

func TestSwitchingRequestableOffEndsGrant(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "reason", 10, "neuro")
	if _, err := integration.decideRequest("req-001", true, requestDurationAsked, ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	off := defaultPermissionPolicy()
	off.scopes[ScopeExtensions] = ScopeConfig{Allowed: false, Requestable: false}
	integration.setPolicy(off)

	if off.ScopeAllowed(ScopeExtensions) {
		t.Fatalf("turning requests off for a scope must end the approval it was given")
	}
}

func TestRevokeGrant(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)
	integration.requestPermission("extensions", "reason", 0, "neuro")
	if _, err := integration.decideRequest("req-001", true, 0, ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if !integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("expected the grant to be active before revoking")
	}
	if !integration.revokeGrant(ScopeExtensions) {
		t.Fatalf("expected revoke to find the grant")
	}
	if integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("revoked grant must no longer lift the scope")
	}
	if integration.revokeGrant(ScopeExtensions) {
		t.Fatalf("a second revoke has nothing to revoke")
	}
}

func TestRequestPermissionIsAlwaysAllowedByPolicy(t *testing.T) {
	policy := defaultPermissionPolicy()
	if policy.DefaultAllow {
		t.Fatalf("test precondition: default policy denies by default")
	}
	if !policy.IsAllowed(string(CmdRequestPermission)) {
		t.Fatalf("request_permission must not be refused by the policy itself")
	}

	policy.denied[string(CmdRequestPermission)] = struct{}{}
	if policy.IsAllowed(string(CmdRequestPermission)) {
		t.Fatalf("the operator's deny list must still be able to switch the request path off")
	}
}

func TestPermissionRequestAdminRoutes(t *testing.T) {
	integration, _ := requestTestIntegration(t, true)
	admin := NewAdminServer(integration, "127.0.0.1:8300", credentialFromToken(testDashboardToken, "env"))
	mux := admin.routes()

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ND-Token", testDashboardToken)
		req.Header.Set("X-ND-Operator-Token", testDashboardToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	integration.requestPermission("extensions", "install the MCP bridge", 20, "neuro")

	list := do(http.MethodGet, "/api/permission-requests", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "req-001") {
		t.Fatalf("expected the queue to list req-001, got %d: %s", list.Code, list.Body.String())
	}

	if rec := do(http.MethodPost, "/api/permission-requests/req-404/approve", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown request should be 404, got %d", rec.Code)
	}

	approve := do(http.MethodPost, "/api/permission-requests/req-001/approve", `{"minutes": 15, "note": "go on"}`)
	if approve.Code != http.StatusOK {
		t.Fatalf("approve should be 200, got %d: %s", approve.Code, approve.Body.String())
	}
	if !integration.policy().ScopeAllowed(ScopeExtensions) {
		t.Fatalf("approve route did not create the grant")
	}

	var body struct {
		OK      bool              `json:"ok"`
		Request PermissionRequest `json:"request"`
	}
	if err := json.Unmarshal(approve.Body.Bytes(), &body); err != nil {
		t.Fatalf("approve response is not JSON: %v", err)
	}
	if body.Request.GrantedMinutes != 15 || body.Request.Minutes != 20 {
		t.Fatalf("expected Neuro's 20 minutes recorded and the operator's 15 granted, got %+v", body.Request)
	}

	if rec := do(http.MethodPost, "/api/permissions/grants/extensions/revoke", ``); rec.Code != http.StatusOK {
		t.Fatalf("revoke should be 200, got %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/permissions/grants/extensions/revoke", ``); rec.Code != http.StatusNotFound {
		t.Fatalf("second revoke should be 404, got %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/permissions/grants/nonsense/revoke", ``); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown scope should be 400, got %d", rec.Code)
	}
}

func TestPermissionRequestSpecIsRegisteredInEveryMode(t *testing.T) {
	found := false
	for _, spec := range alwaysRegisteredSpecs() {
		if spec.Name == CmdRequestPermission {
			found = true
			if spec.Schema == nil {
				t.Fatalf("request_permission needs a schema so small models get the fields")
			}
		}
	}
	if !found {
		t.Fatalf("request_permission must be in the always-registered group")
	}
}
