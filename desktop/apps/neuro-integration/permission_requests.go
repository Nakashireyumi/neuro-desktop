package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// errNoSuchRequest is returned for an unknown request id, so the dashboard can
// answer 404 rather than 409.
var errNoSuchRequest = errors.New("no such permission request")

// requestDurationAsked is the decideRequest sentinel for "grant the duration
// Neuro asked for".
const requestDurationAsked = -1

// Permission requests let Neuro ask the operator for a capability that the
// policy marks as requestable (the "Neuro may request" switch on the scope).
//
// Asking never grants anything. Only an approval in the dashboard creates a
// grant, a grant always has a duration (or lasts until revoked), and every
// decision is audited and reported back to Neuro as a message. Nothing in the
// request path blocks: the action result comes back at once with "pending", so
// a slow operator can never stall Neuro's action window.

const (
	requestMinMinutes     = 1
	requestMaxMinutes     = 240
	requestDefaultMinutes = 30
	requestMaxGrantMinute = 1440
	requestMaxReasonChars = 240
	requestCooldown       = 10 * time.Minute
	requestWindow         = 10 * time.Minute
	requestMaxPerWindow   = 5
	requestHistoryLimit   = 50
)

// ScopeGrant is an operator approval in force until it expires or is revoked.
type ScopeGrant struct {
	Scope     PermissionScope `json:"scope"`
	GrantedAt time.Time       `json:"granted_at"`
	ExpiresAt time.Time       `json:"expires_at,omitempty"` // zero: until revoked
	Reason    string          `json:"reason,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
}

// grantStore is runtime state, not policy. It is shared across dashboard policy
// saves, is never written to the permissions file, and is empty after a restart,
// which is the safe direction.
type grantStore struct {
	mu     sync.Mutex
	grants map[PermissionScope]ScopeGrant
	now    func() time.Time
}

func newGrantStore() *grantStore {
	return &grantStore{grants: map[PermissionScope]ScopeGrant{}, now: time.Now}
}

func (g *grantStore) clock() time.Time {
	if g == nil || g.now == nil {
		return time.Now()
	}
	return g.now()
}

// activeFor reports whether an unexpired grant covers the scope. A nil store
// grants nothing.
func (g *grantStore) activeFor(scope PermissionScope) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.grants[scope]
	if !ok {
		return false
	}
	if !grant.ExpiresAt.IsZero() && !g.clock().Before(grant.ExpiresAt) {
		delete(g.grants, scope)
		return false
	}
	return true
}

func (g *grantStore) put(grant ScopeGrant) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants[grant.Scope] = grant
}

func (g *grantStore) revoke(scope PermissionScope) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.grants[scope]; !ok {
		return false
	}
	delete(g.grants, scope)
	return true
}

// active returns the unexpired grants, sorted by scope, for the dashboard.
func (g *grantStore) active() []ScopeGrant {
	out := []ScopeGrant{}
	if g == nil {
		return out
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	for scope, grant := range g.grants {
		if !grant.ExpiresAt.IsZero() && !now.Before(grant.ExpiresAt) {
			delete(g.grants, scope)
			continue
		}
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out
}

// PermissionRequest is one ask from Neuro, kept for the dashboard's history.
type PermissionRequest struct {
	ID      string          `json:"id"`
	Scope   PermissionScope `json:"scope"`
	Reason  string          `json:"reason"`
	Minutes int             `json:"minutes"`
	Source  string          `json:"source,omitempty"`
	Status  string          `json:"status"` // pending | approved | denied
	// GrantedMinutes is what the operator approved (0 with a set GrantedUntil
	// means until revoked). Minutes is what Neuro asked for.
	GrantedMinutes int       `json:"granted_minutes,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
	DecidedAt      time.Time `json:"decided_at,omitempty"`
	GrantedUntil   time.Time `json:"granted_until,omitempty"`
	Note           string    `json:"note,omitempty"`
}

// requestBook holds the open and recent requests and the rate-limit state.
type requestBook struct {
	mu     sync.Mutex
	seq    int
	items  []*PermissionRequest
	denied map[PermissionScope]time.Time
	filed  []time.Time
	now    func() time.Time
}

func newRequestBook() *requestBook {
	return &requestBook{denied: map[PermissionScope]time.Time{}, now: time.Now}
}

func (b *requestBook) clock() time.Time {
	if b == nil || b.now == nil {
		return time.Now()
	}
	return b.now()
}

// pendingFor returns the open request for a scope. Caller holds b.mu.
func (b *requestBook) pendingFor(scope PermissionScope) *PermissionRequest {
	for _, item := range b.items {
		if item.Scope == scope && item.Status == "pending" {
			return item
		}
	}
	return nil
}

// recentFilings counts requests filed inside the rate-limit window. Caller holds b.mu.
func (b *requestBook) recentFilings(now time.Time) int {
	kept := b.filed[:0]
	for _, when := range b.filed {
		if now.Sub(when) < requestWindow {
			kept = append(kept, when)
		}
	}
	b.filed = kept
	return len(kept)
}

// find returns the request with the given id. Caller holds b.mu.
func (b *requestBook) find(id string) *PermissionRequest {
	for _, item := range b.items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

// list copies the open and the decided requests for the dashboard.
func (b *requestBook) list() (pending []PermissionRequest, decided []PermissionRequest) {
	pending = []PermissionRequest{}
	decided = []PermissionRequest{}
	if b == nil {
		return pending, decided
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.items) - 1; i >= 0; i-- {
		item := *b.items[i]
		if item.Status == "pending" {
			pending = append(pending, item)
		} else {
			decided = append(decided, item)
		}
	}
	return pending, decided
}

// requestableScopes lists the scopes the operator has let Neuro ask for.
func requestableScopes(policy *PermissionPolicy) []PermissionScope {
	out := []PermissionScope{}
	for _, scope := range allScopes() {
		if policy.ScopeRequestable(scope) {
			out = append(out, scope)
		}
	}
	return out
}

// requestPermission handles Neuro's request_permission action. It files a
// request and returns at once; it never changes the policy.
func (n *NDIntegration) requestPermission(scopeName, reason string, minutes int, source string) neuro.ExecutionResult {
	scope := PermissionScope(strings.ToLower(strings.TrimSpace(scopeName)))
	if !knownScope(scope) {
		return neuro.NewFailureResult(fmt.Sprintf("Unknown permission scope %q. Use one of: %s.", scopeName, scopeNameList(allScopes())))
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return neuro.NewFailureResult(fmt.Sprintf("reason is required: say in one sentence why you need the %s permission. The operator reads it.", scope))
	}
	if runes := []rune(reason); len(runes) > requestMaxReasonChars {
		reason = string(runes[:requestMaxReasonChars])
	}

	if minutes == 0 {
		minutes = requestDefaultMinutes
	}
	if minutes < requestMinMinutes || minutes > requestMaxMinutes {
		return neuro.NewFailureResult(fmt.Sprintf("minutes must be between %d and %d (the default is %d).", requestMinMinutes, requestMaxMinutes, requestDefaultMinutes))
	}

	policy := n.policy()
	if !policy.ScopeRequestable(scope) {
		return neuro.NewFailureResult(fmt.Sprintf("Neuro may not ask for the %s permission: the operator has not allowed permission requests for it. Use another action, or tell the operator in chat what you need.", scope))
	}
	if policy.ScopeAllowed(scope) {
		return neuro.NewSuccessResult(fmt.Sprintf("The %s permission is already on. Retry the action you wanted.", scope))
	}

	book := n.requests
	if book == nil {
		return neuro.NewFailureResult("Permission requests are not available in this process.")
	}
	now := book.clock()

	book.mu.Lock()
	defer book.mu.Unlock()

	if open := book.pendingFor(scope); open != nil {
		return neuro.NewSuccessResult(fmt.Sprintf("Request %s for the %s permission is still waiting for the operator. Do not send it again; you will get a message when they decide.", open.ID, scope))
	}
	if when, ok := book.denied[scope]; ok && now.Sub(when) < requestCooldown {
		wait := int((requestCooldown-now.Sub(when))/time.Minute) + 1
		return neuro.NewFailureResult(fmt.Sprintf("The operator denied the %s permission a few minutes ago. Wait about %d minute(s) before asking again, or try another approach.", scope, wait))
	}
	if book.recentFilings(now) >= requestMaxPerWindow {
		return neuro.NewFailureResult("Too many permission requests in the last 10 minutes. Wait before asking again.")
	}

	book.seq++
	req := &PermissionRequest{
		ID:          fmt.Sprintf("req-%03d", book.seq),
		Scope:       scope,
		Reason:      reason,
		Minutes:     minutes,
		Source:      source,
		Status:      "pending",
		RequestedAt: now,
	}
	book.items = append(book.items, req)
	if len(book.items) > requestHistoryLimit {
		book.items = book.items[len(book.items)-requestHistoryLimit:]
	}
	book.filed = append(book.filed, now)

	n.recordAudit("permission_request", map[string]interface{}{
		"request_id": req.ID, "scope": string(scope), "minutes": minutes, "source": source, "decision": "pending",
	})
	return neuro.NewSuccessResult(fmt.Sprintf("Request %s sent to the operator for the %s permission (%d minutes). Wait for their decision: you will get a message. Do not repeat the request.", req.ID, scope, minutes))
}

// decideRequest is the operator's answer, from the dashboard. minutes is the
// grant length: 0 means until revoked, requestDurationAsked means the duration
// Neuro asked for, and anything else is a fixed number of minutes.
func (n *NDIntegration) decideRequest(id string, approve bool, minutes int, note string) (PermissionRequest, error) {
	book := n.requests
	policy := n.policy()
	if book == nil || policy == nil {
		return PermissionRequest{}, fmt.Errorf("permission requests are not available in this process")
	}
	if approve && minutes != requestDurationAsked && (minutes < 0 || minutes > requestMaxGrantMinute) {
		return PermissionRequest{}, fmt.Errorf("minutes must be 0 (until revoked), %d (as asked), or between 1 and %d", requestDurationAsked, requestMaxGrantMinute)
	}
	note = strings.TrimSpace(note)
	if runes := []rune(note); len(runes) > requestMaxReasonChars {
		note = string(runes[:requestMaxReasonChars])
	}

	book.mu.Lock()
	req := book.find(id)
	if req == nil {
		book.mu.Unlock()
		return PermissionRequest{}, fmt.Errorf("%w: %s", errNoSuchRequest, id)
	}
	if req.Status != "pending" {
		status := req.Status
		book.mu.Unlock()
		return PermissionRequest{}, fmt.Errorf("request %s was already %s", id, status)
	}

	now := book.clock()
	if approve && minutes == requestDurationAsked {
		minutes = req.Minutes
	}
	var message string
	if approve {
		grant := ScopeGrant{Scope: req.Scope, GrantedAt: now, Reason: req.Reason, RequestID: req.ID}
		if minutes > 0 {
			grant.ExpiresAt = now.Add(time.Duration(minutes) * time.Minute)
		}
		if !n.putScopeGrant(policy.grants, grant) {
			book.mu.Unlock()
			return PermissionRequest{}, fmt.Errorf("permission approvals are unavailable while the integration is shutting down")
		}
		req.Status = "approved"
		req.GrantedMinutes = minutes
		req.GrantedUntil = grant.ExpiresAt
		until := "until the operator revokes it"
		if !grant.ExpiresAt.IsZero() {
			until = "until " + grant.ExpiresAt.Format("15:04") + " (local time)"
		}
		message = fmt.Sprintf("## Permission granted\n\nThe operator approved the **%s** permission %s (request %s). You can use it now.", req.Scope, until, req.ID)
	} else {
		req.Status = "denied"
		book.denied[req.Scope] = now
		message = fmt.Sprintf("## Permission denied\n\nThe operator did not approve the **%s** permission (request %s).", req.Scope, req.ID)
		if note != "" {
			message += " Their note: " + note
		}
		message += " Try another approach; asking again works only after a cooldown."
	}
	req.DecidedAt = now
	req.Note = note
	decided := *req
	book.mu.Unlock()
	if decided.Status == "approved" {
		n.hideAllAdapterActivity()
	}

	n.recordAudit("permission_decision", map[string]interface{}{
		"request_id": decided.ID, "scope": string(decided.Scope), "decision": decided.Status,
		"minutes": minutes, "note": note,
	})
	n.sendToNeuro(message, false)
	return decided, nil
}

// revokeGrant ends an operator approval early.
func (n *NDIntegration) revokeGrant(scope PermissionScope) bool {
	policy := n.policy()
	if policy == nil || !n.revokeScopeGrant(policy.grants, scope) {
		return false
	}
	n.hideAllAdapterActivity()
	n.recordAudit("permission_revoked", map[string]interface{}{"scope": string(scope)})
	n.sendToNeuro(fmt.Sprintf("## Permission revoked\n\nThe operator turned the **%s** permission off again. Stop using it.", scope), false)
	return true
}

// permissionRequestsSnapshot is what the dashboard shows in the permissions card.
func (n *NDIntegration) permissionRequestsSnapshot() map[string]interface{} {
	policy := n.policy()
	pending, decided := n.requests.list()
	if len(decided) > 20 {
		decided = decided[:20]
	}
	return map[string]interface{}{
		"pending":     pending,
		"recent":      decided,
		"grants":      policy.grants.active(),
		"requestable": requestableScopes(policy),
	}
}

// recordAudit writes an audit line when the auditor is wired (tests may not).
func (n *NDIntegration) recordAudit(kind string, fields map[string]interface{}) {
	if n == nil || n.audit == nil {
		return
	}
	n.audit.record(kind, fields)
}

// sendToNeuro posts a context message when the Neuro connection exists.
func (n *NDIntegration) sendToNeuro(message string, silent bool) {
	if n == nil || n.client == nil {
		return
	}
	if err := n.client.SendContext(message, silent); err != nil {
		log.Printf("could not tell Neuro about a permission decision: %v", err)
	}
}

func scopeNameList(scopes []PermissionScope) string {
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, string(scope))
	}
	return strings.Join(names, ", ")
}

// CmdRequestPermission is the action Neuro uses to ask for a permission.
const CmdRequestPermission CommandType = "request_permission"

// permissionRequestSpecs is registered in every mode. The scope enum is written
// out in full so that a small model can pick from a list instead of guessing.
func permissionRequestSpecs() []actionSpec {
	scopes := make([]interface{}, 0, len(allScopes()))
	for _, scope := range allScopes() {
		scopes = append(scopes, string(scope))
	}
	return []actionSpec{
		{
			Name: CmdRequestPermission,
			Description: "Ask the operator for a permission that the operator has switched on for requests, " +
				"when an action was refused because that scope is off. Returns at once. The decision comes later " +
				"as a message, so do not repeat the request. Example: " +
				`{"scope": "extensions", "reason": "I need the MCP bridge to read project files", "minutes": 30}`,
			Schema: neuro.WrapSchema(map[string]interface{}{
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        scopes,
					"description": "The permission scope to ask for",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One sentence: why you need it. The operator reads this.",
				},
				"minutes": map[string]interface{}{
					"type":        "integer",
					"description": "How long you need it, from 1 to 240 (default 30)",
				},
			}, []string{"scope", "reason"}),
		},
	}
}
