package main

import (
	"sync"
	"time"
)

// At most one one-shot timer exists per scope. This lock also serializes an
// expiry's privacy/runtime cleanup with a replacement grant: a callback whose
// timer already fired cannot clear a newer approval's viewer state.
//
// Lock order is expiry -> grantStore. Release grantStore before touching adapter
// or MCP state; neither callback path takes requestBook.mu.
type grantExpiryTimers struct {
	mu     sync.Mutex
	closed bool
	timers map[PermissionScope]*scopeGrantExpiry
}

type scopeGrantExpiry struct {
	store *grantStore
	grant ScopeGrant
	timer *time.Timer
}

// putScopeGrant installs the grant and its timer as one operation. A permanent
// approval cancels any previous timer without creating another one.
func (n *NDIntegration) putScopeGrant(store *grantStore, grant ScopeGrant) bool {
	if n == nil || store == nil || !knownScope(grant.Scope) {
		return false
	}
	expiry := &n.grantExpiry
	expiry.mu.Lock()
	defer expiry.mu.Unlock()
	if expiry.closed {
		return false
	}
	if expiry.timers == nil {
		expiry.timers = map[PermissionScope]*scopeGrantExpiry{}
	}
	if previous := expiry.timers[grant.Scope]; previous != nil {
		previous.timer.Stop()
		delete(expiry.timers, grant.Scope)
	}
	store.mu.Lock()
	if store.grants == nil {
		store.grants = map[PermissionScope]ScopeGrant{}
	}
	store.grants[grant.Scope] = grant
	delay := grant.ExpiresAt.Sub(store.clock())
	store.mu.Unlock()
	if grant.ExpiresAt.IsZero() {
		return true
	}
	registration := &scopeGrantExpiry{store: store, grant: grant}
	expiry.timers[grant.Scope] = registration
	registration.timer = time.AfterFunc(delay, func() { n.expireScopeGrant(registration) })
	return true
}

func sameScopeGrant(a, b ScopeGrant) bool {
	return a.Scope == b.Scope && a.RequestID == b.RequestID &&
		a.GrantedAt.Equal(b.GrantedAt) && a.ExpiresAt.Equal(b.ExpiresAt)
}

func (n *NDIntegration) expireScopeGrant(registration *scopeGrantExpiry) {
	expiry := &n.grantExpiry
	expiry.mu.Lock()
	defer expiry.mu.Unlock()
	scope := registration.grant.Scope
	if expiry.closed || expiry.timers[scope] != registration {
		return
	}
	store := registration.store
	store.mu.Lock()
	current, present := store.grants[scope]
	if present && !sameScopeGrant(current, registration.grant) {
		// A direct store replacement must not be revoked by a stale callback.
		delete(expiry.timers, scope)
		store.mu.Unlock()
		return
	}
	if present {
		if delay := current.ExpiresAt.Sub(store.clock()); delay > 0 {
			// A changed clock may make the callback early. Wait once until the
			// actual deadline instead of polling or expiring a live approval.
			registration.timer.Stop()
			registration.timer = time.AfterFunc(delay, func() { n.expireScopeGrant(registration) })
			store.mu.Unlock()
			return
		}
		delete(store.grants, scope)
	}
	store.mu.Unlock()
	delete(expiry.timers, scope)
	// The grant may already have been lazily pruned by a permission read. Its
	// timer still owns the expiry cleanup, including erasing private history.
	n.hideAllAdapterActivity()
	n.syncMCPServers()
	n.recordAudit("permission_expired", map[string]interface{}{"scope": string(scope), "request_id": registration.grant.RequestID})
}

func (n *NDIntegration) revokeScopeGrant(store *grantStore, scope PermissionScope) bool {
	if n == nil || store == nil {
		return false
	}
	expiry := &n.grantExpiry
	expiry.mu.Lock()
	defer expiry.mu.Unlock()
	registration := expiry.timers[scope]
	if registration != nil {
		registration.timer.Stop()
		delete(expiry.timers, scope)
	}
	// Count a pending expiry as a revocation even if a read already pruned its
	// expired grant, so the caller still performs privacy cleanup.
	return store.revoke(scope) || registration != nil
}

// stopGrantExpiryTimers waits for an in-progress expiry's cleanup, cancels every
// pending timer, and rejects further approvals. Close calls this before stopping
// MCP processes, so a queued callback cannot restart one during shutdown.
func (n *NDIntegration) stopGrantExpiryTimers() {
	if n == nil {
		return
	}
	expiry := &n.grantExpiry
	expiry.mu.Lock()
	defer expiry.mu.Unlock()
	expiry.closed = true
	for scope, registration := range expiry.timers {
		registration.timer.Stop()
		delete(expiry.timers, scope)
	}
}
