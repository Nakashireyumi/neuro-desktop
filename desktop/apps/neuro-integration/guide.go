package main

import (
	"fmt"
	"log"
	"sort"
	"strings"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// The guide exists for small models. A 2B-parameter model that is handed 50
// action names and nothing else will pick the wrong one, invent parameters, or
// forget that it must start a session before it can move. Two things fix most of
// that, and both live here:
//
//   - one plain-text cheat sheet (this file), available as the `desktop_guide`
//     action and pushed once as context after startup,
//   - error messages that name the scope, the fix, and the next action to call
//     (see policyDenialMessage and rateLimitDenial).
//
// The rule of thumb for the wording: two sentences or fewer per action, the
// exact parameter names, one example, and an explicit order of operations.

// CmdDesktopGuide is the "tell me how to use you" action.
const guideActionHelp = "Returns a short instruction sheet for using Neuro Desktop: which actions exist, in what order to call them, and what each permission scope allows"

// guideActionSpecs is always registered: it is the cheapest way to make a weak
// model behave, and it costs one action slot.
func guideActionSpecs() []actionSpec {
	return []actionSpec{
		{
			Name:        CmdDesktopGuide,
			Description: guideActionHelp + ". Call this first if you are unsure which action to use, or after a refusal.",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"topic": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"all", "desktop", "games", "shell", "safety"},
					"description": "Which part of the guide to return (default all)",
				},
			}, nil),
		},
	}
}

func (n *NDIntegration) desktopGuide(topic string) neuro.ExecutionResult {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		topic = "all"
	}

	sections := map[string]string{
		"desktop": desktopGuideSection(),
		"games":   gameGuideSection(n),
		"shell":   shellGuideSection(),
		"safety":  safetyGuideSection(n),
	}

	if topic == "all" {
		order := []string{"desktop", "games", "shell", "safety"}
		parts := make([]string, 0, len(order))
		for _, key := range order {
			parts = append(parts, sections[key])
		}
		return neuro.NewSuccessResult(strings.Join(parts, "\n\n"))
	}

	section, ok := sections[topic]
	if !ok {
		keys := make([]string, 0, len(sections))
		for key := range sections {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return neuro.NewFailureResult(fmt.Sprintf(
			"Unknown guide topic %q. Use one of: all, %s.", topic, strings.Join(keys, ", ")))
	}
	return neuro.NewSuccessResult(section)
}

func desktopGuideSection() string {
	return strings.TrimSpace(`
## Neuro Desktop — how to use it

Rules that matter more than any single action:

1. Call ONE action at a time and wait for its result before the next one.
2. If an action is refused, the message says which permission scope is off. Do
   not retry it; either ask Vedal to enable that scope, or use another action.
3. Coordinates are screen pixels: x grows to the right, y grows DOWN.
   (0,0) is the top-left corner. Read the screen size from get_status first if
   you are unsure.
4. Type text with type_text, press keys with key_press, combine keys with
   key_combo (e.g. ctrl+c). Use run_script only when you need a sequence.

Everyday actions:

- get_status -> mouse position, screen size, active window, running processes.
- move_mouse_to {x, y} -> moves the pointer (no click).
- mouse_click {button: "left"|"right"|"middle"} -> clicks where the pointer is.
- type_text {text} -> types into whatever has focus. Click the target first.
- key_press {key} -> one key, e.g. "enter", "esc", "f1".
- key_combo {keys: ["ctrl","c"]} -> keys pressed together.
- run_script {script} -> several lines at once; see the script section below.
- desktop_guide -> this text.
- reset_controls -> the escape hatch: clears queued inputs and releases every held key and button. Use it when something is stuck. It takes no parameters.
- list_catalog_items / find_catalog_items -> extensions and plugins.
- desktop_context -> what the desktop looks like right now (may include a screenshot).
`)
}

// desktopScriptSection documents the script language next to the guide so a weak
// model that only reads one action's output still learns the syntax.
func desktopScriptSection() string {
	return strings.TrimSpace(`
Script lines (run_script, one per line):

TYPE "hello world"       type text
PRESS enter              tap a key
HOLD shift               hold a key down
RELEASE shift            let it go
HOLD_FOR w 1.5           hold for N seconds (max 30) — game movement
COMBO ctrl shift s       keys together
SHORTCUT ctrl c          same as COMBO
RELEASE_ALL              release everything (use when unsure)
MOVE 640 360             move the pointer
MOVE_REL 40 -10          move relative to now (mouse-look)
CLICK left               click a button
WAIT 0.5                 pause
SHELL "ls -la"           run a command line (needs the shell scope)
LAUNCH notepad           start a program (needs the system scope)
`)
}

func gameGuideSection(n *NDIntegration) string {
	lines := []string{
		"## Playing a game",
		"",
		"Order of operations: game_detect -> game_start_session -> game_move / game_look /",
		"game_action / game_press -> game_status when unsure -> game_end_session.",
		"",
		"- game_list_profiles -> the games configured here, with their keybinds.",
		"- game_detect -> which game is running right now (uses the window title and processes).",
		"- game_start_session {profile_id, mode?} -> begin playing; you then receive the",
		"  control map (which key is forward, jump, ...). mode may be auto, nd, external",
		"  or hybrid.",
		"- game_move {direction: \"forward\"|\"back\"|\"left\"|\"right\"|\"jump\"|\"sneak\"|\"sprint\",",
		"  seconds?} -> holds that direction like a player would (never leaves a key down).",
		"- game_look {dx, dy} -> turns the camera; positive dx turns right, positive dy looks down.",
		"- game_action {action: \"attack\"|\"use\"|\"inventory\"|...} -> one named action from",
		"  the profile.",
		"- game_press {keys: [\"e\"]} -> raw keys; only allowed when the profile sets allow_raw_keys.",
		"- game_observe -> a screenshot plus a vision summary, delivered as context. Use it",
		"  when you need to know what is on screen.",
		"- game_release_all -> drop every key/button immediately. Use it when something",
		"  seems stuck.",
		"- game_end_session -> stop playing.",
		"",
		"If the game has its own dedicated integration connected through the relay, game_move",
		"answers that input is delegated: do not try to move, use that integration's actions.",
	}
	if n != nil && n.games != nil {
		profiles := n.games.Profiles()
		if len(profiles) > 0 {
			ids := make([]string, 0, len(profiles))
			for _, profile := range profiles {
				ids = append(ids, profile.ID)
			}
			sort.Strings(ids)
			lines = append(lines, "", "Configured profiles: "+strings.Join(ids, ", ")+".")
		}
	}
	return strings.Join(lines, "\n")
}

func shellGuideSection() string {
	return strings.TrimSpace(`
## Command line (shell_command)

- shell_command {command} -> runs one command line and returns its exit code and output.
- On a headless machine (no desktop), this is the main thing you can do; mouse and
  keyboard actions will answer that there is no display.
- Only programs on the operator's allowlist may run (NEURO_SHELL_ALLOWLIST). If the
  answer says a program is not allowlisted, do not retry: ask Vedal, or use another
  action.
- Commands are killed after the timeout (default 20s, max 120s), and destructive
  patterns (rm -rf /, mkfs, shutdown, curl | sh, sudo, ...) are always blocked.
- Output is truncated, so prefer commands that print a summary, e.g. ls -1 | head.
`)
}

func safetyGuideSection(n *NDIntegration) string {
	lines := []string{
		"## Safety and permissions",
		"",
		"Every action passes a gate before it runs:",
		"",
		"1. Pause / kill switch — the operator can stop everything instantly.",
		"2. Hard deny list — some actions are disabled outside the dashboard.",
		"3. Permission scopes — input, game, shell, filesystem, process, network, system, vision.",
		"4. Rate limit — a scope may be budgeted to N actions per minute.",
		"5. Shell firewall — programs must be allowlisted and match no blocked pattern.",
		"",
		"Scopes that are commonly off: system (shutdown, lock, launching programs),",
		"shell (command lines), filesystem (extension install). If an action is refused with a",
		"scope name, tell Vedal which scope to enable instead of retrying.",
	}
	if n != nil {
		if reason := n.stop.blockReason(string(CmdMouseMove)); reason != "" {
			lines = append(lines, "", "RIGHT NOW: "+reason)
		}
		if n.stop.fileKillActive() {
			lines = append(lines, "", "The kill switch file is present, so only safety actions run.")
		}
	}
	return strings.Join(lines, "\n")
}

// startupGuide is the short version pushed as context after registering actions.
// It is deliberately small: a weak model reads the first few lines and little
// else, so the first few lines are the important ones.
func (n *NDIntegration) startupGuide() string {
	return strings.TrimSpace(`
## Neuro Desktop is connected

You can control this desktop. The essentials:

- Work one action at a time; wait for each result.
- Mouse: move_mouse_to {x,y} then mouse_click. y grows downward.
- Keyboard: type_text, key_press, key_combo {keys:[...]}.
- Games: game_detect, then game_start_session, then game_move / game_look / game_action.
- Command line: shell_command {command} (only if the shell scope and allowlist allow it).
- Unsure? Call desktop_guide for the full instruction sheet, or get_status for the
  current screen, window and processes.
- A refusal names the permission scope that is off. Do not retry it.
`) + "\n\n" + desktopScriptSection()
}

// sendStartupGuide pushes the guide once per connection, silently.
func (n *NDIntegration) sendStartupGuide() {
	if !getEnvBool("NEURO_PROMPT_ASSIST", true) {
		return
	}
	if err := n.client.SendContext(n.startupGuide(), true); err != nil {
		log.Printf("Failed to send the startup guide: %v", err)
	}
}

// escapeHatchSpecs is the one action a model can always reach for when it is
// stuck. It takes no parameters, so a small model cannot get the call wrong.
func escapeHatchSpecs() []actionSpec {
	return []actionSpec{
		{
			Name:        CmdResetControls,
			Description: "Escape hatch: use this when something is stuck, a queued input keeps running, or you are unsure what to do. It clears queued inputs that have not run yet, releases every held key and mouse button, and reports the result. It takes no parameters and starts nothing. Example: {}",
			Schema:      nil,
		},
	}
}

// resetControls is the escape hatch. Queued inputs are cleared first, so
// nothing runs after the release, and then every held key and button is
// released. It starts nothing and enables no scope, which is why it is always
// allowed (alwaysAllowed) and still runs while the bridge is stopped
// (safeDuringStop).
func (n *NDIntegration) resetControls() neuro.ExecutionResult {
	queue := IPCCommand{Type: CmdClearActionQueue, ExecuteNow: true, ClearAfter: true}
	queueResult := neuro.NewSuccessResult("Cleared queued inputs.")
	if resp, err := n.sendToExecutor(queue); err != nil {
		queueResult = neuro.NewFailureResult(fmt.Sprintf("Could not clear queued inputs: %v.", err))
	} else if !resp.Success {
		queueResult = neuro.NewFailureResult(nonEmptyOr(resp.Error, "Could not clear queued inputs") + ".")
	}
	return combineResetResults(queueResult, n.releaseAllInput())
}

// combineResetResults builds the outcome of reset_controls. It says what
// worked and what did not. When something failed it says what that means for
// the next input, because a model cannot act on "partly worked" alone.
func combineResetResults(queueResult, releaseResult neuro.ExecutionResult) neuro.ExecutionResult {
	const next = " Next: call desktop_guide if you are unsure what to do, and send one action at a time."
	if queueResult.Successful && releaseResult.Successful {
		return neuro.NewSuccessResult("Controls reset: queued inputs are cleared and every held key and button is released." + next)
	}
	var problems []string
	if !queueResult.Successful {
		problems = append(problems, "Queued inputs may still run ("+queueResult.Message+").")
	}
	if !releaseResult.Successful {
		problems = append(problems, "Held keys or buttons may still be down ("+releaseResult.Message+").")
	}
	return neuro.NewFailureResult("Controls reset only partly worked. " + strings.Join(problems, " ") + next)
}
