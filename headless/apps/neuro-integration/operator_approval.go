package main

import (
	"net/http"
	"strings"
)

func (a *AdminServer) requireOperatorReentry(w http.ResponseWriter, r *http.Request) bool {
	if a.credential.matches(strings.TrimSpace(r.Header.Get("X-ND-Operator-Token"))) {
		return true
	}
	a.integration.audit.record("operator_approval", map[string]interface{}{"decision": "denied", "method": r.Method, "path": r.URL.Path})
	writeError(w, http.StatusForbidden, "Re-enter your operator token to change permissions. A signed-in or delegated session cannot approve new rights by itself.")
	return false
}
