package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHeadlessAnswerFollowsTheEnvironment(t *testing.T) {
	t.Setenv("NEURO_HEADLESS", "1")
	a := NewAgent()
	if !a.headless || a.headlessReason != "NEURO_HEADLESS is set" {
		t.Fatalf("NEURO_HEADLESS=1 must be headless: %+v", a)
	}
	t.Setenv("NEURO_HEADLESS", "0")
	if NewAgent().headless {
		t.Fatal("NEURO_HEADLESS=0 must force a display")
	}
}

func TestInputIsRefusedWithAReasonThatSendsNeuroToTheShell(t *testing.T) {
	t.Setenv("NEURO_HEADLESS", "1")
	result := NewAgent().Execute(map[string]interface{}{
		"type":   "move_mouse_to",
		"params": map[string]interface{}{"x": 1.0, "y": 2.0},
	})
	if result.Success || !strings.Contains(result.Error, "display session") || !strings.Contains(result.Error, "shell_command") {
		t.Fatalf("headless input must be refused with the no-display reason: %+v", result)
	}

	t.Setenv("NEURO_HEADLESS", "0")
	result = NewAgent().Execute(map[string]interface{}{
		"type":   "key_press",
		"params": map[string]interface{}{"key": "a"},
	})
	if result.Success || !strings.Contains(result.Error, "not in the Go client yet") ||
		!strings.Contains(result.Error, "neuro-client") {
		t.Fatalf("with a display, unported input must say where it still runs: %+v", result)
	}
}

func TestStatusHasTheKeysTheServerReads(t *testing.T) {
	result := NewAgent().Execute(map[string]interface{}{"type": "get_status"})
	if !result.Success {
		t.Fatal(result.Error)
	}
	for _, key := range []string{
		"status", "timestamp", "headless", "platform", "active_window", "open_windows",
		"running_processes", "recent_actions", "screen", "mouse_position", "screenshot_png_b64",
	} {
		if _, ok := result.Data[key]; !ok {
			t.Errorf("status lacks %q, which the server reads", key)
		}
	}
	if result.Data["status"] != "running" {
		t.Errorf("status = %v", result.Data["status"])
	}
	if _, ok := result.Data["headless"].(bool); !ok {
		t.Errorf("headless must be a boolean: %v", result.Data["headless"])
	}
}

func TestParamsMustBeAnObjectWhenTheyAreGiven(t *testing.T) {
	a := NewAgent()
	result := a.Execute(map[string]interface{}{"type": "get_status", "params": "nope"})
	if result.Success || result.Error != "params must be an object" {
		t.Fatalf("got %+v", result)
	}
	// An empty list is falsy in Python, so the agent reads it as no params at all.
	if result := a.Execute(map[string]interface{}{"type": "get_status", "params": []interface{}{}}); !result.Success {
		t.Fatalf("an empty params list should be read as none: %+v", result)
	}
}

func TestLifecycleAndQueueCommands(t *testing.T) {
	a := NewAgent()
	for _, name := range []string{"shutdown_gracefully", "shutdown_immediately"} {
		result := a.Execute(map[string]interface{}{"type": name})
		if !result.Success || result.Data["shutdown"] != true {
			t.Errorf("%s: %+v", name, result)
		}
	}
	for _, name := range []string{"execute_queue", "clear_action_queue"} {
		if result := a.Execute(map[string]interface{}{"type": name}); !result.Success {
			t.Errorf("%s should succeed with nothing queued: %+v", name, result)
		}
	}
	result := a.Execute(map[string]interface{}{"type": "heartbeat"})
	if !result.Success || result.Data["heartbeat"] != "alive" {
		t.Errorf("heartbeat: %+v", result)
	}
}

func TestUnknownCommandsSayTheBridgeHandlesThem(t *testing.T) {
	result := NewAgent().Execute(map[string]interface{}{"type": "game_observe"})
	if result.Success || !strings.Contains(result.Error, "is not an executor command") {
		t.Fatalf("got %+v", result)
	}
}

// TestCommandSetsMatchThePythonAgent fails when the Python agent runs a command
// this client neither handles nor refuses, so a gap cannot be silent. It reads the
// list from agent.py, the same way the Python side reads the Go list.
func TestCommandSetsMatchThePythonAgent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "backend", "python", "controller", "agent.py"))
	if err != nil {
		t.Fatalf("cannot read the Python agent: %v", err)
	}
	block := regexp.MustCompile(`(?s)EXECUTOR_COMMANDS = \((.*?)\n\)`).FindSubmatch(data)
	if block == nil {
		t.Fatal("EXECUTOR_COMMANDS not found in agent.py")
	}
	python := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z_]+)"`).FindAllSubmatch(block[1], -1) {
		python[string(m[1])] = true
	}
	if len(python) < 10 {
		t.Fatalf("read only %d commands from agent.py; the pattern probably broke", len(python))
	}

	for name := range python {
		_, handled := handlers[name]
		if !handled && !unportedCommands[name] {
			t.Errorf("the Python agent runs %q, and this client neither handles nor refuses it", name)
		}
	}
	for name := range handlers {
		// heartbeat is answered by the Python agent but is not in its list.
		if !python[name] && name != "heartbeat" {
			t.Errorf("%q is handled here, but the Python agent does not list it", name)
		}
	}
	for name := range unportedCommands {
		if !python[name] {
			t.Errorf("%q is refused here, but the Python agent does not list it", name)
		}
	}
}
