package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Backend shutdown must not wait behind a slow observation or executor action.
// Stop admission first and write readiness before waking main's Close path.
func (n *NDIntegration) shutdownFromBackend(command CommandType) {
	n.stop.setPaused(true)
	n.cancelVisionServiceRequests()
	if n.client != nil {
		if err := n.client.SendShutdownReady(); err != nil {
			log.Printf("Failed to send shutdown/ready: %v", err)
		}
		_ = n.client.Close() // Discard acknowledged but unstarted SDK work.
	}
	n.markDone()
	if err := n.tryShutdownExecutor(command); err != nil {
		log.Printf("Executor shutdown was not confirmed: %v", err)
	}
	if n.executorHub != nil {
		// EOF also releases input in the matching Python client after any active
		// bounded command finishes. It cannot interrupt an OS call in progress.
		n.executorHub.Close()
	}
}

func (n *NDIntegration) tryShutdownExecutor(command CommandType) error {
	if !n.ipcMu.TryLock() {
		return fmt.Errorf("an executor command is still running; closing its TCP session")
	}
	defer n.ipcMu.Unlock()
	cmd := IPCCommand{Type: command, ClearAfter: true}
	if err := n.prepareExecutionSession(&cmd); err != nil {
		return err
	}
	if n.executorHub != nil && n.executorHub.HasClient() {
		response, err := n.executorHub.SendCommand(cmd, time.Second)
		if err != nil {
			return err
		}
		if !response.Success {
			return fmt.Errorf("%s", nonEmptyOr(response.Error, "executor refused shutdown"))
		}
		n.executorQueue = scriptPermissions{}
		return nil
	}
	if strings.TrimSpace(n.ipcFilePath) == "" {
		return fmt.Errorf("no executor is connected")
	}
	if _, err := os.Lstat(n.ipcFilePath); !os.IsNotExist(err) {
		return fmt.Errorf("file IPC path is occupied or unavailable; no command was overwritten")
	}
	n.fileRequestSequence++
	cmd.RequestID = fmt.Sprintf("%d", n.fileRequestSequence)
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(n.ipcFilePath, data); err != nil {
		return err
	}
	return fmt.Errorf("shutdown request written to file IPC; delivery is unconfirmed")
}
