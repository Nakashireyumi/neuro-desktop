package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The safety layer is what stands between "Neuro asked for it" and "it happened".
//
// Layers, in the order an action passes through them:
//
//	1. paused flag            — /api/control/pause, or NEURO_PAUSED=1 at startup
//	2. kill switch file       — a file that exists means stop (NEURO_KILL_SWITCH_FILE)
//	3. hard deny list         — actions that no dashboard edit can enable
//	4. permission scope       — Vedal's dashboard policy
//	5. rate limit             — per-scope actions-per-minute budget
//	6. shell firewall         — allowlist/denylist for command lines
//
// Every decision (allowed or refused) can be written to an audit log
// (NEURO_AUDIT_LOG) so a streamer can answer "what did it actually do?".

// safeDuringStop are the actions that still run while paused or killed: they
// release input and report state. Everything else is refused.
var safeDuringStop = map[string]bool{
	string(CmdClearActionQueue):   true,
	string(CmdKeyReleaseAll):      true,
	string(CmdGetStatus):          true,
	string(CmdSendDesktopContext): true,
	string(CmdGameReleaseAll):     true,
	string(CmdGameStatus):         true,
	string(CmdGameEndSession):     true,
	string(CmdDesktopGuide):       true,
	string(CmdResetControls):      true,
}

// hardDeniedActions comes from the environment and is merged into every policy:
// unlike the dashboard's deny list, an operator (or a stray dashboard request)
// cannot turn it off while the bridge is running.
func hardDeniedActions() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("NEURO_DENY_ACTIONS"))
	if raw == "" {
		return nil
	}
	out := map[string]bool{}
	for _, name := range strings.Split(raw, ",") {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed != "" {
			out[trimmed] = true
		}
	}
	return out
}

// hardDenied reports whether the action is blocked by the operator's hard list.
func (p *PermissionPolicy) hardDenied(action string) bool {
	if p != nil && p.hardDeny[strings.ToLower(action)] {
		return true
	}
	return false
}

// applyHardDeny merges the environment deny list into the policy.
func (p *PermissionPolicy) applyHardDeny(names map[string]bool) {
	if p == nil {
		return
	}
	p.hardDeny = names
}

// stopSwitch answers whether actions are currently blocked, and why.
type stopSwitch struct {
	paused atomic.Bool

	// killSwitchFile, when set, means "an action is blocked while this file
	// exists". The operator decides what creates it (OBS, a stream deck, a
	// chat command, an ops script) — the bridge only honours it.
	killSwitchFile string

	// lastCheck memoises the file stat so a hot action loop does not hit the
	// filesystem on every call.
	mu         sync.Mutex
	checkedAt  time.Time
	fileActive bool
}

func newStopSwitch() *stopSwitch {
	sw := &stopSwitch{
		killSwitchFile: strings.TrimSpace(os.Getenv("NEURO_KILL_SWITCH_FILE")),
	}
	sw.paused.Store(getEnvBool("NEURO_PAUSED", false))
	return sw
}

func (s *stopSwitch) pausedNow() bool {
	if s == nil {
		return false
	}
	return s.paused.Load()
}

func (s *stopSwitch) setPaused(paused bool) {
	if s == nil {
		return
	}
	s.paused.Store(paused)
}

// fileKillActive reports whether the kill-switch file exists (checked at most
// once a second).
func (s *stopSwitch) fileKillActive() bool {
	if s == nil || s.killSwitchFile == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.checkedAt) < time.Second {
		return s.fileActive
	}

	_, err := os.Stat(s.killSwitchFile)
	s.fileActive = err == nil
	s.checkedAt = time.Now()
	return s.fileActive
}

// blockReason returns a message for Neuro when the action must not run.
func (s *stopSwitch) blockReason(action string) string {
	if safeDuringStop[action] {
		return ""
	}
	if s.fileKillActive() {
		return fmt.Sprintf(
			"Neurcro is stopped: the kill switch file %s exists. "+
				"Remove that file (or ask the operator) before trying %q again.",
			s.killSwitchFile, action)
	}
	if s.pausedNow() {
		return fmt.Sprintf(
			"Neurcro is paused by the operator, so %q is refused. "+
				"Vedal can resume it from the dashboard (Pause / Resume).", action)
	}
	return ""
}

func (s *stopSwitch) status() map[string]interface{} {
	if s == nil {
		return map[string]interface{}{"paused": false, "kill_switch_file": ""}
	}
	return map[string]interface{}{
		"paused":             s.pausedNow(),
		"paused_at_start":    s.pausedNow(),
		"kill_switch_file":   s.killSwitchFile,
		"kill_switch_active": s.fileKillActive(),
	}
}

// ---------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------

// auditor appends JSON lines to NEURO_AUDIT_LOG. It is best-effort: a failed
// audit write must never take the bridge down, but it is also never silently
// claimed to have worked (the error lands in the log).
type auditor struct {
	path string

	mu  sync.Mutex
	err string
}

func newAuditor() *auditor {
	return &auditor{path: strings.TrimSpace(os.Getenv("NEURO_AUDIT_LOG"))}
}

func (a *auditor) record(event string, fields map[string]interface{}) {
	if a == nil || a.path == "" {
		return
	}

	entry := map[string]interface{}{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"event": event,
	}
	for key, value := range fields {
		entry[key] = value
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	// Open per write rather than holding the file. A held handle keeps the file
	// locked on Windows, so nothing else can delete or rotate the log while the
	// bridge runs. Audit writes are rare, so reopening costs nothing that shows.
	if dir := filepath.Dir(a.path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	file, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		a.err = err.Error()
		return
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		a.err = err.Error()
	}
}

func (a *auditor) status() map[string]interface{} {
	if a == nil {
		return map[string]interface{}{"enabled": false}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]interface{}{
		"enabled": a.path != "",
		"path":    a.path,
		"error":   a.err,
	}
}
