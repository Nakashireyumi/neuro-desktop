package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func scriptPayload(script string) json.RawMessage {
	payload, _ := json.Marshal(map[string]interface{}{"script": script})
	return payload
}

func TestScriptScopesRecognizeQuotedEscapedAndSeparatedCommands(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	resetShellRules()
	t.Cleanup(resetShellRules)
	for _, test := range []struct{ script, scope string }{
		{`SHELL "echo hello"`, "shell"},
		{`"SHELL" "echo hello"`, "shell"},
		{`'shell' "echo hello"`, "shell"},
		{`S\HELL "echo hello"`, "shell"},
		{`S"HE"'LL' "echo hello"`, "shell"},
		{`LAUNCH app`, "system"},
		{`"LAUNCH" app`, "system"},
		{`L\AUNCH app`, "system"},
		{`'la'"un"ch app`, "system"},
		{"TYPE hello\u2028\"SHELL\" \"echo hello\"", "shell"},
		{"TYPE hello\vLAUNCH app", "system"},
		{"TYPE hello\r\nLAUNCH app", "system"},
	} {
		t.Run(test.script, func(t *testing.T) {
			action, _ := newPolicyAction(t, actionSpec{Name: CmdRunScript}, `{"scopes":{"input":true,"shell":false,"system":false}}`)
			_, result := action.Validate(scriptPayload(test.script))
			if result.Successful || !strings.Contains(result.Message, test.scope) {
				t.Fatalf("script must need %s, got %+v", test.scope, result)
			}
		})
	}
}

func TestScriptIntentsHonorTheirActionPolicies(t *testing.T) {
	for _, test := range []struct{ command, action, scope string }{
		{"LOCK_WORKSTATION", "lock_workstation", "system"},
		{"OPEN_SETTINGS", "open_windows_settings", "system"},
		{"OPEN_WINDOWS_SETTINGS", "open_windows_settings", "system"},
		{"OPEN_POWER_USER_MENU", "open_power_user_menu", "system"},
		{"OPEN_FILE_EXPLORER", "open_file_explorer", "filesystem"},
		{"OPEN_TASK_MANAGER", "open_task_manager", "process"},
		{"OPEN_RUN_DIALOG", "open_run_dialog", "process"},
		{"CLOSE_ALL_APPS", "close_all_apps", "process"},
		{"TAKE_SCREEN_SNIP", "take_screen_snip", "vision"},
	} {
		t.Run(test.command, func(t *testing.T) {
			policy := `{"default_allow":true,"scopes":{"input":true,"` + test.scope + `":false}}`
			action, _ := newPolicyAction(t, actionSpec{Name: CmdRunScript}, policy)
			if _, result := action.Validate(scriptPayload(`"` + test.command + `"`)); result.Successful {
				t.Fatalf("nested %s bypassed disabled %s scope", test.command, test.scope)
			}
			policy = `{"default_allow":true,"scopes":{"input":true,"` + test.scope + `":true},"denied_actions":["` + test.action + `"]}`
			action, _ = newPolicyAction(t, actionSpec{Name: CmdRunScript}, policy)
			if _, result := action.Validate(scriptPayload(test.command)); result.Successful {
				t.Fatalf("nested %s bypassed explicit action denial", test.command)
			}
		})
	}
}

func TestScriptShellUsesServerFirewallAndActionDenial(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	resetShellRules()
	t.Cleanup(resetShellRules)
	action, _ := newPolicyAction(t, actionSpec{Name: CmdRunScript}, `{"scopes":{"input":true,"shell":true}}`)
	for _, script := range []string{`SHELL "python3 tool.py"`, `SHELL "echo hi; echo hidden"`, `SHELL "echo hi | cat"`} {
		if _, result := action.Validate(scriptPayload(script)); result.Successful {
			t.Fatalf("script bypassed shell firewall: %s", script)
		}
	}
	if _, result := action.Validate(scriptPayload(`SHELL "echo hello"`)); !result.Successful {
		t.Fatalf("allowlisted shell should be accepted: %s", result.Message)
	}
	action, _ = newPolicyAction(t, actionSpec{Name: CmdRunScript}, `{"scopes":{"input":true,"shell":true},"denied_actions":["shell_command"]}`)
	if _, result := action.Validate(scriptPayload(`SHELL "echo hello"`)); result.Successful {
		t.Fatal("script bypassed explicit shell_command denial")
	}
}

func TestScriptPrimitivesHonorTheirSpecificActionDenials(t *testing.T) {
	for _, test := range []struct{ script, action string }{
		{`TYPE hello`, string(CmdTypeText)},
		{`PRESS enter`, string(CmdKeyPress)},
		{`COMBO ctrl c`, string(CmdKeyCombo)},
		{`SHORTCUT ctrl c`, string(CmdKeyCombo)},
		{`HOLD_FOR w 0.1`, string(CmdKeyHoldFor)},
		{`MOVE_REL 1 2`, string(CmdMoveMouseRelative)},
		{`CLICK left`, string(CmdMouseClick)},
	} {
		t.Run(test.action, func(t *testing.T) {
			action, _ := newPolicyAction(t, actionSpec{Name: CmdRunScript}, `{"scopes":{"input":true},"denied_actions":["`+test.action+`"]}`)
			if _, result := action.Validate(scriptPayload(test.script)); result.Successful {
				t.Fatalf("script bypassed %s action denial", test.action)
			}
		})
	}
}

func TestScriptPreflightChecksWholeScriptAndBounds(t *testing.T) {
	for _, script := range []string{
		"TYPE hello\nINVALID", "TYPE hello\nPRESS", "TYPE hello\nENTER extra",
		"TYPE hello\n'broken", "TYPE hello\nPRESS x\\", "WAIT NaN", "WAIT Inf", "WAIT -1", "WAIT 31",
		"HOLD_FOR x 0", "HOLD_FOR x NaN", "MOVE 1 2 Inf", "MOVE_REL 1 2 3",
		"MOVE_N 1.1 0", "CLICK_N NaN 0", "CLICK bad", "LINE 1 2 3 4 STEPS 1001",
		"LINE 1 2 3 4 trailing", "PATH 1 2 3", "MOVE 2147483648 1",
		strings.Repeat("PRESS x\n", 513), "TYPE " + strings.Repeat("x", maxScriptBytes),
		"TYPE " + string([]byte{0xff}),
	} {
		if _, err := validateScript(script); err == nil {
			t.Fatalf("accepted invalid script %.80q", script)
		}
	}
	valid := `# Comments do not add instructions.
TYPE "hello world"
ENTER
PRESS a
HOLD shift
RELEASE shift
SHORTCUT ctrl s
COMBO ctrl c
HOLD_FOR w 0.1
RELEASE_ALL
MOVE -1 2 0.1
MOVE_REL 1 -2 0
MOVE_N 0 1
CLICK
CLICK_N 0.5 0.5 right
LINE 1 2 3 4 steps 2
PATH 1 2 3 4
WAIT 0
OPEN_START_MENU
LAUNCH app
SHELL "echo hello"`
	if _, err := validateScript(valid); err != nil {
		t.Fatalf("valid grammar rejected: %v", err)
	}
	for _, script := range []string{"WAIT 10\nWAIT 10", "LINE 1 2 3 4 STEPS 1000", "TYPE " + strings.Repeat("x", 1000)} {
		if _, err := validateScript(script); err == nil || !strings.Contains(err.Error(), "20-second") {
			t.Fatalf("script exceeding execution budget was not refused: %v", err)
		}
	}
}

func TestScriptTokenizationMatchesPythonShlex(t *testing.T) {
	for _, test := range []struct {
		line string
		want []string
	}{
		{`S"HE"'LL' 'echo hi'`, []string{"SHELL", "echo hi"}},
		{`L\AUNCH C:\\app`, []string{"LAUNCH", `C:\app`}},
		{`TYPE "a\q\"b\\c"`, []string{"TYPE", `a\q"b\c`}},
		{`TYPE '' a\ b # ordinary text`, []string{"TYPE", "", "a b", "#", "ordinary", "text"}},
	} {
		got, err := scriptTokens(test.line)
		if err != nil || strings.Join(got, "\x00") != strings.Join(test.want, "\x00") {
			t.Fatalf("%q => %q, %v; want %q", test.line, got, err, test.want)
		}
	}
}

func scriptDispatchTestIntegration(t *testing.T) (*NDIntegration, *fakeExecutor) {
	t.Helper()
	n := &NDIntegration{executorHub: newHub(t, ""), stop: &stopSwitch{}}
	n.setPolicy(defaultPermissionPolicy())
	executor := startFakeExecutor(t, n.executorHub, "", func(IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true}, true
	})
	t.Cleanup(func() { _ = executor.conn.Close() })
	return n, executor
}

func TestQueuedScriptRechecksRevokedPermissionBeforeAnyDrain(t *testing.T) {
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	resetShellRules()
	t.Cleanup(resetShellRules)
	for _, drain := range []IPCCommand{
		{Type: CmdExecuteQueue},
		{Type: CmdKeyPress, Params: map[string]interface{}{"key": "enter"}, ExecuteNow: true, ClearAfter: true},
	} {
		t.Run(string(drain.Type), func(t *testing.T) {
			n, executor := scriptDispatchTestIntegration(t)
			policy := defaultPermissionPolicy()
			policy.scopes[ScopeShell] = ScopeConfig{Allowed: true}
			n.setPolicy(policy)
			_, err := n.sendToExecutorForAction(IPCCommand{Type: CmdRunScript, Params: map[string]interface{}{"script": `SHELL "echo hello"`}}, string(CmdRunScript))
			if err != nil {
				t.Fatalf("could not enqueue allowed script: %v", err)
			}
			sent := executor.seenCommands()
			if len(sent) != 1 || len(sent[0].Params["allowlist"].([]interface{})) != 1 {
				t.Fatalf("effective server shell policy was not attached: %+v", sent)
			}
			n.setPolicy(defaultPermissionPolicy()) // shell revoked after enqueue
			if _, err := n.sendToExecutorForAction(drain, string(drain.Type)); err == nil || !strings.Contains(err.Error(), "shell") {
				t.Fatalf("queued shell must be refused after revocation, got %v", err)
			}
			if len(executor.seenCommands()) != 1 {
				t.Fatal("revoked queued script reached the executor")
			}
			if _, err := n.sendToExecutor(IPCCommand{Type: CmdClearActionQueue}); err != nil {
				t.Fatalf("clearing revoked work must remain available: %v", err)
			}
			if n.executorQueue.instructions != 0 {
				t.Fatal("confirmed clear retained old permission requirements")
			}
		})
	}
}

func TestScriptDispatchRechecksAfterWaitingForExecutor(t *testing.T) {
	for _, change := range []string{"pause", "revoke"} {
		t.Run(change, func(t *testing.T) {
			n, executor := scriptDispatchTestIntegration(t)
			policy := defaultPermissionPolicy()
			policy.scopes[ScopeSystem] = ScopeConfig{Allowed: true}
			n.setPolicy(policy)
			n.ipcMu.Lock() // simulate an earlier, slow executor request
			entered := make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				close(entered)
				_, err := n.sendToExecutorForAction(IPCCommand{Type: CmdRunScript, Params: map[string]interface{}{"script": "LAUNCH app"}, ExecuteNow: true, ClearAfter: true}, string(CmdRunScript))
				finished <- err
			}()
			<-entered
			if change == "pause" {
				n.stop.setPaused(true)
			} else {
				n.setPolicy(defaultPermissionPolicy())
			}
			n.ipcMu.Unlock()
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("stale accepted script was dispatched after permission changed")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("dispatch did not finish")
			}
			if len(executor.seenCommands()) != 0 {
				t.Fatal("stale script reached the executor")
			}
		})
	}
}

func TestQueuedScriptRetainsActionDenialsAndBounds(t *testing.T) {
	n, executor := scriptDispatchTestIntegration(t)
	first := IPCCommand{Type: CmdRunScript, Params: map[string]interface{}{"script": "WAIT 10"}}
	if _, err := n.sendToExecutorForAction(first, string(CmdRunScript)); err != nil {
		t.Fatalf("first bounded script failed: %v", err)
	}
	if _, err := n.sendToExecutorForAction(first, string(CmdRunScript)); err == nil || !strings.Contains(err.Error(), "20-second") {
		t.Fatalf("retained queue delay budget was bypassed: %v", err)
	}
	policy := defaultPermissionPolicy()
	policy.denied[string(CmdRunScript)] = struct{}{}
	n.setPolicy(policy)
	if _, err := n.sendToExecutorForAction(IPCCommand{Type: CmdExecuteQueue}, string(CmdExecuteQueue)); err == nil {
		t.Fatal("retained script bypassed a new run_script action denial")
	}
	if len(executor.seenCommands()) != 1 {
		t.Fatal("denied or oversized queue was forwarded")
	}
	if _, err := n.sendToExecutor(IPCCommand{Type: CmdClearActionQueue}); err != nil {
		t.Fatal(err)
	}
	n.setPolicy(defaultPermissionPolicy())
	// Metadata is deliberately conservative after a transport error. A confirmed
	// clear above must discard it, so a fresh bounded script is accepted.
	if _, err := n.sendToExecutorForAction(first, string(CmdRunScript)); err != nil {
		t.Fatalf("confirmed queue clear failed to reset the budget: %v", err)
	}
}

func TestQueuedLaunchAndIntentRetainTheirRequirements(t *testing.T) {
	for _, test := range []struct {
		script string
		scope  PermissionScope
	}{
		{"LAUNCH app", ScopeSystem},
		{"OPEN_SETTINGS", ScopeSystem},
		{"OPEN_FILE_EXPLORER", ScopeFilesystem},
		{"TAKE_SCREEN_SNIP", ScopeVision},
	} {
		t.Run(test.script, func(t *testing.T) {
			n, executor := scriptDispatchTestIntegration(t)
			policy := defaultPermissionPolicy()
			policy.scopes[test.scope] = ScopeConfig{Allowed: true}
			n.setPolicy(policy)
			if _, err := n.sendToExecutorForAction(IPCCommand{Type: CmdRunScript, Params: map[string]interface{}{"script": test.script}}, string(CmdRunScript)); err != nil {
				t.Fatal(err)
			}
			policy = defaultPermissionPolicy()
			policy.scopes[test.scope] = ScopeConfig{Allowed: false}
			n.setPolicy(policy)
			if _, err := n.sendToExecutorForAction(IPCCommand{Type: CmdExecuteQueue}, string(CmdExecuteQueue)); err == nil {
				t.Fatal("queued script lost its required permission")
			}
			if len(executor.seenCommands()) != 1 {
				t.Fatal("revoked queued script was dispatched")
			}
		})
	}
}

func TestDispatchGuardsRunAfterWaitingForExecutor(t *testing.T) {
	n := &NDIntegration{}
	n.ipcMu.Lock()
	checked := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		_, _ = n.sendToExecutor(IPCCommand{Type: CmdGetStatus}, func() error { close(checked); return nil })
		close(finished)
	}()
	select {
	case <-checked:
		t.Fatal("dispatch guard ran before acquiring transport lock")
	case <-time.After(10 * time.Millisecond):
	}
	n.ipcMu.Unlock()
	<-finished
	select {
	case <-checked:
	default:
		t.Fatal("dispatch guard did not run")
	}
}

func TestRelayCommandRechecksPolicyAfterWaitingForExecutor(t *testing.T) {
	n, executor := scriptDispatchTestIntegration(t)
	initial := defaultPermissionPolicy()
	initial.scopes[ScopeInput] = ScopeConfig{Allowed: true, Limits: ScopeLimits{MaxActionsPerMinute: 2}}
	n.setPolicy(initial)
	relay := newRelayState(RelayConfig{})
	relay.setOwner(n)
	n.ipcMu.Lock()
	locked := true
	defer func() {
		if locked {
			n.ipcMu.Unlock()
		}
	}()
	finished := make(chan struct{})
	go func() {
		relay.executeRelayCommand("fixture-watcher", map[string]interface{}{
			"action": "type_text", "params": map[string]interface{}{"text": "must not type"},
		})
		close(finished)
	}()
	// Synchronize the initial relay policy check without a timing assumption:
	// its rate limiter is touched only after that first policy gate passes.
	deadline := time.Now().Add(2 * time.Second)
	for {
		n.rate.mu.Lock()
		passedFirstGate := len(n.rate.windows[ScopeInput]) == 1
		n.rate.mu.Unlock()
		if passedFirstGate {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay did not begin validation")
		}
		time.Sleep(time.Millisecond)
	}
	policy := defaultPermissionPolicy()
	policy.denied[string(CmdTypeText)] = struct{}{}
	n.setPolicy(policy)
	n.ipcMu.Unlock()
	locked = false
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not finish after executor lock released")
	}
	if len(executor.seenCommands()) != 0 {
		t.Fatal("revoked watcher command reached executor")
	}
	n.rate.mu.Lock()
	defer n.rate.mu.Unlock()
	if len(n.rate.windows[ScopeInput]) != 1 {
		t.Fatal("dispatch-time recheck charged the relay rate limit again")
	}
}
