package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// ExecutorHub is the server-side socket that remote (or local) executor
// clients connect to. Neuro actions are forwarded here instead of only
// using same-machine file IPC.
type ExecutorHub struct {
	addr string

	mu       sync.Mutex
	listener net.Listener
	conn     net.Conn
	reader   *bufio.Reader
}

type executorEnvelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Command json.RawMessage `json:"command,omitempty"`
	Success bool            `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Role    string          `json:"role,omitempty"`
	Version string          `json:"version,omitempty"`
}

func NewExecutorHub(addr string) *ExecutorHub {
	if addr == "" {
		addr = "0.0.0.0:9876"
	}
	return &ExecutorHub{addr: addr}
}

func (h *ExecutorHub) Start() error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("executor hub listen %s: %w", h.addr, err)
	}
	h.listener = ln
	log.Printf("Executor hub listening on %s (clients connect here)", h.addr)
	go h.acceptLoop()
	return nil
}

func (h *ExecutorHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil {
		_ = h.conn.Close()
		h.conn = nil
	}
	if h.listener != nil {
		_ = h.listener.Close()
		h.listener = nil
	}
}

func (h *ExecutorHub) HasClient() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

func (h *ExecutorHub) acceptLoop() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return
		}
		log.Printf("Executor client connected from %s", conn.RemoteAddr())
		h.mu.Lock()
		if h.conn != nil {
			_ = h.conn.Close()
		}
		h.conn = conn
		h.reader = bufio.NewReader(conn)
		h.mu.Unlock()

		// Expect hello
		if err := h.handshake(); err != nil {
			log.Printf("Executor handshake failed: %v", err)
			h.mu.Lock()
			_ = conn.Close()
			h.conn = nil
			h.reader = nil
			h.mu.Unlock()
		}
	}
}

func (h *ExecutorHub) handshake() error {
	h.mu.Lock()
	reader := h.reader
	conn := h.conn
	h.mu.Unlock()
	if reader == nil || conn == nil {
		return fmt.Errorf("no connection")
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := reader.ReadBytes('\n')
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}
	var env executorEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return err
	}
	if env.Type != "hello" || env.Role != "executor" {
		return fmt.Errorf("expected hello from executor, got %s/%s", env.Type, env.Role)
	}
	ack, _ := json.Marshal(executorEnvelope{Type: "hello_ack", Role: "bridge", Version: "1"})
	ack = append(ack, '\n')
	_, err = conn.Write(ack)
	return err
}

func (h *ExecutorHub) SendCommand(cmd IPCCommand, timeout time.Duration) (*IPCResponse, error) {
	h.mu.Lock()
	conn := h.conn
	reader := h.reader
	h.mu.Unlock()
	if conn == nil || reader == nil {
		return nil, fmt.Errorf("no executor client connected")
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	env := executorEnvelope{Type: "command", ID: id, Command: cmdBytes}
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != conn {
		return nil, fmt.Errorf("executor client changed during send")
	}

	_ = conn.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	if _, err := conn.Write(payload); err != nil {
		h.dropLocked()
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			h.dropLocked()
			return nil, err
		}
		var respEnv executorEnvelope
		if err := json.Unmarshal(line, &respEnv); err != nil {
			continue
		}
		if respEnv.Type == "pong" {
			continue
		}
		if respEnv.Type != "result" || respEnv.ID != id {
			continue
		}
		out := &IPCResponse{Success: respEnv.Success, Error: respEnv.Error}
		if len(respEnv.Data) > 0 {
			_ = json.Unmarshal(respEnv.Data, &out.Data)
		}
		return out, nil
	}
	return nil, fmt.Errorf("timeout waiting for executor result")
}

func (h *ExecutorHub) dropLocked() {
	if h.conn != nil {
		_ = h.conn.Close()
	}
	h.conn = nil
	h.reader = nil
}
