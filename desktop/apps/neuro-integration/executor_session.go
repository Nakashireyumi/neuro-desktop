package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// An executor queue is approved by one running bridge. Never inherit it after
// that bridge exits: its new instance has neither those approvals nor the
// in-memory queue metadata. This nonce is not an authentication credential.
func newExecutionSession() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("could not create executor session: %w", err)
	}
	return hex.EncodeToString(nonce[:]), nil
}

// prepareExecutionSession runs under ipcMu. Constructors initialize eagerly;
// the lazy branch keeps isolated integrations/test fixtures safe as well.
func (n *NDIntegration) prepareExecutionSession(cmd *IPCCommand) error {
	if n.executionSession == "" {
		var err error
		n.executionSession, err = newExecutionSession()
		if err != nil {
			return err
		}
	}
	cmd.ExecutionSession = n.executionSession
	cmd.RequestID = "" // Only file IPC needs its own per-request correlation.
	return nil
}

const maxIPCFileBytes = 8 << 20

// Protocol-shaped files may belong to another running bridge. Startup cannot
// establish that they are abandoned, so it never removes or reads them. The
// operator must stop both peers before inspecting/removing abandoned files.
func requireUnusedIPCPaths(path string) error {
	if path == "" {
		return nil
	}
	const guidance = "stop both the bridge and file IPC client, inspect the files and remove only abandoned protocol files, or choose a separate --ipc-file path"
	for _, candidate := range []string{path, path + ".response"} {
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("could not inspect IPC path %s; it was left untouched: %w; %s", candidate, err, guidance)
		}
		return fmt.Errorf("IPC path %s already exists and was left untouched; %s", candidate, guidance)
	}
	return nil
}

// Do not follow a configured IPC path to another file or read an unbounded
// response into memory. The second stat also detects replacement between the
// initial inspection and open on every supported operating system.
func readBoundedIPCFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxIPCFileBytes {
		return nil, nil, fmt.Errorf("IPC path %s is not a bounded regular protocol file; it was left untouched", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("could not inspect IPC file %s: %w", path, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("could not inspect opened IPC file %s: %w", path, err)
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("IPC path %s changed while opening; it was left untouched", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxIPCFileBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("could not read IPC file %s: %w", path, err)
	}
	if len(data) > maxIPCFileBytes {
		return nil, nil, fmt.Errorf("IPC path %s exceeds the protocol size limit; it was left untouched", path)
	}
	return data, opened, nil
}

// A failed or timed-out dispatch must not leave its command for the executor to
// pick up later. Correlation prevents cleanup from deleting a newer bridge's
// command or an unrelated file that replaced the configured path.
func removeOwnedIPCFile(path string, command IPCCommand, response bool) {
	data, info, err := readBoundedIPCFile(path)
	if err != nil || !recognizedIPCFile(data, response) {
		return
	}
	var owner struct {
		ExecutionSession string `json:"execution_session"`
		RequestID        string `json:"request_id"`
	}
	if json.Unmarshal(data, &owner) != nil || owner.ExecutionSession == "" || owner.RequestID == "" ||
		owner.ExecutionSession != command.ExecutionSession || owner.RequestID != command.RequestID {
		return
	}
	if current, err := os.Lstat(path); err == nil && os.SameFile(info, current) {
		_ = os.Remove(path)
	}
}

func discardPreviousIPCResponse(path string) error {
	data, info, err := readBoundedIPCFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !recognizedIPCFile(data, true) {
		return fmt.Errorf("IPC response path %s contains an unrecognized document; it was left untouched", path)
	}
	current, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("IPC response path %s changed before dispatch; it was left untouched", path)
	}
	return os.Remove(path)
}

func recognizedIPCFile(data []byte, response bool) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return false
	}
	allowed := map[string]bool{"execution_session": true, "request_id": true}
	if response {
		allowed["success"], allowed["data"], allowed["error"] = true, true, true
		if _, ok := fields["success"]; !ok {
			return false
		}
		var parsed IPCResponse
		if json.Unmarshal(data, &parsed) != nil || string(fields["success"]) == "null" {
			return false
		}
	} else {
		allowed["type"], allowed["params"], allowed["execute_now"], allowed["clear_after"] = true, true, true, true
		if _, ok := fields["execute_now"]; !ok {
			return false
		}
		if _, ok := fields["clear_after"]; !ok {
			return false
		}
		var parsed IPCCommand
		if json.Unmarshal(data, &parsed) != nil || !isExecutorCommand(parsed.Type) ||
			string(fields["execute_now"]) == "null" || string(fields["clear_after"]) == "null" {
			return false
		}
	}
	for key := range fields {
		if !allowed[key] {
			return false
		}
	}
	return true
}
