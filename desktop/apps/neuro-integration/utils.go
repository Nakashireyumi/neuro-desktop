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
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	responseFile := n.ipcFilePath + ".response"

	// Drop any previous response first: otherwise a slow command could return
	// the *previous* action's result, which is how "the action silently did
	// nothing" reports happened.
	_ = os.Remove(responseFile)

	if err := atomicWriteFile(n.ipcFilePath, cmdBytes); err != nil {
		return nil, fmt.Errorf("failed to write IPC command file: %w", err)
	}

	deadline := time.Now().Add(ipcTimeout())
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(responseFile)
		if err == nil {
			var resp IPCResponse
			if err := json.Unmarshal(data, &resp); err == nil {
				_ = os.Remove(responseFile)
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
	_ = os.Remove(n.ipcFilePath)
	_ = os.Remove(responseFile)
	return nil, fmt.Errorf("timeout waiting for executor response (no TCP client; file IPC timed out)")
}

func nonEmptyOr(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
