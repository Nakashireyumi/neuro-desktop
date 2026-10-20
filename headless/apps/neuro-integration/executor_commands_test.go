package main

import (
	"strings"
	"testing"
)

// Every command the bridge can send to the executor must be declared in
// executor_commands.go, and every declared command must actually reach the
// executor. Without this, a new action that forgets the map is rejected by
// sendToExecutor with a confusing "handled by the bridge" error instead of
// being executed, which is exactly the silent-nothing-happened class of bug
// this whole pass was about.
func TestExecutorCommandListIsExplicit(t *testing.T) {
	if len(executorCommands) == 0 {
		t.Fatal("executorCommands is empty; the bridge could not execute anything")
	}

	for name, sendable := range executorCommands {
		if name == "" {
			t.Fatal("executorCommands contains an empty command type")
		}
		if !sendable {
			t.Fatalf("executorCommands[%q] is false; use deletion instead", name)
		}
	}

	// Sanity: the primitive set the game layer and the shell action rely on.
	for _, name := range []CommandType{
		CmdMouseMove,
		CmdKeyReleaseAll,
		CmdRunScript,
		CmdShellCommand,
		CmdGetStatus,
	} {
		if !isExecutorCommand(name) {
			t.Fatalf("%q must be an executor command", name)
		}
	}

	// Bridge-local commands must never be forwarded: the executor would have no
	// idea what to do with them.
	for _, name := range []CommandType{
		CmdOpenWindowsMenu,
		CmdShowDesktop,
		CmdListCatalogItems,
		CmdInstallExtension,
		CmdGameMove,
		CmdGameObserve,
		EnableLLControls,
	} {
		if isExecutorCommand(name) {
			t.Fatalf("%q is handled by the bridge and must not be in executorCommands", name)
		}
	}
}

func TestSendToExecutorRejectsBridgeCommands(t *testing.T) {
	integration := &NDIntegration{}

	for _, name := range []CommandType{CmdOpenWindowsMenu, CmdGameMove, CmdInstallExtension} {
		_, err := integration.sendToExecutor(IPCCommand{Type: name})
		if err == nil {
			t.Fatalf("%q was accepted by sendToExecutor", name)
		}
		if !strings.Contains(err.Error(), "handled by the bridge") {
			t.Fatalf("%q produced the wrong error: %v", name, err)
		}
	}
}

func TestSendToExecutorAcceptsExecutorCommands(t *testing.T) {
	// No hub and no IPC path: the call must fail on the missing transport, not
	// on the command-type guard.
	integration := &NDIntegration{}

	for name := range executorCommands {
		_, err := integration.sendToExecutor(IPCCommand{Type: name})
		if err == nil {
			continue // a transport error is fine too; here it is always non-nil
		}
		if strings.Contains(err.Error(), "handled by the bridge") {
			t.Fatalf("%q is declared as an executor command but was rejected: %v", name, err)
		}
	}
}
