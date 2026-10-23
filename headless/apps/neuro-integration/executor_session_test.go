package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecutorDispatchKeepsOneUnpredictableSessionPerBridge(t *testing.T) {
	first, firstExecutor := scriptDispatchTestIntegration(t)
	second, secondExecutor := scriptDispatchTestIntegration(t)
	for _, integration := range []*NDIntegration{first, second} {
		for _, command := range []CommandType{CmdGetStatus, CmdClearActionQueue, CmdKeyReleaseAll} {
			_, err := integration.sendToExecutor(IPCCommand{Type: command, ExecutionSession: "caller-cannot-choose", RequestID: "caller-cannot-choose"})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	firstCommands, secondCommands := firstExecutor.seenCommands(), secondExecutor.seenCommands()
	for _, commands := range [][]IPCCommand{firstCommands, secondCommands} {
		session := commands[0].ExecutionSession
		decoded, err := hex.DecodeString(session)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("session is not a random 32-byte nonce: %q", session)
		}
		for _, command := range commands {
			if command.ExecutionSession != session || command.RequestID != "" {
				t.Fatalf("TCP command did not use its bridge's execution session: %+v", command)
			}
		}
	}
	if firstCommands[0].ExecutionSession == secondCommands[0].ExecutionSession {
		t.Fatal("a replacement bridge inherited its predecessor's execution session")
	}
}

func waitForFileCommand(path string, timeout time.Duration) (IPCCommand, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var command IPCCommand
			if err := json.Unmarshal(data, &command); err != nil {
				return command, err
			}
			return command, nil
		}
		if !os.IsNotExist(err) && !transientIPCReadError(err) {
			return IPCCommand{}, err
		}
		time.Sleep(time.Millisecond)
	}
	return IPCCommand{}, fmt.Errorf("executor did not receive an IPC command")
}

func TestIPCReadRetriesOnlyWindowsSharingViolations(t *testing.T) {
	for _, code := range []syscall.Errno{32, 33} {
		wrapped := &os.PathError{Op: "open", Path: "ipc.json", Err: code}
		if got := transientIPCReadError(wrapped); got != (runtime.GOOS == "windows") {
			t.Fatalf("sharing violation retry = %v on %s", got, runtime.GOOS)
		}
	}
	for _, err := range []error{nil, os.ErrPermission, os.ErrInvalid, syscall.Errno(5)} {
		if transientIPCReadError(err) {
			t.Fatalf("permanent error was made retryable: %v", err)
		}
	}
}

func writeIPCResponse(path string, command IPCCommand, mutate func(*IPCResponse)) error {
	response := IPCResponse{Success: true, ExecutionSession: command.ExecutionSession, RequestID: command.RequestID}
	if mutate != nil {
		mutate(&response)
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return atomicWriteFile(path+".response", data)
}

func TestFileIPCDiscardsLateResponseFromOtherRequestOrBridge(t *testing.T) {
	t.Setenv("NEURO_IPC_TIMEOUT_SECONDS", "2")
	path := filepath.Join(t.TempDir(), "ipc.json")
	n := &NDIntegration{ipcFilePath: path}
	for index, wrongSession := range []bool{false, true} {
		t.Run(fmt.Sprintf("request-%d", index+1), func(t *testing.T) {
			finished := make(chan error, 1)
			go func() {
				response, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus})
				if err == nil && (response == nil || !response.Success || response.Data["current"] != true) {
					err = fmt.Errorf("late response was accepted: %+v", response)
				}
				finished <- err
			}()
			command, err := waitForFileCommand(path, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if command.RequestID != fmt.Sprint(index+1) || command.ExecutionSession == "" {
				t.Fatalf("file request correlation missing: %+v", command)
			}
			if err := writeIPCResponse(path, command, func(response *IPCResponse) {
				if wrongSession {
					response.ExecutionSession = "earlier-bridge"
				} else {
					response.RequestID = "earlier-request"
				}
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				t.Fatalf("mismatched response completed the current request: %v", err)
			case <-time.After(75 * time.Millisecond):
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := writeIPCResponse(path, command, func(response *IPCResponse) {
				response.Data = map[string]interface{}{"current": true}
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("matching IPC response was not accepted")
			}
		})
	}
}

func TestFileIPCRefusesResponseWithoutSessionAndRequestEcho(t *testing.T) {
	for _, missing := range []string{"session", "request", "both"} {
		t.Run(missing, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ipc.json")
			n := &NDIntegration{ipcFilePath: path}
			finished := make(chan error, 1)
			go func() {
				_, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus})
				finished <- err
			}()
			command, err := waitForFileCommand(path, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeIPCResponse(path, command, func(response *IPCResponse) {
				if missing == "session" || missing == "both" {
					response.ExecutionSession = ""
				}
				if missing == "request" || missing == "both" {
					response.RequestID = ""
				}
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err == nil || !strings.Contains(err.Error(), "update neuro-client") {
					t.Fatalf("old IPC client was not refused with update guidance: %v", err)
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("a rejected request could still execute on a later file poll: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("missing correlation should fail promptly")
			}
		})
	}
}

func TestFileIPCMismatchedEchoTimesOutWithoutLeavingACommand(t *testing.T) {
	t.Setenv("NEURO_IPC_TIMEOUT_SECONDS", "1")
	path := filepath.Join(t.TempDir(), "ipc.json")
	n := &NDIntegration{ipcFilePath: path}
	finished := make(chan error, 1)
	go func() {
		_, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus})
		finished <- err
	}()
	command, err := waitForFileCommand(path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeIPCResponse(path, command, func(response *IPCResponse) { response.RequestID = "wrong-request" }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("mismatched echo was accepted: %v", err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("timed-out request could still execute on a later file poll: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("mismatched response did not reach the bounded timeout")
	}
}

func TestFileIPCDoesNotOverwriteAnOccupiedOrPrivatePath(t *testing.T) {
	for _, suffix := range []string{"", ".response"} {
		t.Run("path"+suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ipc.json")
			private := `{"token":"private-fixture-secret"}`
			if err := os.WriteFile(path+suffix, []byte(private), 0600); err != nil {
				t.Fatal(err)
			}
			n := &NDIntegration{ipcFilePath: path}
			if _, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus}); err == nil {
				t.Fatal("an occupied/private path did not stop dispatch")
			}
			data, err := os.ReadFile(path + suffix)
			if err != nil || string(data) != private {
				t.Fatalf("dispatch changed an unrelated file: %q %v", data, err)
			}
		})
	}
}

const abandonedCommand = `{"type":"key_press","params":{"key":"x"},"execute_now":true,"clear_after":false}`
const abandonedResponse = `{"success":true,"data":{"queued":true}}`

func TestBridgeStartupAcceptsUnusedIPCPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ipc.json")
	n, err := NewNDIntegration(IntegrationOptions{WSURL: "ws://127.0.0.1:1", GameName: "session-test", IPCPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if n.executionSession == "" {
		t.Fatal("constructor did not create the execution session")
	}
	for _, candidate := range []string{path, path + ".response"} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("construction created an IPC file: %s (%v)", candidate, err)
		}
	}
	if err := requireUnusedIPCPaths(""); err != nil {
		t.Fatalf("unconfigured file IPC should not stop startup: %v", err)
	}
}

func TestBridgeStartupPreservesExistingProtocolFiles(t *testing.T) {
	for _, occupied := range []string{"command", "response", "both"} {
		t.Run(occupied, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ipc.json")
			files := map[string]string{}
			if occupied != "response" {
				files[path] = abandonedCommand
			}
			if occupied != "command" {
				files[path+".response"] = abandonedResponse
			}
			for candidate, content := range files {
				if err := os.WriteFile(candidate, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := NewNDIntegration(IntegrationOptions{WSURL: "ws://127.0.0.1:1", GameName: "session-test", IPCPath: path})
			if err == nil {
				t.Fatal("existing protocol file did not stop a competing bridge")
			}
			for _, guidance := range []string{"left untouched", "stop both the bridge and file IPC client", "inspect", "abandoned protocol files", "separate --ipc-file path"} {
				if !strings.Contains(err.Error(), guidance) {
					t.Fatalf("startup error did not explain how to recover (%s): %v", guidance, err)
				}
			}
			for candidate, expected := range files {
				data, err := os.ReadFile(candidate)
				if err != nil || string(data) != expected {
					t.Fatalf("startup changed existing protocol work: %s: %q %v", candidate, data, err)
				}
			}
		})
	}
}

func TestBridgeStartupKeepsUnknownOrPrivateIPCFiles(t *testing.T) {
	for _, fixture := range []struct{ name, content string }{
		{"private", `{"token":"private-fixture-secret"}`},
		{"extra-field", `{"success":true,"token":"private-fixture-secret"}`},
		{"malformed", `{"success":true`},
		{"null-success", `{"success":null}`},
		{"unknown-command", `{"type":"unknown","execute_now":true,"clear_after":false}`},
		{"null-flag", `{"type":"key_press","execute_now":null,"clear_after":false}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ipc.json")
			if err := os.WriteFile(path, []byte(abandonedCommand), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".response", []byte(fixture.content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := requireUnusedIPCPaths(path); err == nil {
				t.Fatal("unrecognized file did not stop startup")
			}
			for candidate, expected := range map[string]string{path: abandonedCommand, path + ".response": fixture.content} {
				data, err := os.ReadFile(candidate)
				if err != nil || string(data) != expected {
					t.Fatalf("startup changed an unverified file pair: %s: %q %v", candidate, data, err)
				}
			}
		})
	}
}

func TestBridgeStartupRefusesSymlinkDirectoryOrOversizedIPCFile(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ipc.json")
			var target string
			switch kind {
			case "symlink":
				target = filepath.Join(t.TempDir(), "private.json")
				if err := os.WriteFile(target, []byte(abandonedCommand), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink creation unavailable: %v", err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(maxIPCFileBytes + 1)
				_ = file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := requireUnusedIPCPaths(path); err == nil {
				t.Fatal("unsafe IPC path did not stop startup")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("unsafe IPC path was removed: %v", err)
			}
			if target != "" {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != abandonedCommand {
					t.Fatal("symlink target was changed")
				}
			}
		})
	}
}
