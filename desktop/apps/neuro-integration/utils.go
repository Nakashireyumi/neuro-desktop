package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// bridgeProtocolVersion is exchanged during the executor hello handshake so
// that a stale executor binary is detected instead of silently misbehaving.
const bridgeProtocolVersion = "2"

func (n *NDIntegration) markDone() {
	n.doneOnce.Do(func() {
		close(n.done)
	})
	n.contextStopOnce.Do(func() {
		close(n.contextStopChan)
	})
}

// atomicWriteFile writes data to path via a temp file + rename so that readers
// never observe a half-written file. The executor polls this path, so a
// truncated read used to surface as intermittent "Failed to parse IPC command".
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func ipcTimeout() time.Duration {
	seconds := getEnvInt("NEURO_IPC_TIMEOUT_SECONDS", 30)
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

// sendToExecutor forwards a command to the executor, the process that actually
// touches this machine (see docs/EXECUTOR_PROTOCOL.md). It prefers a connected
// TCP executor (local or a remote machine) and falls back to the same-machine
// file IPC used by the co-located agent.
//
// Commands the bridge handles itself are rejected here: a `switch` that forgot
// one of them used to look exactly like an executor that silently did nothing.
func (n *NDIntegration) sendToExecutor(cmd IPCCommand, guards ...func() error) (*IPCResponse, error) {
	return n.sendToExecutorForAction(cmd, "", guards...)
}

func (n *NDIntegration) sendToExecutorForAction(cmd IPCCommand, action string, guards ...func() error) (*IPCResponse, error) {
	if !isExecutorCommand(cmd.Type) {
		return nil, fmt.Errorf(
			"%q is handled by the bridge, not by the executor (see executor_commands.go)",
			cmd.Type,
		)
	}

	n.ipcMu.Lock()
	defer n.ipcMu.Unlock()

	for _, guard := range guards {
		if err := guard(); err != nil {
			return nil, err
		}
	}
	additions, err := n.prepareExecutorDispatch(&cmd, action)
	if err != nil {
		return nil, err
	}
	if err := n.prepareExecutionSession(&cmd); err != nil {
		return nil, err
	}
	// The executor may append successfully and then fail while draining. Keep
	// conservative requirements until a confirmed clear, including on timeout.
	n.executorQueue.merge(additions)
	var response *IPCResponse
	if n.executorHub != nil && n.executorHub.HasClient() {
		response, err = n.executorHub.SendCommand(cmd, ipcTimeout())
	} else {
		response, err = n.sendToFileIPC(cmd)
	}
	if err == nil && response != nil && response.Success &&
		(cmd.Type == CmdClearActionQueue || cmd.Type == CmdShutdownGracefully || cmd.Type == CmdShutdownImmediately ||
			(queuesExecutorInput(cmd.Type) && cmd.ClearAfter)) {
		n.executorQueue = scriptPermissions{}
	}
	return response, err
}

func (n *NDIntegration) sendToFileIPC(cmd IPCCommand) (*IPCResponse, error) {
	if strings.TrimSpace(n.ipcFilePath) == "" {
		return nil, fmt.Errorf("no TCP executor is connected and no file IPC path is configured")
	}
	n.fileRequestSequence++
	cmd.RequestID = fmt.Sprintf("%d", n.fileRequestSequence)
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	responseFile := n.ipcFilePath + ".response"

	// Drop any previous response first: otherwise a slow command could return
	// the *previous* action's result, which is how "the action silently did
	// nothing" reports happened.
	if err := discardPreviousIPCResponse(responseFile); err != nil {
		return nil, fmt.Errorf("could not clear previous IPC response: %w", err)
	}
	if _, err := os.Lstat(n.ipcFilePath); err == nil {
		return nil, fmt.Errorf("IPC command path is already occupied; use a separate path for each running bridge")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("could not inspect IPC command path: %w", err)
	}

	if err := atomicWriteFile(n.ipcFilePath, cmdBytes); err != nil {
		return nil, fmt.Errorf("failed to write IPC command file: %w", err)
	}
	defer removeOwnedIPCFile(n.ipcFilePath, cmd, false)

	deadline := time.Now().Add(ipcTimeout())
	for time.Now().Before(deadline) {
		data, _, err := readBoundedIPCFile(responseFile)
		if err == nil {
			var resp IPCResponse
			if err := json.Unmarshal(data, &resp); err == nil {
				if resp.ExecutionSession == "" || resp.RequestID == "" {
					return nil, fmt.Errorf("file IPC client does not support execution_session/request_id; update neuro-client to match the server")
				}
				if resp.ExecutionSession != cmd.ExecutionSession || resp.RequestID != cmd.RequestID {
					time.Sleep(25 * time.Millisecond)
					continue // An earlier bridge/request finished late.
				}
				removeOwnedIPCFile(responseFile, cmd, true)
				return &resp, nil
			}
			// Another writer may have been mid-write; keep polling briefly.
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to read IPC response file: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// The command was never picked up. Remove it so it cannot execute later
	// out of order (e.g. after the operator grants/denies a permission).
	removeOwnedIPCFile(responseFile, cmd, true)
	return nil, fmt.Errorf("timeout waiting for executor response (no TCP client; file IPC timed out)")
}

func nonEmptyOr(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
