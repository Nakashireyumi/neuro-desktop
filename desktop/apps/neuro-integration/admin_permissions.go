package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Dashboard handlers for permission requests, approvals, and catalog trust.
// Every write here is an operator action, so each one is audited.

// readJSONBody decodes a small JSON body. An empty body is allowed and leaves v
// at its zero value.
func readJSONBody(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("could not read the request body")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid JSON body: %v", err)
	}
	return nil
}

// catalogItemView is the catalog entry as the dashboard sees it: the item's own
// fields plus the signature state, computed on the spot from the publisher file.
func catalogItemView(item CatalogItem, publishers publisherFile) map[string]interface{} {
	view := map[string]interface{}{}
	if raw, err := json.Marshal(catalogPresentationItem(item, true)); err == nil {
		_ = json.Unmarshal(raw, &view)
	}
	status := verifyCatalogItem(item, publishers)
	view["signature_state"] = status.State
	view["signature_detail"] = status.Detail
	return view
}

// A display-only compatibility transform corrects the shipped Memory entry's
// old wording without altering signed catalog bytes or installation metadata.
// Signature checks always use the original item, before presentation.
func catalogPresentationItem(item CatalogItem, operator bool) CatalogItem {
	if item.ID != "memory" {
		return item
	}
	if operator {
		item.Description = strings.ReplaceAll(item.Description, "once Vedal enables this extension", "when you enable this extension")
	} else {
		item.Description = strings.ReplaceAll(item.Description, "once Vedal enables this extension", "after runtime approval and enabling")
	}
	hints := make([]string, len(item.LaunchHints))
	copy(hints, item.LaunchHints)
	for i, hint := range hints {
		if hint == "Installs disabled. Vedal turns it on in the Extensions page; Neuro cannot start it herself." {
			if operator {
				hints[i] = "Installs disabled. Enable it here, or allow Neuro and Evil to manage it with per-extension permissions."
			} else {
				hints[i] = "Installs disabled. You can enable it after its runtime permissions are approved and management is delegated to you."
			}
		}
	}
	item.LaunchHints = hints
	return item
}

// handlePermissionRequests returns the operator's queue: what Neuro asked for,
// what is still open, and which approvals are active.
func (a *AdminServer) handlePermissionRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, a.integration.permissionRequestsSnapshot())
}

// handlePermissionRequestAction handles POST
// /api/permission-requests/{id}/approve and /api/permission-requests/{id}/deny.
// Approve takes an optional "minutes" (0 = until revoked, omitted = the duration
// Neuro asked for) and an optional "note" that is passed on to Neuro.
func (a *AdminServer) handlePermissionRequestAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireOperatorReentry(w, r) {
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/permission-requests/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusBadRequest, "expected /api/permission-requests/{id}/approve or /deny")
		return
	}
	id := strings.TrimSpace(parts[0])

	var body struct {
		Minutes *int   `json:"minutes"`
		Note    string `json:"note"`
	}
	if err := readJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var (
		decided PermissionRequest
		err     error
	)
	switch strings.ToLower(parts[1]) {
	case "approve":
		minutes := requestDurationAsked
		if body.Minutes != nil {
			minutes = *body.Minutes
		}
		decided, err = a.integration.decideRequest(id, true, minutes, body.Note)
	case "deny":
		decided, err = a.integration.decideRequest(id, false, 0, body.Note)
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown action %q (use approve or deny)", parts[1]))
		return
	}

	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, errNoSuchRequest) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	verb := "denied"
	if decided.Status == "approved" {
		verb = "approved"
	}
	a.writeAudit(fmt.Sprintf("permission request %s %s for %s", id, verb, decided.Scope), false)
	go a.integration.syncMCPServers()
	writeJSON(w, map[string]interface{}{"ok": true, "request": decided})
}

// handlePermissionGrant handles POST /api/permissions/grants/{scope}/revoke.
func (a *AdminServer) handlePermissionGrant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireOperatorReentry(w, r) {
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/permissions/grants/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "revoke" {
		writeError(w, http.StatusBadRequest, "expected /api/permissions/grants/{scope}/revoke")
		return
	}

	scope := PermissionScope(strings.ToLower(strings.TrimSpace(parts[0])))
	if !knownScope(scope) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown permission scope %q", parts[0]))
		return
	}
	if !a.integration.revokeGrant(scope) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no approval is active for the %s scope", scope))
		return
	}

	a.writeAudit(fmt.Sprintf("permission approval for %s revoked", scope), false)
	go a.integration.syncMCPServers()
	writeJSON(w, map[string]interface{}{"ok": true, "scope": scope})
}
