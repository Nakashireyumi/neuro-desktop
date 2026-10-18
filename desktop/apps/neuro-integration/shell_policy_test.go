package main

import (
	"reflect"
	"strings"
	"testing"
)

// The server owns the shell policy and sends its effective lists with every
// shell_command, so the agent never needs its own copy of the environment.
func TestShellCommandCarriesTheServerPolicy(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo,ls")
	t.Setenv("NEURO_SHELL_DENYLIST", "secret-word")
	resetShellRules()
	t.Cleanup(resetShellRules)

	cmd, err := buildIPCCommand(CmdShellCommand, map[string]interface{}{"command": "echo hi"}, true, true)
	if err != nil {
		t.Fatalf("buildIPCCommand failed: %v", err)
	}
	if got := cmd.Params["allowlist"]; !reflect.DeepEqual(got, []string{"echo", "ls"}) {
		t.Fatalf("allowlist = %#v, want [echo ls]", got)
	}
	if got := cmd.Params["denylist"]; !reflect.DeepEqual(got, []string{"secret-word"}) {
		t.Fatalf("denylist = %#v, want [secret-word] (built-ins stay on both sides)", got)
	}
}

func TestShellCommandWithoutAllowlistSendsAnEmptyList(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "")
	t.Setenv("NEURO_SHELL_DENYLIST", "")
	resetShellRules()
	t.Cleanup(resetShellRules)

	cmd, err := buildIPCCommand(CmdShellCommand, map[string]interface{}{"command": "echo hi"}, true, true)
	if err != nil {
		t.Fatalf("buildIPCCommand failed: %v", err)
	}
	list, ok := cmd.Params["allowlist"].([]string)
	if !ok || len(list) != 0 {
		t.Fatalf("allowlist = %#v, want an empty (non-nil) list so the agent refuses", cmd.Params["allowlist"])
	}
}

// One program per command: an allowlisted program must not be able to run a
// second one through the shell.
func TestShellChainingIsRefusedEvenForAnAllowlistedProgram(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	t.Setenv("NEURO_SHELL_DENYLIST", "")
	resetShellRules()
	t.Cleanup(resetShellRules)

	for _, command := range []string{
		"echo a && whoami",
		"echo a; id",
		"echo a | sh",
		"echo $(id)",
		"echo ${HOME}",
		"echo a > /tmp/x",
		"echo a < /etc/passwd",
		"echo `id`",
		"echo a\nid",
		"echo a & id",
	} {
		err := checkShellCommand(command)
		if err == nil {
			t.Fatalf("%q was allowed", command)
		}
		if !strings.Contains(err.Error(), "one program per command") {
			t.Fatalf("%q produced the wrong message: %v", command, err)
		}
	}

	if err := checkShellCommand("echo plain arguments are fine"); err != nil {
		t.Fatalf("a plain command was refused: %v", err)
	}
}
