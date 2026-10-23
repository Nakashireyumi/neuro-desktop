package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the shipping Python client over the real executor socket, with a
// fake Neuro backend. This catches protocol drift that two separate unit suites
// and the fake executor cannot see. No display or GUI dependencies are used.
func TestRealHeadlessAgentActionFlow(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python is not installed")
	}
	t.Setenv("NEURO_HEADLESS", "1")
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	t.Setenv("NEURO_SHELL_DENYLIST", "")
	t.Setenv("NEURO_DENY_ACTIONS", "")
	t.Setenv("NEURO_EXECUTOR_TOKEN", "")
	t.Setenv("NEURO_PAUSED", "0")
	t.Setenv("NEURO_KILL_SWITCH_FILE", "")
	t.Setenv("NEURO_AUDIT_LOG", filepath.Join(t.TempDir(), "audit.jsonl"))
	resetShellRules()
	t.Cleanup(resetShellRules)

	policyPath := filepath.Join(t.TempDir(), "permissions.json")
	writeFile(t, policyPath, allowEverythingPolicy)
	backend := startFakeNeuro(t)
	hub := newHub(t, "")
	integration, err := NewNDIntegration(IntegrationOptions{
		WSURL: backend.url(), GameName: "Neuro Desktop", PermissionsPath: policyPath,
		IPCPath: filepath.Join(t.TempDir(), "ipc.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	integration.executorHub = hub
	if err := integration.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = integration.Close() })

	logPath := filepath.Join(t.TempDir(), "agent.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	command := exec.Command(python, "-m", "controller.agent", "--bridge", hub.Addr())
	command.Dir = filepath.Join("..", "..", "..", "desktop", "python")
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("Python client: %s", data)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for !hub.Info().Connected && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !hub.Info().Connected {
		t.Fatal("the real Python client did not connect")
	}
	if hub.Info().Version != bridgeProtocolVersion {
		t.Fatalf("client protocol version missing from dashboard telemetry: %+v", hub.Info())
	}

	response, err := hub.SendCommand(IPCCommand{Type: CmdGetStatus, Params: map[string]interface{}{}}, 5*time.Second)
	if err != nil || response == nil || !response.Success || response.Data["headless"] != true {
		t.Fatalf("expected headless telemetry from Python: %#v, %v", response, err)
	}

	backend.waitFor("actions/register", 5*time.Second)
	backend.sendAction("real-shell", "shell_command", `{"command":"echo nd-real-agent-output"}`)
	result := backend.waitForActionResult("real-shell", 5*time.Second)
	if result["success"] != true {
		t.Fatalf("shell action failed: %v", result)
	}
	backend.waitForContextContaining("nd-real-agent-output", 5*time.Second)

	admin := newTestAdmin(t, integration, testDashboardToken)
	statusCode, _ := sendJSON(t, http.MethodPost, admin.URL+"/api/control/pause", "", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("could not pause: HTTP %d", statusCode)
	}
	backend.sendAction("paused-shell", "shell_command", `{"command":"echo should-not-run"}`)
	result = backend.waitForActionResult("paused-shell", 5*time.Second)
	if result["success"] != false || !strings.Contains(result["message"].(string), "paused") {
		t.Fatalf("pause did not block the action: %v", result)
	}
	statusCode, _ = sendJSON(t, http.MethodPost, admin.URL+"/api/games/release", "", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("input release must work while paused: HTTP %d", statusCode)
	}
	statusCode, _ = sendJSON(t, http.MethodPost, admin.URL+"/api/control/resume", "", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("could not resume: HTTP %d", statusCode)
	}
	backend.sendAction("resumed-shell", "shell_command", `{"command":"echo nd-resumed-output"}`)
	if result = backend.waitForActionResult("resumed-shell", 5*time.Second); result["success"] != true {
		t.Fatalf("resume did not allow the action: %v", result)
	}
	backend.waitForContextContaining("nd-resumed-output", 5*time.Second)
}
