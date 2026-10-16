package main

// A port of desktop/backend/python/controller/shell.py. The rules, the messages and
// the output format are the same, so Neuro gets the same answers from either client.
// Two deliberate differences: the timeout is capped at maxTimeout even when the
// server asks for more, and a command that is still running after its timeout is
// killed with its process group (on Unix).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTimeout   = 20 * time.Second
	maxTimeout       = 120 * time.Second
	defaultMaxOutput = 4000
	maxMaxOutput     = 200000
	maxCommandChars  = 4000
	// captureLimit bounds what is kept in memory per stream while a command runs.
	// The reply is cut further by NEURO_SHELL_MAX_OUTPUT.
	captureLimit = 4 << 20
)

// builtinDenyPatterns is shell.py's BUILT_IN_DENY_PATTERNS, in the same order.
var builtinDenyPatterns = []string{
	`\brm\s+(-{1,2}[a-zA-Z-]+\s+)*/(\*)?(\s|$)`,
	`\bmkfs(\.\w+)?\b`,
	`\bdd\b[^\n]*\bof\s*=\s*/dev/`,
	`>\s*/dev/(sd|nvme|hd|disk)`,
	`\b(shutdown|reboot|poweroff|halt|init\s+0)\b`,
	`\bmkinitrd\b`,
	`\bchmod\s+(-[a-zA-Z]+\s+)*777\s+/\s*$`,
	`\bchown\s+(-[a-zA-Z]+\s+)*[^\s]+\s+/\s*$`,
	`:\(\)\s*\{.*\};\s*:`,
	`\bcurl\b[^\n]*\|\s*(ba|z|d|k)?sh\b`,
	`\bwget\b[^\n]*\|\s*(ba|z|d|k)?sh\b`,
	`\bhistory\s+-c\b`,
	`\bsudo\b`,
	`\bdoas\b`,
	`\bsu\s+-?\s*$`,
	`\bpasswd\b`,
	`\bvisudo\b`,
	`\bcrontab\s+-r\b`,
	`\breg\s+delete\b`,
	`\bdiskpart\b`,
	`\bformat\s+[a-zA-Z]:`,
}

// chainingPattern matches the shell operators the firewall refuses outright.
var chainingPattern = regexp.MustCompile("[;&|`<>\n\r]|\\$\\(|\\$\\{")

const (
	notConfiguredLocal = "Shell access is not configured: NEURO_SHELL_ALLOWLIST is empty, so no command may run. " +
		"Vedal can allow specific programs, e.g. NEURO_SHELL_ALLOWLIST=ls,cat,python3."
	notConfiguredServer = "Shell access is not configured on the server: its NEURO_SHELL_ALLOWLIST is empty, so no command " +
		"may run. Vedal can allow specific programs on the server, e.g. NEURO_SHELL_ALLOWLIST=ls,cat,python3."
	chainingMessage = "The shell firewall refused the command: it contains a shell operator (; & | < > ` $( or a newline). " +
		"Only one program per command is allowed, with plain arguments. Split it into separate shell_command calls, one step each."
)

// shellRefusal is a command the firewall or the environment refused. Its text is
// the reply Neuro sees.
type shellRefusal struct{ message string }

func (e *shellRefusal) Error() string { return e.message }

func refuse(format string, args ...interface{}) error {
	return &shellRefusal{message: fmt.Sprintf(format, args...)}
}

func floatEnv(name string, fallback, maximum float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func intEnv(name string, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func shellTimeout() time.Duration {
	return time.Duration(floatEnv("NEURO_SHELL_TIMEOUT", defaultTimeout.Seconds(), maxTimeout.Seconds()) * float64(time.Second))
}

func maxOutput() int {
	return intEnv("NEURO_SHELL_MAX_OUTPUT", defaultMaxOutput, maxMaxOutput)
}

func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func localAllowlist() []string { return splitList(os.Getenv("NEURO_SHELL_ALLOWLIST")) }

// denyPatterns is the built-in list plus NEURO_SHELL_DENYLIST.
func denyPatterns() []string {
	return append(append([]string(nil), builtinDenyPatterns...), splitList(os.Getenv("NEURO_SHELL_DENYLIST"))...)
}

// namesOf reads a JSON list of program names, as shell.py's _names does.
func namesOf(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return trimNames(typed)
		}
		return nil
	}
	var names []string
	for _, item := range items {
		if name := strings.TrimSpace(stringOf(item)); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func trimNames(items []string) []string {
	var names []string
	for _, item := range items {
		if name := strings.TrimSpace(item); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// firstToken returns the program name the command starts with, without a path or
// an extension, in lower case.
func firstToken(command string) string {
	stripped := strings.TrimSpace(command)
	if stripped == "" {
		return ""
	}
	parts, ok := splitShellWords(stripped, runtime.GOOS != "windows")
	if !ok {
		parts = strings.Fields(stripped)
	}
	if len(parts) == 0 {
		return ""
	}
	program := strings.ReplaceAll(parts[0], "\\", "/")
	if i := strings.LastIndex(program, "/"); i >= 0 {
		program = program[i+1:]
	}
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".ps1", ".sh"} {
		if strings.HasSuffix(strings.ToLower(program), suffix) {
			program = program[:len(program)-len(suffix)]
		}
	}
	return strings.ToLower(program)
}

// splitShellWords splits a command line the way shlex does. With posix set, quotes
// and backslashes are processed; without it they are kept, as shlex(posix=False)
// does. ok is false for an unbalanced quote, and the caller falls back to plain
// whitespace splitting, as shell.py does.
func splitShellWords(s string, posix bool) ([]string, bool) {
	var words []string
	var cur []rune
	inWord := false
	var quote rune
	escaped := false
	for _, r := range s {
		if escaped {
			cur = append(cur, r)
			escaped = false
			continue
		}
		if quote != 0 {
			switch {
			case r == quote:
				quote = 0
				if !posix {
					cur = append(cur, r)
				}
			case r == '\\' && posix && quote == '"':
				escaped = true
			default:
				cur = append(cur, r)
			}
			continue
		}
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				words = append(words, string(cur))
				cur = cur[:0]
				inWord = false
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
			if !posix {
				cur = append(cur, r)
			}
		case r == '\\' && posix:
			escaped = true
			inWord = true
		default:
			cur = append(cur, r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inWord {
		words = append(words, string(cur))
	}
	return words, true
}

// checkCommand returns the normalised command, or a *shellRefusal explaining why it
// may not run. policy is what the server sent with the command ("allowlist" and
// "denylist"). The server's policy is the authority; this machine's
// NEURO_SHELL_ALLOWLIST can only narrow it, never widen it.
func checkCommand(commandLine string, policy map[string]interface{}) (string, error) {
	command := strings.TrimSpace(commandLine)
	if command == "" {
		return "", refuse("The command is empty. Pass the command line as the first argument.")
	}
	if n := utf8.RuneCountInString(command); n > maxCommandChars {
		return "", refuse("The command is too long (%d characters, limit %d). Split it into steps.", n, maxCommandChars)
	}
	if chainingPattern.MatchString(command) {
		return "", refuse("%s", chainingMessage)
	}

	local := localAllowlist()
	var server, serverDenies []string
	serverSet := false
	if policy != nil {
		if raw, present := policy["allowlist"]; present && raw != nil {
			serverSet = true
			server = namesOf(raw)
			serverDenies = namesOf(policy["denylist"])
			if len(server) == 0 {
				return "", refuse("%s", notConfiguredServer)
			}
		}
	}
	if !serverSet && len(local) == 0 {
		return "", refuse("%s", notConfiguredLocal)
	}

	program := firstToken(command)
	type check struct {
		label string
		names []string
	}
	var checks []check
	if serverSet {
		checks = append(checks, check{"the server's shell allowlist", server})
	}
	if len(local) > 0 {
		checks = append(checks, check{"this machine's NEURO_SHELL_ALLOWLIST", local})
	}
	for _, c := range checks {
		allowed := map[string]bool{}
		for _, name := range c.names {
			allowed[strings.ToLower(name)] = true
		}
		if allowed["*"] {
			continue
		}
		if !allowed[program] {
			sorted := append([]string(nil), c.names...)
			sort.Strings(sorted)
			return "", refuse("Program '%s' is not on %s (%s). Vedal can add it with NEURO_SHELL_ALLOWLIST on the server.",
				program, c.label, strings.Join(sorted, ", "))
		}
	}

	patterns := append(denyPatterns(), serverDenies...)
	for _, pattern := range patterns {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return "", refuse("A blocked pattern is not a valid regular expression (%s).", pattern)
		}
		if re.MatchString(command) {
			return "", refuse("The command matches a blocked pattern and will not be run (pattern: %s). "+
				"Split it into smaller, non-destructive steps.", pattern)
		}
	}
	return command, nil
}

// runShell runs one command line after the checks and returns the summary Neuro
// reads: the exit code, then the stdout and stderr, each cut to the output limit.
func runShell(commandLine, cwd string, timeout time.Duration, policy map[string]interface{}) (string, error) {
	command, err := checkCommand(commandLine, policy)
	if err != nil {
		return "", err
	}
	if cwd == "" {
		cwd = os.Getenv("NEURO_SHELL_CWD")
	}
	if timeout <= 0 {
		timeout = shellTimeout()
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := newShellCommand(ctx, command)
	cmd.Dir = cwd
	stdout := &cappedBuffer{limit: captureLimit}
	stderr := &cappedBuffer{limit: captureLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// A grandchild that keeps the pipes open must not hold the reply forever.
	cmd.WaitDelay = 2 * time.Second
	configureProcessGroup(cmd)

	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", refuse("The command did not finish within %.0fs and was killed.", timeout.Seconds())
	}
	code := 0
	if runErr != nil && !errors.Is(runErr, exec.ErrWaitDelay) {
		var exitErr *exec.ExitError
		switch {
		case errors.As(runErr, &exitErr):
			// A non-zero exit is an answer, not an error.
		case errors.Is(runErr, fs.ErrNotExist) && cwd != "":
			return "", refuse("The working directory does not exist: %v", runErr)
		default:
			return "", refuse("Could not start the command: %v", runErr)
		}
	}
	if cmd.ProcessState != nil {
		code = exitCode(cmd.ProcessState)
	}
	return formatShellOutput(code, stdout, stderr, maxOutput()), nil
}

func formatShellOutput(code int, stdout, stderr *cappedBuffer, limit int) string {
	lines := []string{fmt.Sprintf("exit code: %d", code)}
	out := clip(stdout.String(), limit, stdout.overflow)
	errText := clip(stderr.String(), limit, stderr.overflow)
	if out != "" {
		lines = append(lines, "stdout:\n"+out)
	}
	if errText != "" {
		lines = append(lines, "stderr:\n"+errText)
	}
	if out == "" && errText == "" {
		lines = append(lines, "(no output)")
	}
	return strings.Join(lines, "\n")
}

// clip trims the text and keeps at most limit characters, with a note saying how
// many were cut. A stream that overflowed the capture buffer says so too.
func clip(text string, limit int, overflow int) string {
	text = strings.TrimSpace(strings.ToValidUTF8(text, "\uFFFD"))
	runes := []rune(text)
	if len(runes) <= limit && overflow == 0 {
		return text
	}
	kept := text
	if len(runes) > limit {
		kept = string(runes[:limit])
	}
	cut := len(runes) - limit
	if cut < 0 {
		cut = 0
	}
	return kept + fmt.Sprintf("\n… [%d more characters truncated]", cut+overflow)
}

// cappedBuffer keeps the first limit bytes of a stream and counts the rest.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := c.limit - c.buf.Len()
	if room > 0 {
		if len(p) <= room {
			c.buf.Write(p)
		} else {
			c.buf.Write(p[:room])
			c.overflow += len(p) - room
		}
	} else {
		c.overflow += len(p)
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

// shellCommand is the shell_command action: parameters, then the run, then the reply.
func (a *Agent) shellCommand(params map[string]interface{}) Result {
	commandLine, _ := params["command"].(string)
	if strings.TrimSpace(commandLine) == "" {
		return failure("shell_command needs a `command` string")
	}

	var timeout time.Duration
	if raw := params["timeout"]; !isFalsy(raw) {
		seconds, err := toFloat(raw)
		if err != nil {
			return failure("shell_command's timeout must be a number of seconds")
		}
		timeout = time.Duration(seconds * float64(time.Second))
	}

	var policy map[string]interface{}
	if raw, present := params["allowlist"]; present {
		denylist := params["denylist"]
		if isFalsy(denylist) {
			denylist = []interface{}{}
		}
		policy = map[string]interface{}{"allowlist": raw, "denylist": denylist}
	}

	cwd, _ := params["cwd"].(string)
	output, err := runShell(commandLine, cwd, timeout, policy)
	if err != nil {
		return failure(err.Error())
	}
	return success(map[string]interface{}{"output": output, "command": commandLine})
}

func toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, fmt.Errorf("not a number: %v", value)
	}
}
