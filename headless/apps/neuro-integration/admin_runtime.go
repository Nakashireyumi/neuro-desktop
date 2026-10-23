package main

import (
	"net/http"
	"net/url"
	"os"
	"time"
)

// handleRuntime serves GET /api/runtime: what is running right now. The
// dashboard's Extensions page shows this first. Every field is a live check,
// not a configuration echo, and no field carries a secret (URLs lose their
// user information before they are returned).
func (a *AdminServer) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	integration := a.integration
	now := time.Now().UTC()

	executor := ExecutorInfo{}
	if integration.executorHub != nil {
		executor = integration.executorHub.Info()
	}
	relay := integration.relay.status()
	visionService := integration.visionServiceSnapshot()
	vision := visionStatus{Configured: visionService.Configured, URL: visionService.Endpoint}
	visionState := "disabled"
	if visionService.Effective {
		vision = probeVisionServer(false)
		visionState = visionRuntimeState(vision)
	}
	servers := integration.mcpSnapshotForRuntime()

	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"checked_at": now.Format(time.RFC3339),
		"neuro":      integration.neuroConnection.snapshot(),
		"bridge": map[string]interface{}{
			"state":          "running",
			"pid":            os.Getpid(),
			"started_at":     integration.startedAt.UTC().Format(time.RFC3339),
			"uptime_seconds": int(time.Since(integration.startedAt).Seconds()),
			"admin_listen":   a.addr,
			"paused":         integration.stop.pausedNow(),
		},
		"executor": map[string]interface{}{
			"state":     executorState(executor.Connected),
			"connected": executor.Connected,
		},
		"relay": map[string]interface{}{
			"state":      relayRuntimeState(relay),
			"enabled":    relay.Enabled,
			"url":        redactURL(relay.URL),
			"connected":  relay.Connected,
			"registered": relay.Registered,
			"peer_count": relay.PeerCount,
			"last_error": relay.LastError,
		},
		"vision": map[string]interface{}{
			"state":      visionState,
			"enabled":    visionService.Enabled,
			"effective":  visionService.Effective,
			"actors":     visionService.Actors,
			"persona":    visionService.Persona,
			"reason":     visionService.Reason,
			"configured": vision.Configured,
			"url":        redactURL(vision.URL),
			"reachable":  vision.Reachable,
			"backend":    vision.Backend,
			"latency_ms": vision.LatencyMS,
			"error":      vision.Error,
			"checked_at": vision.CheckedAt,
		},
		"mcp": map[string]interface{}{
			"state":   mcpRuntimeState(servers),
			"running": countMCPRunning(servers),
			"servers": servers,
		},
	})
}

func executorState(connected bool) string {
	if connected {
		return "connected"
	}
	return "waiting"
}

// relayRuntimeState collapses the relay link into the four states the dashboard
// shows: disabled, disconnected, connected (socket open, not yet registered),
// registered (routing traffic), and idle (registered with nothing behind it).
func relayRuntimeState(status RelayStatus) string {
	switch {
	case !status.Enabled:
		return "disabled"
	case !status.Connected:
		return "disconnected"
	case !status.Registered:
		return "connected"
	case status.PeerCount == 0:
		return "idle"
	default:
		return "registered"
	}
}

func visionRuntimeState(status visionStatus) string {
	switch {
	case !status.Configured:
		return "not_configured"
	case status.Reachable:
		return "reachable"
	default:
		return "unreachable"
	}
}

func mcpRuntimeState(servers []mcpServerStatus) string {
	if len(servers) == 0 {
		return "off"
	}
	running, starting := 0, 0
	for _, server := range servers {
		switch server.State {
		case "running":
			running++
		case "starting":
			starting++
		}
	}
	switch {
	case running > 0:
		return "running"
	case starting > 0:
		return "starting"
	default:
		return "failed"
	}
}

func countMCPRunning(servers []mcpServerStatus) int {
	count := 0
	for _, server := range servers {
		if server.State == "running" {
			count++
		}
	}
	return count
}

// redactURL drops user information and query strings, which can carry tokens.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "(unparseable)"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
