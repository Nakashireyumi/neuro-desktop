package main

import "fmt"

// An observation may wait for the executor and an external inference service.
// Keep checking the approval that started it before collecting or disclosing
// anything. A replacement policy or temporary approval requires a new request.
type observationPermit struct {
	integration *NDIntegration
	policy      *PermissionPolicy
	action      CommandType
	grant       ScopeGrant
	temporary   bool
}

func (n *NDIntegration) observationPermit(action CommandType) (*observationPermit, error) {
	p := &observationPermit{integration: n, policy: n.policy(), action: action}
	if p.policy != nil && !p.policy.scopes[ScopeVision].Allowed {
		p.temporary = true
		p.grant = currentVisionGrant(p.policy)
	}
	return p, p.check()
}

func currentVisionGrant(policy *PermissionPolicy) ScopeGrant {
	if policy == nil || policy.grants == nil {
		return ScopeGrant{}
	}
	policy.grants.mu.Lock()
	defer policy.grants.mu.Unlock()
	return policy.grants.grants[ScopeVision]
}

func (p *observationPermit) check() error {
	n := p.integration
	if p.policy == nil || n.policy() != p.policy {
		return fmt.Errorf("observation permission changed; request a new observation")
	}
	if !p.policy.ScopeAllowed(ScopeVision) || !p.policy.IsAllowed(string(p.action)) {
		return fmt.Errorf("%s needs the vision permission", p.action)
	}
	if p.temporary && !sameScopeGrant(p.grant, currentVisionGrant(p.policy)) {
		return fmt.Errorf("the observation approval changed; request a new observation")
	}
	if reason := n.stop.blockReason(string(p.action)); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	return nil
}
