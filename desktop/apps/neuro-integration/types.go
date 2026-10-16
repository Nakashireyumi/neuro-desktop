package main

import (
	"sync"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Command types on the executor protocol (docs/EXECUTOR_PROTOCOL.md).
type CommandType string

type NDIntegration struct {
	// rate enforces the per-scope actions-per-minute budgets from the policy.
	rate            actionRateLimiter
	client          *neuro.Client
	ipcFilePath     string
	permissionsPath string
	executorHub     *ExecutorHub
	done            chan struct{}
	doneOnce        sync.Once
	ipcMu           sync.Mutex
	contextStopChan chan struct{}
	contextStopOnce sync.Once

	// permissions is swapped live by the operator dashboard, so every read goes
	// through policy() / every write through setPolicy().
	permissionsMu sync.RWMutex
	permissions   *PermissionPolicy

	// Per-extension approvals are separate from model-accessible install state.
	extensionPermissionsMu sync.RWMutex
	extensionMu            sync.Mutex
	adaptersMu             sync.Mutex
	adapters               *adapterRuntime

	// games holds the profile registry plus the live game session.
	games *GameRuntime

	// relay tracks the optional multiplexing relay (other integrations and
	// Neuro-OS watchers live behind it).
	relay *RelayState

	// stats powers the dashboard's counters.
	stats           *BridgeStats
	neuroConnection neuroConnectionState

	// stop is the operator's brake: pause flag plus an optional kill-switch file.
	stop *stopSwitch
	// audit records action decisions when NEURO_AUDIT_LOG is configured.
	audit *auditor

	// requests holds Neuro's permission requests and the operator's decisions.
	requests *requestBook
	// Temporary approvals expire without a permanent background polling loop.
	grantExpiry grantExpiryTimers

	// mcp runs the enabled MCP servers and tracks the tools they expose.
	mcp *mcpManager

	startedAt time.Time
}

// IPCCommand is one command for the executor.
type IPCCommand struct {
	Type       CommandType            `json:"type"`
	Params     map[string]interface{} `json:"params,omitempty"`
	ExecuteNow bool                   `json:"execute_now"`
	ClearAfter bool                   `json:"clear_after"`
}

// IPCResponse is the executor's answer to one command.
type IPCResponse struct {
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

func (n *NDIntegration) policy() *PermissionPolicy {
	n.permissionsMu.RLock()
	defer n.permissionsMu.RUnlock()
	return n.permissions
}

// setPolicy swaps the live policy. Operator approvals survive the swap, because
// the grant store belongs to the running bridge rather than to the policy file.
func (n *NDIntegration) setPolicy(policy *PermissionPolicy) {
	n.permissionsMu.Lock()
	if policy != nil {
		if n.permissions != nil && n.permissions.grants != nil {
			policy.grants = n.permissions.grants
		} else if policy.grants == nil {
			policy.grants = newGrantStore()
		}
	}
	n.permissions = policy
	n.permissionsMu.Unlock()
	n.hideAllAdapterActivity()
}

// BridgeStats are lightweight counters for the operator dashboard. They are
// intentionally in memory only: this is operator telemetry, not an audit trail.
type BridgeStats struct {
	mu            sync.Mutex
	actionsSeen   int
	actionsFailed int
	denied        int
	lastAction    string
	lastActionAt  time.Time
	connectedAt   time.Time
	reconnects    int
}

func newBridgeStats() *BridgeStats {
	return &BridgeStats{connectedAt: time.Now()}
}

func (s *BridgeStats) noteAction(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actionsSeen++
	s.lastAction = name
	s.lastActionAt = time.Now()
}

// noteAction records one action that reached the executor (or was accepted and
// is about to). Visibility for the dashboard, not an audit trail.
func (s *BridgeStats) noteFailure(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actionsFailed++
	s.lastAction = name
	s.lastActionAt = time.Now()
}

func (s *BridgeStats) noteDenied(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denied++
	s.lastAction = name
	s.lastActionAt = time.Now()
}

func (s *BridgeStats) noteReconnect() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconnects++
	s.connectedAt = time.Now()
}

func (s *BridgeStats) snapshot() map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]interface{}{
		"actions_seen":   s.actionsSeen,
		"actions_failed": s.actionsFailed,
		"actions_denied": s.denied,
		"last_action":    s.lastAction,
		"reconnects":     s.reconnects,
		"uptime_seconds": int(time.Since(s.connectedAt).Seconds()),
	}
	if !s.lastActionAt.IsZero() {
		out["last_action_at"] = s.lastActionAt.UTC().Format(time.RFC3339)
	}
	return out
}
