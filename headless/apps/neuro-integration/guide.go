package main

import (
	"fmt"
	"log"
	"sort"
	"strings"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Detailed instructions are requested by topic instead of consuming startup
// context. Names in this guide refer to the public action surface, not IPC.
func guideActionSpecs() []actionSpec {
	return []actionSpec{{
		Name:        CmdDesktopGuide,
		Description: "Get a short overview or help with desktop controls, scripts, games, shell commands, or permissions. Call this first when unsure which action or parameter to use.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"topic": map[string]interface{}{
				"type":        "string",
				"enum":        []interface{}{"overview", "desktop", "script", "games", "shell", "safety", "all"},
				"description": "Help topic (default overview); all returns every section",
			},
		}, nil),
	}}
}

// Use the actual registration when connected. Before registration, derive the
// same mode selection for local previews without claiming every action exists.
func (n *NDIntegration) guideActions() map[CommandType]bool {
	available := map[CommandType]bool{}
	if n != nil {
		if names := n.registeredActionNames(); len(names) > 0 {
			for _, name := range names {
				available[CommandType(name)] = true
			}
			return available
		}
	}
	actionModeMu.Lock()
	high, low, games := RegisterHLActionsOnStartup, RegisterLLActionsOnStartup, RegisterGameActionsOnStartup
	actionModeMu.Unlock()
	specs := alwaysRegisteredSpecs()
	if high {
		specs = append(specs, HLActionSpecs...)
	}
	if low {
		specs = append(specs, LLActionSpecs...)
	}
	if games {
		specs = append(specs, gameActionSpecs()...)
	}
	for _, spec := range specs {
		available[spec.Name] = true
	}
	return available
}

func (n *NDIntegration) desktopGuide(topic string) neuro.ExecutionResult {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		topic = "overview"
	}
	available := n.guideActions()
	sections := map[string]string{
		"overview": n.startupGuide(),
		"desktop":  desktopGuideSection(available),
		"script":   desktopScriptSection(available[CmdRunScript]),
		"games":    gameGuideSection(n, available),
		"shell":    shellGuideSection(),
		"safety":   safetyGuideSection(n),
	}
	if topic == "all" {
		return neuro.NewSuccessResult(boundedContextMessage(allGuideSections(available)))
	}
	if section, ok := sections[topic]; ok {
		return neuro.NewSuccessResult(boundedText(section, contextMessageMaxBytes))
	}
	return neuro.NewFailureResult(fmt.Sprintf("Unknown guide topic %q. Use overview, desktop, script, games, shell, safety, or all.", contextField(topic, 80)))
}

// "all" is an index with useful syntax, not a concatenated manual that silently
// loses its later sections to truncation. Each named topic supplies the detail.
func allGuideSections(available map[CommandType]bool) string {
	sections := []string{
		"## Desktop controls\nRequest get_desktop_context {} for fresh platform, screen, input availability, and application state. Use only registered controls. Coordinates start at top-left; y grows downward. Accepted work is queued; its outcome follows as a message.",
	}
	if available[CmdRunScript] {
		sections = append(sections, "## Script syntax\nrun_script {script:\"MOVE 640 360\\nCLICK left\\nCOMBO ctrl c\"}. One command per line; quote text; only whole-line # comments. WAIT runs once in source order. Limits: 64 KiB, 512 pending instructions, 20-second estimated delay plus a cooperative deadline. Request topic script for all commands and queue options.")
	} else {
		sections = append(sections, "## Script syntax\nScript input is not registered in this mode. Request topic desktop for the available controls.")
	}
	if available[CmdGameStartSession] {
		sections = append(sections, "## Games\nUse game_list_profiles, then game_start_session {profile_id}. Follow its control map with game_move, game_look, and game_action. game_observe requests state; game_end_session ends control. Dedicated integrations retain their input ownership.")
	} else {
		sections = append(sections, "## Games\nGame actions are not registered in this configuration.")
	}
	sections = append(sections,
		"## Shell commands\nshell_command {command:\"echo hello\"} needs the shell scope and program allowlist. No chaining, pipes, or redirection. Its output follows as a message. A timeout cannot undo side effects.",
		"## Permissions and recovery\nVedal approves permissions. request_permission {scope,reason,minutes} asks only for requestable scopes. Wait for approval; do not repeat denials. reset_controls {} clears queued input and releases held controls, including while paused. It cannot undo completed input.",
	)
	return strings.Join(sections, "\n\n")
}

func desktopGuideSection(available map[CommandType]bool) string {
	lines := []string{
		"## Desktop controls",
		"Use only registered actions. Coordinates are screen pixels: (0,0) is top-left; x grows right, y grows down.",
		"An accepted result means queued, not completed. Wait for a requested observation before deciding from screen state. Use a short script when input order matters.",
	}
	for _, item := range []struct {
		name CommandType
		help string
	}{
		{CmdGetDesktopContext, "get_desktop_context {}: request current platform, input availability, screen geometry, and application context. The text snapshot follows as a message."},
		{CmdMouseMove, "move_mouse_to {x,y}: move the pointer; mouse_click {button:\"left\"} clicks its current location."},
		{CmdTypeText, "type_text {text}: type printable ASCII into the focused control. Focus the target first; non-ASCII text is refused and needs shell_command."},
		{CmdKeyPress, "key_press {key:\"enter\"}: tap one key."},
		{CmdRunScript, "run_script {script:\"COMBO ctrl c\"}: combine keys or run a short Neurcro macro. Request desktop_guide {topic:\"script\"} for syntax."},
		{EnableLLControls, "enable_low_level_controls {}: switch to direct mouse, keyboard, and script controls."},
		{DisableLLControls, "disable_low_level_controls {}: switch to the high-level desktop actions already described in their schemas."},
	} {
		if available[item.name] {
			lines = append(lines, "- "+item.help)
		}
	}
	lines = append(lines, "- reset_controls {}: clear queued input and release held keys/buttons; its outcome follows as a message.")
	return strings.Join(lines, "\n")
}

func desktopScriptSection(available bool) string {
	if !available {
		return "## Script syntax\nScript input is not registered in this mode. Use the available desktop controls; request the desktop help topic to see the current mode."
	}
	return strings.TrimSpace(`
## Script syntax

Neurcro is Neuro Desktop’s macro language. Call run_script with a script string. One command per line; blank lines and whole-line # comments are ignored. Inline comments are not supported. Quote literal text and paths, escaping backslashes when needed.

`) + "\n```text\n" + strings.TrimSpace(`
TYPE "hello world"
PRESS enter
HOLD shift
RELEASE shift
HOLD_FOR w 0.5
COMBO ctrl c
RELEASE_ALL
MOVE 640 360
CLICK left
MOVE_REL 40 -10
WAIT 0.2
`) + "\n```\n" + strings.TrimSpace(`
SHORTCUT is an alias for COMBO. MOVE_N and CLICK_N use 0..1 coordinates. CLICK takes only an optional button; move first for absolute coordinates.

The entire script is checked before any input runs. Limits: 64 KiB UTF-8, 512 pending instructions, 20 seconds total estimated delay and a cooperative execution deadline. WAIT pauses once in source order. Runtime failures clear pending work and release held input; earlier OS side effects remain.

SHELL "echo hello" needs the shell scope and allowlist. LAUNCH opens a path/URL through the OS opener and needs the system scope. Neither runs while parsing. No loops, variables, conditions, or inline comments. Use execute_now:false with clear_after:false only when deliberately retaining a queue; execute_queue drains it and clear_action_queue discards it.
`)
}

func gameGuideSection(n *NDIntegration, available map[CommandType]bool) string {
	if !available[CmdGameStartSession] {
		return "## Games\nGame actions are not registered in this configuration. General desktop controls remain available according to their current mode and permissions."
	}
	lines := []string{
		"## Games",
		"Use game_list_profiles for configured IDs. game_detect inspects the current window/processes; game_start_session {profile_id,mode?} selects a game and sends its control map.",
		"- game_move {direction,seconds?}: forward, back, left, right, up, or down, using the profile's bindings.",
		"- game_look {dx,dy}: relative mouse-look; positive dx goes right and positive dy goes down.",
		"- game_action {action}: a named action such as jump or attack from the control map. game_press {keys:[\"e\"]} requires allow_raw_keys in the profile.",
		"- game_status: current session and owner. game_observe: window/game state and an approved vision description when configured; the text follows as a message.",
		"- game_end_session stops the session. game_release_all releases held input. Accepted work reports failures as messages.",
		"When input belongs to a dedicated integration, use its registered actions. Neuro Desktop must not compete with it. A session does not grant permissions.",
	}
	if n != nil && n.games != nil {
		profiles := n.games.Profiles()
		ids := make([]string, 0, len(profiles))
		for _, profile := range profiles {
			ids = append(ids, contextField(profile.ID, 48))
		}
		sort.Strings(ids)
		if len(ids) > 12 {
			ids = append(ids[:12], "… (use game_list_profiles for more)")
		}
		if len(ids) > 0 {
			lines = append(lines, "Configured profiles: "+strings.Join(ids, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

func shellGuideSection() string {
	return strings.TrimSpace(`
## Shell commands

shell_command {command:"echo hello"} runs one approved command line on the controlled PC. Its output follows as a message after acceptance. Use this for command-line work, including on headless machines.

The shell scope and effective program allowlist must allow it. Chaining, pipes, and shell redirections are refused. Prefer a program's own summary/limit options. timeout defaults to 20 seconds and is capped at 120; the executor transport may time out sooner. A timeout does not prove a command had no side effects; inspect before retrying.

If refused, correct a malformed argument once or request a requestable permission. Do not repeat a permission denial. Vedal controls the allowlist; this action cannot widen it.
`)
}

func safetyGuideSection(n *NDIntegration) string {
	lines := []string{
		"## Permissions and recovery",
		"Pause/kill switches, denied actions, permission scopes, rate limits, and per-extension grants apply before work runs. Acceptance does not grant permission; queued work is checked again.",
		"Scope examples: input for mouse/keyboard; vision for desktop metadata; game for game controls; extensions plus per-item delegation for extension management; shell for commands; system for launching or locking.",
		"A refused action explains the reason. Use another allowed action, or request_permission {scope,reason,minutes} if requests for that scope are enabled. Vedal makes the approval decision. Wait for it; never retry a pending request.",
		"reset_controls clears queued input and releases held keys/buttons, even while paused. It reports partial failure honestly; it cannot undo input or shell side effects that already happened.",
	}
	if n != nil && n.stop != nil {
		if reason := n.stop.blockReason(string(CmdMouseMove)); reason != "" {
			lines = append(lines, "Current stop state: "+contextField(reason, 160))
		}
	}
	return strings.Join(lines, "\n")
}

func (n *NDIntegration) startupGuide() string {
	return strings.TrimSpace(`
## Neuro Desktop connected

Use only the registered actions and their exact parameters. Request get_desktop_context {} for current platform, screen, input availability, and application state; its text snapshot follows as a message. This connection does not prove an executor or desktop input is ready.

An accepted result means work was queued, not completed. Use short ordered sequences; wait for requested observations before relying on screen state. Coordinates start at top-left, with y downward. A refusal explains the fix; do not repeat permission denials. Vedal approves permissions.

If input is stuck, use reset_controls {}. For details, request desktop_guide with topic desktop, script, games, shell, or safety. Help is available on demand; no full manual is sent automatically.
`)
}

func (n *NDIntegration) sendStartupGuide() {
	if !getEnvBool("NEURO_PROMPT_ASSIST", true) {
		return
	}
	n.contextDelivery.mu.Lock()
	defer n.contextDelivery.mu.Unlock()
	if n.contextDelivery.briefSent {
		return
	}
	if err := n.client.SendContext(n.startupGuide(), true); err != nil {
		log.Printf("Failed to send the startup guide: %v", err)
		return
	}
	n.contextDelivery.briefSent = true
}

// escapeHatchSpecs is the one action a model can always reach for when it is
// stuck. It takes no parameters, so a small model cannot get the call wrong.
func escapeHatchSpecs() []actionSpec {
	return []actionSpec{
		{
			Name:        CmdResetControls,
			Description: "Clear queued input and release held keys and mouse buttons, including while paused. Reports partial failure; completed effects cannot be undone. Example: {}",
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
