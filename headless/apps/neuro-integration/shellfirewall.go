package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The shell capability is the only path where Neuro Desktop runs a program the
// model asked for, so it is fenced in three places:
//
//  1. the `shell` permission scope (denied in the shipped example policy),
//  2. this firewall, which refuses commands before they reach the executor and
//     answers Neuro with a message that says how to fix it,
//  3. the Python executor, which re-checks the same rules because a script or a
//     relay watcher can bypass the action layer.
//
// The rules come from the environment so an operator can tighten them without a
// rebuild:
//
//	NEURO_SHELL_ALLOWLIST=ls,cat,python3   programs that may run (empty = nothing)
//	NEURO_SHELL_DENYLIST=^systemctl        extra patterns, on top of the built-ins
//	NEURO_SHELL_TIMEOUT=20                 seconds (max 120)
//	NEURO_SHELL_CWD=/srv/work              working directory
//
// `NEURO_SHELL_ALLOWLIST=*` allows any program and logs a startup warning: it is
// the operator explicitly accepting that Neuro has a shell.

// shellChainingPattern matches shell operators that let one allowlisted program
// run another command: ; & | < > backticks, $( and ${, and line breaks.
var shellChainingPattern = regexp.MustCompile("[;&|`<>\\n\\r]|\\$\\(|\\$\\{")

// builtInShellDenyPatterns are refused even when the program is allowlisted:
// unrecoverable or sideways actions rather than merely powerful ones.
var builtInShellDenyPatterns = []string{
	`\brm\s+(-{1,2}[a-zA-Z-]+\s+)*/(\*)?(\s|$)`,
	`\bmkfs(\.\w+)?\b`,
	`\bdd\b[^\n]*\bof\s*=\s*/dev/`,
	`>\s*/dev/(sd|nvme|hd|disk)`,
	`\b(shutdown|reboot|poweroff|halt|init\s+0)\b`,
	`\bchmod\s+(-[a-zA-Z]+\s+)*777\s+/\s*$`,
	`\bchown\s+(-[a-zA-Z]+\s+)*[^\s]+\s+/\s*$`,
	`:\(\)\s*\{.*\};\s*:`,
	`\b(curl|wget)\b[^\n]*\|\s*(ba|z|d|k)?sh\b`,
	`\bsudo\b`,
	`\bdoas\b`,
	`\bpasswd\b`,
	`\bvisudo\b`,
	`\bcrontab\s+-r\b`,
	`\breg\s+delete\b`,
	`\bdiskpart\b`,
	`\bformat\s+[a-zA-Z]:`,
}

var (
	shellRulesOnce sync.Once
	shellRules     struct {
		allowlist []string
		open      bool
		deny      []*regexp.Regexp
		err       error
	}
)

func loadShellRules() ([]string, bool, []*regexp.Regexp, error) {
	shellRulesOnce.Do(func() {
		allowlist := splitListEnv("NEURO_SHELL_ALLOWLIST")
		for _, program := range allowlist {
			if program == "*" {
				shellRules.open = true
			}
		}
		shellRules.allowlist = allowlist

		patterns := append([]string{}, builtInShellDenyPatterns...)
		patterns = append(patterns, splitListEnv("NEURO_SHELL_DENYLIST")...)
		for _, pattern := range patterns {
			compiled, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				shellRules.err = fmt.Errorf("invalid shell deny pattern %q: %w", pattern, err)
				return
			}
			shellRules.deny = append(shellRules.deny, compiled)
		}
	})
	return shellRules.allowlist, shellRules.open, shellRules.deny, shellRules.err
}

// resetShellRules clears the memoised rules; used by tests.
func resetShellRules() {
	shellRulesOnce = sync.Once{}
	shellRules.allowlist = nil
	shellRules.open = false
	shellRules.deny = nil
	shellRules.err = nil
}

func splitListEnv(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// shellProgramName reduces "/usr/bin/ls -la" to "ls" so the allowlist is about
// programs, not paths or flags.
func shellProgramName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	if quoted := command; strings.HasPrefix(quoted, `"`) || strings.HasPrefix(quoted, `'`) {
		// Windows style: "C:\Program Files\app.exe" --flag
		quote := quoted[:1]
		if end := strings.Index(command[1:], quote); end >= 0 {
			command = command[1 : end+1]
		}
	} else if index := strings.IndexAny(command, " \t"); index >= 0 {
		command = command[:index]
	}

	command = strings.ReplaceAll(command, "\\", "/")
	if index := strings.LastIndex(command, "/"); index >= 0 {
		command = command[index+1:]
	}
	lowered := strings.ToLower(command)
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".ps1", ".sh", ".py"} {
		if strings.HasSuffix(lowered, suffix) {
			lowered = strings.TrimSuffix(lowered, suffix)
			break
		}
	}
	return lowered
}

// checkShellCommand applies the firewall. An empty error means the command may
// be attempted.
func checkShellCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("The command is empty. Pass the command line in the `command` parameter, e.g. \"ls -la\".")
	}
	if len(command) > 4000 {
		return fmt.Errorf("The command is too long (%d characters, limit 4000). Split it into steps.", len(command))
	}
	// One program per command. Chaining, pipes, redirection and substitution
	// would let an allowlisted program run anything, so they are refused.
	if shellChainingPattern.MatchString(command) {
		return fmt.Errorf("The shell firewall refused the command: it contains a shell operator (; & | < > ` $( or a newline). " +
			"Only one program per command is allowed, with plain arguments. " +
			"Split it into separate shell_command calls, one step each.")
	}

	allowlist, open, deny, err := loadShellRules()
	if err != nil {
		return err
	}
	if len(allowlist) == 0 {
		return fmt.Errorf(
			"Shell access is not configured: NEURO_SHELL_ALLOWLIST is empty, so no command may run. " +
				"Vedal can allow specific programs, e.g. NEURO_SHELL_ALLOWLIST=ls,cat,python3 — and must " +
				"also enable the `shell` permission scope in the dashboard.")
	}

	program := shellProgramName(command)
	if !open {
		allowed := false
		for _, candidate := range allowlist {
			if strings.EqualFold(strings.TrimSpace(candidate), program) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf(
				"Program %q is not on the shell allowlist (%s). Vedal can add it with NEURO_SHELL_ALLOWLIST.",
				program, strings.Join(allowlist, ", "))
		}
	}

	for _, pattern := range deny {
		if pattern.MatchString(command) {
			return fmt.Errorf(
				"The command was blocked by the shell firewall (pattern %s). "+
					"Split it into smaller, non-destructive steps.", pattern.String())
		}
	}

	return nil
}

// shellCWD returns the configured working directory, when it exists.
func shellCWD() (string, error) {
	raw := strings.TrimSpace(os.Getenv("NEURO_SHELL_CWD"))
	if raw == "" {
		return "", nil
	}
	info, err := os.Stat(raw)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("NEURO_SHELL_CWD=%s is not a directory", raw)
	}
	return filepath.Clean(raw), nil
}

// shellTimeoutSeconds mirrors the Python default/limit so Neuro gets an answer
// before the executor would time out.
func shellTimeoutSeconds(requested float64) float64 {
	if requested <= 0 {
		requested = 20
	}
	if requested > 120 {
		requested = 120
	}
	return requested
}

// policyDenialMessage explains a refusal in the shape a small model can act on:
// which scope, how to turn it on, and what to do instead.
func policyDenialMessage(action string, policy *PermissionPolicy) string {
	scope, _ := scopeForAction(action)
	if scope == "" {
		return fmt.Sprintf(
			"Action %q is denied by the permission policy (explicit deny list). "+
				"Vedal can allow it in the dashboard under Permissions → Explicitly Denied Actions.", action)
	}

	scopeName := string(scope)
	fix := fmt.Sprintf(
		"Vedal can allow it in the dashboard under Permissions → %s scope.", scopeName)
	if scope == ScopeShell {
		fix += " The shell scope also needs NEURO_SHELL_ALLOWLIST to name the program."
	}
	if policy.ScopeRequestable(scope) {
		fix += fmt.Sprintf(" You can ask for it: call request_permission with scope %q, a one-sentence reason and minutes, then wait for the decision message.", scopeName)
	}

	return fmt.Sprintf(
		"Action %q is denied: the `%s` permission scope is currently off. %s", action, scopeName, fix)
}

// safetySnapshot summarises the fences for the dashboard and for a model that
// asks get_status: what is on, what is off, and where the operator's knobs are.
func (n *NDIntegration) safetySnapshot() map[string]interface{} {
	if n == nil {
		return map[string]interface{}{}
	}

	allowlist, open, deny, err := loadShellRules()
	shell := map[string]interface{}{
		"allowlist":       allowlist,
		"allowlist_open":  open,
		"deny_patterns":   len(deny),
		"timeout_seconds": shellTimeoutSeconds(0),
	}
	if err != nil {
		shell["error"] = err.Error()
	}
	if cwd, err := shellCWD(); err == nil && cwd != "" {
		shell["cwd"] = cwd
	}

	// The safety objects are always wired in Run(), but status is also served by
	// tests and partial constructions, so read them defensively.
	var (
		paused     bool
		killFile   string
		killActive bool
	)
	if n.stop != nil {
		paused = n.stop.pausedNow()
		killFile = n.stop.killSwitchFile
		killActive = n.stop.fileKillActive()
	}

	var auditStatus map[string]interface{}
	if n.audit != nil {
		auditStatus = n.audit.status()
	} else {
		auditStatus = map[string]interface{}{"enabled": false}
	}

	return map[string]interface{}{
		"paused":              paused,
		"kill_switch_file":    killFile,
		"kill_switch_active":  killActive,
		"hard_denied_actions": sortedBoolKeys(hardDeniedActions()),
		"shell":               shell,
		"audit":               auditStatus,
	}
}

func sortedBoolKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
