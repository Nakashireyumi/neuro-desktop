package main

import (
	"net/http"
	"testing"
)

func TestOperatorSessionCannotGrantPermissionsWithoutReentry(t *testing.T) {
	n := testIntegration(t)
	a := NewAdminServer(n, "", credentialFromToken(testDashboardToken, "test"))
	body := map[string]interface{}{"default_allow": false, "scopes": map[string]interface{}{"input": false}}
	for _, method := range []string{http.MethodPut, http.MethodPost} {
		code, _ := adapterRequest(t, a, method, "/api/permissions", body, map[string]string{"X-ND-Token": testDashboardToken})
		if code != 403 {
			t.Fatalf("a signed-in session alone granted global rights: %d", code)
		}
	}
	code, _ := adapterRequest(t, a, http.MethodPut, "/api/permissions", body, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != 200 || n.policy().ScopeAllowed(ScopeInput) {
		t.Fatal("freshly approved global policy must apply live")
	}
	for _, path := range []string{"/api/permission-requests/test/approve", "/api/permission-requests/test/deny", "/api/permissions/grants/input/revoke"} {
		code, _ := adapterRequest(t, a, http.MethodPost, path, map[string]interface{}{}, map[string]string{"X-ND-Token": testDashboardToken})
		if code != 403 {
			t.Fatalf("ordinary session changed permission grants through %s: %d", path, code)
		}
	}
}
