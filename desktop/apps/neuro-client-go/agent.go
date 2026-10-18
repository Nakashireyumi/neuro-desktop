package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// unportedCommands are the Python agent's input, screen and script commands
// (controller/agent.py, EXECUTOR_COMMANDS) that this client refuses. Every other
// Python command is handled in dispatch. The parity test in agent_test.go checks
// that the two sides cover the same commands.
var unportedCommands = map[string]bool{
	"move_mouse_to":       true,
	"move_mouse_relative": true,
	"mouse_click":         true,
	"mouse_hold_for":      true,
	"type_text":           true,
	"key_press":           true,
	"key_combo":           true,
	"key_hold_for":        true,
	"key_release_all":     true,
	"run_script":          true,
}

// inputNotPorted is appended to the name of a refused command when there is a
// display. Without one, the no-display message is used instead, as in Python.
const inputNotPorted = " is not in the Go client yet: mouse, keyboard, screen, window and script commands still run only in the Python client (neuro-client). Run that client on this machine for them."

// Agent executes commands against this machine. This slice has no input, so the
// only state is the headless answer.
type Agent struct {
	headless       bool
	headlessReason string
}

// Result is the outcome of one command, shaped like the Python agent's replies.
type Result struct {
	Success bool
	Data    map[string]interface{}
	Error   string
}

// NewAgent reads the headless state once, as the Python agent does at start-up.
func NewAgent() *Agent {
	a := &Agent{headless: isHeadless()}
	if a.headless {
		a.headlessReason = describeHeadlessReason()
	}
	return a
}

// headlessRequested is NEURO_HEADLESS set to a true value.
func headlessRequested() bool {
	return truthy(os.Getenv("NEURO_HEADLESS"), "1", "true", "yes", "on")
}

// displayAvailable is the same best-effort answer as controller/gui_stub.py:
// Windows and macOS are assumed to have a session; Linux needs DISPLAY or
// WAYLAND_DISPLAY. NEURO_HEADLESS can force either answer.
func displayAvailable() bool {
	forced := strings.ToLower(strings.TrimSpace(os.Getenv("NEURO_HEADLESS")))
	switch forced {
	case "0", "false", "no", "off":
		return true
	case "1", "true", "yes", "on":
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func isHeadless() bool {
	return headlessRequested() || !displayAvailable()
}

func describeHeadlessReason() string {
	if headlessRequested() {
		return "NEURO_HEADLESS is set"
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return "no DISPLAY/WAYLAND_DISPLAY in this session"
	}
	return "no desktop session available"
}

// noDisplayMessage is the refusal the Python agent gives for input on a headless
// machine (gui_stub.py), so Neuro sees the same text from either client.
func noDisplayMessage(reason string) string {
	return "This machine has no display session (" + reason + "), so mouse/keyboard actions cannot run here. " +
		"Use the shell_command action for command-line work, or run the executor on the desktop machine with " +
		"`python3 -m controller.agent --bridge <bridge>:9876`."
}

// Execute runs one command envelope ({"type": ..., "params": {...}}).
func (a *Agent) Execute(command map[string]interface{}) (result Result) {
	defer func() {
		if r := recover(); r != nil {
			result = failure(fmt.Sprintf("internal error: %v", r))
		}
	}()

	name := strings.TrimSpace(stringOf(command["type"]))
	params := map[string]interface{}{}
	if raw := command["params"]; !isFalsy(raw) {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return failure("params must be an object")
		}
		params = m
	}
	return a.dispatch(name, params)
}

// handlers are the commands this client runs. The parity test checks this table
// against the Python agent's command list.
var handlers map[string]func(*Agent, map[string]interface{}) Result

func init() {
	handlers = map[string]func(*Agent, map[string]interface{}) Result{
		"shell_command": (*Agent).shellCommand,
		"get_status": func(a *Agent, params map[string]interface{}) Result {
			return success(a.status(params))
		},
		"heartbeat": func(a *Agent, _ map[string]interface{}) Result {
			return success(map[string]interface{}{"heartbeat": "alive", "timestamp": timestamp()})
		},
		// This client queues nothing, so there is nothing to run or clear.
		"execute_queue": func(a *Agent, _ map[string]interface{}) Result { return success(nil) },
		"clear_action_queue": func(a *Agent, _ map[string]interface{}) Result {
			return success(nil)
		},
		"shutdown_gracefully": func(a *Agent, _ map[string]interface{}) Result {
			return success(map[string]interface{}{"shutdown": true})
		},
		"shutdown_immediately": func(a *Agent, _ map[string]interface{}) Result {
			return success(map[string]interface{}{"shutdown": true})
		},
	}
}

func (a *Agent) dispatch(name string, params map[string]interface{}) Result {
	if handle, ok := handlers[name]; ok {
		return handle(a, params)
	}
	if unportedCommands[name] {
		if a.headless {
			return failure(noDisplayMessage(a.headlessReason))
		}
		return failure("'" + name + "'" + inputNotPorted)
	}
	return failure(fmt.Sprintf("'%s' is not an executor command; the bridge handles it. "+
		"Check that the bridge and agent are the same version.", name))
}

// status returns the same keys as the Python agent's get_status, so the server
// reads it the same way. Fields this client cannot fill are null or empty, and
// `unported` says which ones.
func (a *Agent) status(params map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"status":             "running",
		"timestamp":          timestamp(),
		"headless":           a.headless,
		"platform":           platformName(),
		"active_window":      nil,
		"open_windows":       []string{},
		"running_processes":  []string{},
		"recent_actions":     []interface{}{},
		"screen":             nil,
		"mouse_position":     nil,
		"screenshot_png_b64": nil,
		"client":             "go",
		"unported":           []string{"input", "screen", "windows", "processes", "run_script"},
	}
}

func platformName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func success(data map[string]interface{}) Result {
	return Result{Success: true, Data: data}
}

func failure(message string) Result {
	return Result{Success: false, Error: message}
}

func truthy(value string, accepted ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, want := range accepted {
		if value == want {
			return true
		}
	}
	return false
}

func stringOf(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// isFalsy follows Python's truthiness for the JSON values a command can carry.
func isFalsy(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case bool:
		return !v
	case float64:
		return v == 0
	case string:
		return v == ""
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	default:
		return false
	}
}
