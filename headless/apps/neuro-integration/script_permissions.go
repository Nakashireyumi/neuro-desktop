package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Keep these bounds and grammar in sync with controller/actions.py. A script is
// preflighted in full, before any instruction is admitted to the executor.
const maxScriptBytes = 64 * 1024
const maxScriptInstructions = 512
const maxScriptEstimatedSeconds = 20

type scriptPermissions struct {
	actions          map[string]bool
	system           bool
	shell            []string
	instructions     int
	estimatedSeconds float64
}

func (p *scriptPermissions) addAction(action CommandType) {
	if p.actions == nil {
		p.actions = map[string]bool{}
	}
	p.actions[string(action)] = true
}

func (p *scriptPermissions) merge(other scriptPermissions) {
	for action := range other.actions {
		p.addAction(CommandType(action))
	}
	p.system = p.system || other.system
	p.shell = append(p.shell, other.shell...)
	p.instructions += other.instructions
	p.estimatedSeconds += other.estimatedSeconds
}

func (p scriptPermissions) check(policy *PermissionPolicy) error {
	if policy != nil {
		for action := range p.actions {
			if !policy.IsAllowed(action) {
				return fmt.Errorf("Script needs %s: %s", action, policyDenialMessage(action, policy))
			}
		}
		if p.system && !policy.ScopeAllowed(ScopeSystem) {
			return fmt.Errorf("Script LAUNCH needs the `system` permission scope. Vedal can allow it under Permissions")
		}
	}
	for _, command := range p.shell {
		if err := checkShellCommand(command); err != nil {
			return fmt.Errorf("Script SHELL: %w", err)
		}
	}
	return nil
}

// splitScriptLines follows Python str.splitlines, including Unicode line
// separators. Treating only '\n' as a boundary would hide a later SHELL/LAUNCH
// from Go while the Python executor still ran it as its own instruction.
func splitScriptLines(script string) []string {
	var lines []string
	start := 0
	for i, r := range script {
		switch r {
		case '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
			if r == '\n' && i > 0 && script[i-1] == '\r' {
				start = i + 1
				continue
			}
			lines = append(lines, script[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(script) {
		lines = append(lines, script[start:])
	}
	return lines
}

// scriptTokens implements shlex.split(line, comments=False, posix=True), the
// executor grammar: quotes concatenate words, backslashes escape command names,
// and '#' is ordinary text except for a whole-line comment handled above it.
func scriptTokens(line string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote == '\'' {
			if c == quote {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if c == '\\' {
			if i+1 == len(line) {
				return nil, fmt.Errorf("unfinished escape")
			}
			i++
			if quote == '"' && line[i] != '\\' && line[i] != '"' {
				word.WriteByte('\\')
			}
			word.WriteByte(line[i])
			started = true
			continue
		}
		if quote == '"' {
			if c == quote {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case ' ', '\t', '\r', '\n':
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote")
	}
	if started {
		words = append(words, word.String())
	}
	return words, nil
}

func scriptIntentAction(command string) (CommandType, bool) {
	switch command {
	case "OPEN_START_MENU":
		return CmdOpenWindowsMenu, true
	case "OPEN_SETTINGS":
		return CmdOpenSettings, true
	}
	for _, action := range []CommandType{
		CmdOpenWindowsMenu, CmdShowDesktop, CmdMinimizeAll, CmdCloseForeground,
		CmdOpenTaskManager, CmdCloseAllApps, CmdOpenExplorer, CmdOpenRunDialog,
		CmdOpenSearch, CmdSnapWindowLeft, CmdSnapWindowRight, CmdOpenSettings,
		CmdOpenNotification, CmdOpenClipboard, CmdLockWorkstation,
		CmdSwitchAppNext, CmdSwitchAppPrevious, CmdOpenPowerUserMenu, CmdTakeScreenSnip,
	} {
		if scriptIntentFor(action) == command {
			return action, true
		}
	}
	return "", false
}

func validateScript(script string) (scriptPermissions, error) {
	var permissions scriptPermissions
	if len(script) > maxScriptBytes || !utf8.ValidString(script) {
		return permissions, fmt.Errorf("Script must be valid UTF-8 and at most 64 KiB")
	}
	if strings.TrimSpace(script) == "" {
		return permissions, fmt.Errorf("run_script needs a non-empty script string")
	}
	if strings.ContainsRune(script, 0) {
		return permissions, fmt.Errorf("Script cannot contain NUL characters")
	}
	for i, raw := range splitScriptLines(script) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		permissions.instructions++
		if permissions.instructions > maxScriptInstructions {
			return permissions, fmt.Errorf("Script exceeds 512 instructions")
		}
		tokens, err := scriptTokens(line)
		if err == nil {
			err = validateScriptInstruction(tokens, &permissions)
		}
		if err != nil {
			return permissions, fmt.Errorf("Script line %d: %w", i+1, err)
		}
		permissions.estimatedSeconds += estimateScriptInstruction(tokens)
		if permissions.estimatedSeconds > maxScriptEstimatedSeconds+1e-9 {
			return permissions, fmt.Errorf("Script exceeds the 20-second execution budget; split it into shorter scripts")
		}
	}
	return permissions, nil
}

func validateScriptInstruction(tokens []string, p *scriptPermissions) error {
	if len(tokens) == 0 {
		return nil
	}
	command := strings.ToUpper(tokens[0])
	args := tokens[1:]
	integer := func(value string) bool {
		_, err := strconv.ParseInt(value, 10, 32)
		return err == nil
	}
	number := func(value string, min, max float64, exclusiveMin bool) bool {
		n, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max && (!exclusiveMin || n > min)
	}
	button := func(value string) bool {
		switch strings.ToLower(value) {
		case "left", "right", "middle":
			return true
		}
		return false
	}
	if action, ok := scriptIntentAction(command); ok {
		if len(args) != 0 {
			return fmt.Errorf("%s takes no arguments", command)
		}
		p.addAction(action)
		return nil
	}
	valid := false
	switch command {
	case "TYPE":
		valid = len(args) >= 1
		p.addAction(CmdTypeText)
	case "ENTER":
		valid = len(args) == 0
		p.addAction(CmdKeyPress)
	case "PRESS", "HOLD", "RELEASE":
		valid = len(args) == 1 && args[0] != ""
		p.addAction(CmdKeyPress)
	case "SHORTCUT", "COMBO":
		valid = len(args) >= 1
		for _, key := range args {
			valid = valid && key != ""
		}
		p.addAction(CmdKeyCombo)
	case "HOLD_FOR":
		valid = len(args) == 2 && args[0] != "" && number(args[1], 0, 30, true)
		p.addAction(CmdKeyHoldFor)
	case "RELEASE_ALL":
		valid = len(args) == 0
	case "MOVE", "MOVE_REL":
		max := float64(30)
		if command == "MOVE_REL" {
			max = 2
		}
		valid = (len(args) == 2 || len(args) == 3) && integer(args[0]) && integer(args[1])
		if valid && len(args) == 3 {
			valid = number(args[2], 0, max, false)
		}
		p.addAction(CmdMouseMove)
		if command == "MOVE_REL" {
			p.addAction(CmdMoveMouseRelative)
		}
	case "MOVE_N":
		valid = len(args) == 2 && number(args[0], 0, 1, false) && number(args[1], 0, 1, false)
		p.addAction(CmdMouseMove)
	case "CLICK":
		valid = len(args) == 0 || (len(args) == 1 && button(args[0]))
		p.addAction(CmdMouseClick)
	case "CLICK_N":
		valid = (len(args) == 2 || len(args) == 3) && number(args[0], 0, 1, false) && number(args[1], 0, 1, false)
		if valid && len(args) == 3 {
			valid = button(args[2])
		}
		p.addAction(CmdMouseMove)
		p.addAction(CmdMouseClick)
	case "LINE":
		valid = len(args) == 4 || (len(args) == 6 && strings.EqualFold(args[4], "STEPS") && integer(args[5]))
		if valid {
			for _, value := range args[:4] {
				valid = valid && integer(value)
			}
			if len(args) == 6 {
				steps, _ := strconv.Atoi(args[5])
				valid = valid && steps >= 1 && steps <= 1000
			}
		}
		p.addAction(CmdMouseMove)
	case "PATH":
		valid = len(args) >= 2 && len(args) <= 512 && len(args)%2 == 0
		for _, value := range args {
			valid = valid && integer(value)
		}
		p.addAction(CmdMouseMove)
	case "LAUNCH":
		valid = len(args) >= 1 && strings.TrimSpace(strings.Join(args, " ")) != ""
		p.system = true
	case "SHELL":
		valid = len(args) >= 1 && strings.TrimSpace(strings.Join(args, " ")) != ""
		p.addAction(CmdShellCommand)
		p.shell = append(p.shell, strings.Join(args, " "))
	case "WAIT":
		valid = len(args) == 1 && number(args[0], 0, 30, false)
	default:
		return fmt.Errorf("unknown command %q; call desktop_guide for script syntax", command)
	}
	if !valid {
		return fmt.Errorf("invalid arguments for %s; call desktop_guide for script syntax", command)
	}
	return nil
}

// These are conservative known delays, not a prediction of process startup or
// shell work. The executor also enforces a monotonic deadline while draining
// the ordered queue, and limits a SHELL subprocess to its remaining time.
func estimateScriptInstruction(tokens []string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	seconds := 0.01 // bounded overhead per instruction
	command := strings.ToUpper(tokens[0])
	args := tokens[1:]
	number := func(value string) float64 { n, _ := strconv.ParseFloat(value, 64); return n }
	if _, ok := scriptIntentAction(command); ok {
		return seconds + 0.02
	}
	switch command {
	case "WAIT":
		seconds += number(args[0])
	case "HOLD_FOR":
		seconds += number(args[1])
	case "TYPE":
		seconds += float64(utf8.RuneCountInString(strings.Join(args, " "))) * 0.02
	case "PRESS", "ENTER":
		seconds += 0.02
	case "MOVE":
		duration := 0.1
		if len(args) == 3 {
			duration = number(args[2])
		}
		seconds += 1.5*duration + 0.14
	case "MOVE_N", "CLICK_N":
		seconds += 0.29
	case "MOVE_REL":
		if len(args) == 3 {
			seconds += number(args[2])
		}
	case "LINE":
		steps := 50
		if len(args) == 6 {
			steps, _ = strconv.Atoi(args[5])
		}
		seconds += float64(steps+1) * 0.02
	case "PATH":
		pairs := len(args) / 2
		if pairs == 1 {
			seconds += 0.02
		} else {
			seconds += float64((pairs-1)*31) * 0.02
		}
	}
	return seconds
}

func estimateExecutorInput(cmd IPCCommand) float64 {
	params := cmd.Params
	seconds := 0.01
	switch cmd.Type {
	case CmdTypeText:
		text, _ := params["text"].(string)
		seconds += float64(utf8.RuneCountInString(text)) * 0.02
	case CmdKeyPress:
		seconds += 0.02
	case CmdKeyHoldFor, CmdMouseHoldFor:
		seconds += numericParam(params, "seconds")
	case CmdMouseMove:
		duration := 0.1
		if _, ok := params["duration"]; ok {
			duration = numericParam(params, "duration")
		}
		seconds += 1.5*duration + 0.14
	case CmdMoveMouseRelative:
		seconds += numericParam(params, "duration")
	}
	return seconds
}

// Only these commands append/drain the executor's ordered input queue. Status,
// standalone shell commands and releasing held keys do not flush it.
func queuesExecutorInput(command CommandType) bool {
	switch command {
	case CmdMouseMove, CmdMoveMouseRelative, CmdMouseClick, CmdMouseHoldFor,
		CmdTypeText, CmdKeyPress, CmdKeyCombo, CmdKeyHoldFor, CmdRunScript:
		return true
	}
	return false
}

// prepareExecutorDispatch runs with ipcMu held, after waiting for earlier work.
// Permission changes and pauses between Validate and Execute are checked here;
// any retained script is checked again before a later action drains the queue.
func (n *NDIntegration) prepareExecutorDispatch(cmd *IPCCommand, action string) (scriptPermissions, error) {
	var additions scriptPermissions
	stopAction := action
	if stopAction == "" {
		stopAction = string(cmd.Type)
	}
	if reason := n.stop.blockReason(stopAction); reason != "" {
		return additions, fmt.Errorf("%s", reason)
	}
	policy := n.policy()
	if action != "" && policy != nil && !policy.IsAllowed(action) {
		return additions, fmt.Errorf("%s", policyDenialMessage(action, policy))
	}
	if cmd.Type == CmdShellCommand {
		if policy != nil && !policy.IsAllowed(string(CmdShellCommand)) {
			return additions, fmt.Errorf("%s", policyDenialMessage(string(CmdShellCommand), policy))
		}
		command, _ := cmd.Params["command"].(string)
		if err := checkShellCommand(command); err != nil {
			return additions, err
		}
	}
	if cmd.Type == CmdRunScript {
		script, ok := cmd.Params["script"].(string)
		if !ok {
			return additions, fmt.Errorf("run_script needs a script string")
		}
		var err error
		additions, err = validateScript(script)
		if err != nil {
			return additions, err
		}
		if err := additions.check(policy); err != nil {
			return additions, err
		}
		// Never inherit the executor machine's broader local shell policy.
		params := make(map[string]interface{}, len(cmd.Params)+2)
		for key, value := range cmd.Params {
			params[key] = value
		}
		allowlist, _, _, _ := loadShellRules()
		params["allowlist"] = append([]string{}, allowlist...)
		params["denylist"] = splitListEnv("NEURO_SHELL_DENYLIST")
		cmd.Params = params
	} else if queuesExecutorInput(cmd.Type) {
		additions.instructions = 1
		additions.estimatedSeconds = estimateExecutorInput(*cmd)
		if math.IsNaN(additions.estimatedSeconds) || math.IsInf(additions.estimatedSeconds, 0) || additions.estimatedSeconds < 0 {
			return additions, fmt.Errorf("Executor input duration must be a finite, non-negative number")
		}
	}
	if additions.instructions > 0 && action != "" {
		additions.addAction(CommandType(action))
	}
	if queuesExecutorInput(cmd.Type) && n.executorQueue.instructions+additions.instructions > maxScriptInstructions {
		return additions, fmt.Errorf("Executor queue exceeds 512 instructions; clear_action_queue before adding more")
	}
	if queuesExecutorInput(cmd.Type) && n.executorQueue.estimatedSeconds+additions.estimatedSeconds > maxScriptEstimatedSeconds+1e-9 {
		return additions, fmt.Errorf("Executor queue exceeds the 20-second execution budget; clear_action_queue and use shorter scripts")
	}
	if cmd.Type == CmdExecuteQueue || (queuesExecutorInput(cmd.Type) && cmd.ExecuteNow) {
		if err := n.executorQueue.check(policy); err != nil {
			return additions, fmt.Errorf("Queued %w; clear_action_queue to discard it", err)
		}
	}
	return additions, nil
}
