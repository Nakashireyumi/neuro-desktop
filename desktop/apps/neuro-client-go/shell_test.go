package main

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFirewallRefusesEmptyAndChainedCommands(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "ls,echo")
	cases := map[string]string{
		"":               "is empty",
		"ls; rm -rf x":   "shell operator",
		"echo $(whoami)": "shell operator",
		"echo hi\nls":    "shell operator",
		"ls > out.txt":   "shell operator",
		"echo ${HOME}":   "shell operator",
	}
	for command, want := range cases {
		_, err := checkCommand(command, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("checkCommand(%q): want an error containing %q, got %v", command, want, err)
		}
	}
}

func TestFirewallRefusesVeryLongCommands(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	_, err := checkCommand("echo "+strings.Repeat("a", 4000), nil)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("a command over 4000 characters must be refused, got %v", err)
	}
}

func TestFirewallNeedsAnAllowlistAndHonoursIt(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "")
	if _, err := checkCommand("ls -la", nil); err == nil || !strings.Contains(err.Error(), "NEURO_SHELL_ALLOWLIST is empty") {
		t.Fatalf("with no allowlist nothing may run, got %v", err)
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	if _, err := checkCommand("ls -la", nil); err == nil || !strings.Contains(err.Error(), "Program 'ls' is not on this machine's") {
		t.Fatalf("a program that is not allowed must be refused, got %v", err)
	}
	if got, err := checkCommand("  echo hi  ", nil); err != nil || got != "echo hi" {
		t.Fatalf("an allowed command should pass, got %q, %v", got, err)
	}
}

func TestServerPolicyIsTheAuthorityAndTheLocalListOnlyNarrows(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "")
	if _, err := checkCommand("echo hi", map[string]interface{}{"allowlist": []interface{}{}}); err == nil ||
		!strings.Contains(err.Error(), "not configured on the server") {
		t.Fatalf("an empty server allowlist must refuse whatever this machine says, got %v", err)
	}

	// The server allows echo. A local list that leaves echo out narrows it.
	t.Setenv("NEURO_SHELL_ALLOWLIST", "ls")
	policy := map[string]interface{}{"allowlist": []interface{}{"echo"}}
	if _, err := checkCommand("echo hi", policy); err == nil {
		t.Fatal("the local list must narrow the server's: echo is not on it")
	}

	// A local list cannot widen the server's: ls is on the local list, not the server's.
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo,ls")
	if _, err := checkCommand("ls", policy); err == nil || !strings.Contains(err.Error(), "the server's shell allowlist") {
		t.Fatalf("the local list must not widen the server's, got %v", err)
	}
	if _, err := checkCommand("echo hi", policy); err != nil {
		t.Fatalf("echo is on both lists, so it should pass: %v", err)
	}
}

func TestBuiltInDenyPatternsApplyEvenToAllowedPrograms(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "sudo,ls,rm,shutdown")
	for _, command := range []string{"sudo ls", "rm -rf /", "shutdown now"} {
		if _, err := checkCommand(command, nil); err == nil || !strings.Contains(err.Error(), "blocked pattern") {
			t.Errorf("%q must hit a deny pattern, got %v", command, err)
		}
	}
}

func TestEnvironmentDenylistAddsPatterns(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	t.Setenv("NEURO_SHELL_DENYLIST", "forbidden")
	if _, err := checkCommand("echo forbidden", nil); err == nil || !strings.Contains(err.Error(), "pattern: forbidden") {
		t.Fatalf("NEURO_SHELL_DENYLIST must apply, got %v", err)
	}
}

func TestFirstTokenIsTheProgramName(t *testing.T) {
	cases := map[string]string{
		"ls -la":          "ls",
		"/usr/bin/ls -la": "ls",
		"  Echo.EXE  hi":  "echo",
		"python3.sh x":    "python3",
		"":                "",
	}
	for command, want := range cases {
		if got := firstToken(command); got != want {
			t.Errorf("firstToken(%q) = %q, want %q", command, got, want)
		}
	}
	if runtime.GOOS != "windows" {
		if got := firstToken(`"my tool" arg`); got != "my tool" {
			t.Errorf("a quoted program name: got %q", got)
		}
	}
}

func TestSplitShellWordsRefusesAnUnbalancedQuote(t *testing.T) {
	if _, ok := splitShellWords("echo 'unterminated", true); ok {
		t.Fatal("an unbalanced quote must not split")
	}
	words, ok := splitShellWords(`echo "a b" c`, true)
	if !ok || len(words) != 3 || words[1] != "a b" {
		t.Fatalf("quoted words: got %q (ok=%v)", words, ok)
	}
}

func TestShellRunsAndReportsTheExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo,sh")
	out, err := runShell("echo hi", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "exit code: 0\nstdout:\nhi" {
		t.Fatalf("got %q", out)
	}
	out, err = runShell(`sh -c "exit 3"`, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "exit code: 3") || !strings.Contains(out, "(no output)") {
		t.Fatalf("a non-zero exit is an answer, not an error: got %q", out)
	}
}

func TestShellIsKilledAfterItsTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "sleep")
	start := time.Now()
	_, err := runShell("sleep 30", "", time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "did not finish within 1s and was killed") {
		t.Fatalf("a command over its timeout must be killed, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the command was not killed in time (%s)", time.Since(start))
	}
}

func TestLongOutputIsCutToTheLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "printf")
	t.Setenv("NEURO_SHELL_MAX_OUTPUT", "10")
	out, err := runShell("printf abcdefghijklmnopqrstuvwxyz", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "stdout:\nabcdefghij\n… [16 more characters truncated]") {
		t.Fatalf("got %q", out)
	}
}

func TestAMissingWorkingDirectoryIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	_, err := runShell("echo hi", "/nonexistent-neuro-client-test-dir", 0, nil)
	if err == nil || !strings.Contains(err.Error(), "working directory does not exist") {
		t.Fatalf("got %v", err)
	}
}

func TestTheShellCommandActionReplyMatchesThePythonAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	result := NewAgent().Execute(map[string]interface{}{
		"type":   "shell_command",
		"params": map[string]interface{}{"command": "echo hi", "allowlist": []interface{}{"echo"}, "denylist": nil},
	})
	if !result.Success {
		t.Fatalf("expected success, got %q", result.Error)
	}
	if result.Data["command"] != "echo hi" || result.Data["output"] != "exit code: 0\nstdout:\nhi" {
		t.Fatalf("data = %v", result.Data)
	}
}
