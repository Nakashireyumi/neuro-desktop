package main

import (
	"sync"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Command types that match the Rust IPC implementation.
type CommandType string

type NDIntegration struct {
	client          *neuro.Client
	ipcFilePath     string
	permissionsPath string
	permissions     *PermissionPolicy
	executorHub     *ExecutorHub
	done            chan struct{}
	doneOnce        sync.Once
	ipcMu           sync.Mutex
	contextStopChan chan struct{}
	contextStopOnce sync.Once
}

// IPC Command to Rust binary.
type IPCCommand struct {
	Type       CommandType            `json:"type"`
	Params     map[string]interface{} `json:"params,omitempty"`
	ExecuteNow bool                   `json:"execute_now"`
	ClearAfter bool                   `json:"clear_after"`
}

// IPC Response from Rust binary.
type IPCResponse struct {
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}
