package main

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeExecutor is a stand-in for the executor (the Python agent in
// desktop/backend/python/controller/agent.py): it performs the hello
// handshake and answers commands and pings the way the real client does.
type fakeExecutor struct {
	t       *testing.T
	conn    net.Conn
	reader  *bufio.Reader
	handler func(cmd IPCCommand) (*IPCResponse, bool) // response, shouldReply

	mu       sync.Mutex
	commands []IPCCommand
	pings    int
	// nacked is set when the hub refused this client's hello.
	nacked bool
}

func startFakeExecutor(t *testing.T, hub *ExecutorHub, token string, handler func(IPCCommand) (*IPCResponse, bool)) *fakeExecutor {
	t.Helper()

	conn, err := net.Dial("tcp", hub.Addr())
	if err != nil {
		t.Fatalf("failed to dial hub: %v", err)
	}

	executor := &fakeExecutor{t: t, conn: conn, reader: bufio.NewReader(conn), handler: handler}

	hello := map[string]interface{}{"type": "hello", "role": "executor", "version": "2"}
	if token != "" {
		hello["token"] = token
	}
	if err := executor.write(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	line, err := executor.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read handshake reply: %v", err)
	}
	var reply map[string]interface{}
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		t.Fatalf("handshake reply is not JSON: %v", err)
	}

	switch reply["type"] {
	case "hello_ack":
		go executor.serve()
	case "hello_nack":
		// Callers asserting on rejection read `reply` through the returned
		// executor; mark it so tests can tell the two apart.
		executor.nacked = true
	default:
		t.Fatalf("unexpected handshake reply: %v", reply)
	}

	return executor
}

func (f *fakeExecutor) write(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.conn.Write(data)
	return err
}

func (f *fakeExecutor) serve() {
	for {
		line, err := f.reader.ReadString('\n')
		if err != nil {
			return
		}

		var env struct {
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			Command json.RawMessage `json:"command"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}

		switch env.Type {
		case "ping":
			f.mu.Lock()
			f.pings++
			f.mu.Unlock()
			_ = f.write(map[string]interface{}{"type": "pong", "id": env.ID})
		case "command":
			var cmd IPCCommand
			if err := json.Unmarshal(env.Command, &cmd); err != nil {
				continue
			}
			f.mu.Lock()
			f.commands = append(f.commands, cmd)
			f.mu.Unlock()

			if f.handler == nil {
				continue
			}
			response, reply := f.handler(cmd)
			if !reply {
				continue
			}
			_ = f.write(map[string]interface{}{
				"type":    "result",
				"id":      env.ID,
				"success": response.Success,
				"data":    response.Data,
				"error":   response.Error,
			})
		}
	}
}

func (f *fakeExecutor) seenPings() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pings
}

func (f *fakeExecutor) seenCommands() []IPCCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]IPCCommand, len(f.commands))
	copy(out, f.commands)
	return out
}

func newHub(t *testing.T, token string) *ExecutorHub {
	t.Helper()
	hub := NewExecutorHub("127.0.0.1:0", token)
	if err := hub.Start(); err != nil {
		t.Fatalf("failed to start hub: %v", err)
	}
	t.Cleanup(hub.Close)
	return hub
}

func TestExecutorHubRoundTrip(t *testing.T) {
	hub := newHub(t, "")

	executor := startFakeExecutor(t, hub, "", func(cmd IPCCommand) (*IPCResponse, bool) {
		if cmd.Type != CmdMouseMove {
			t.Errorf("unexpected command: %s", cmd.Type)
		}
		return &IPCResponse{Success: true, Data: map[string]interface{}{"moved": true}}, true
	})
	defer executor.conn.Close()

	if !hub.HasClient() {
		t.Fatal("hub should report a connected client")
	}

	response, err := hub.SendCommand(IPCCommand{Type: CmdMouseMove, Params: map[string]interface{}{"x": 10, "y": 20}}, 2*time.Second)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success, got %+v", response)
	}
	if moved, _ := response.Data["moved"].(bool); !moved {
		t.Fatalf("expected the executor payload to be passed through, got %+v", response.Data)
	}
}

func TestExecutorHubRequiresToken(t *testing.T) {
	hub := newHub(t, "s3cret")

	// The token-protected hub must refuse an executor that does not present it.
	if !hub.tokenAccepted("", "1.2.3.4:5000") {
		// expected
	} else {
		t.Fatal("empty token must be refused")
	}
	if !hub.tokenAccepted("s3cret", "127.0.0.1:5000") {
		t.Fatal("the configured token must be accepted")
	}

	executor := startFakeExecutor(t, hub, "s3cret", func(IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true}, true
	})
	defer executor.conn.Close()

	if !hub.HasClient() {
		t.Fatal("hub should have accepted the authenticated executor")
	}
}

func TestExecutorHubRejectsWrongToken(t *testing.T) {
	hub := newHub(t, "s3cret")

	conn, err := net.Dial("tcp", hub.Addr())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	payload, _ := json.Marshal(map[string]interface{}{
		"type": "hello", "role": "executor", "version": "2", "token": "wrong",
	})
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("expected a rejection frame: %v", err)
	}
	var reply map[string]interface{}
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		t.Fatalf("rejection is not JSON: %v", err)
	}
	if reply["type"] != "hello_nack" {
		t.Fatalf("expected hello_nack, got %v", reply["type"])
	}

	if hub.HasClient() {
		t.Fatal("a rejected executor must not be registered as a client")
	}
}

// A slow action must not make the bridge unresponsive: this is what used to
// block the dashboard, because the hub lock was held for the whole round trip.
func TestExecutorHubSlowCommandDoesNotBlockStatus(t *testing.T) {
	hub := newHub(t, "")

	executor := startFakeExecutor(t, hub, "", func(cmd IPCCommand) (*IPCResponse, bool) {
		time.Sleep(250 * time.Millisecond)
		return &IPCResponse{Success: true}, true
	})
	defer executor.conn.Close()

	done := make(chan error, 1)
	go func() {
		_, err := hub.SendCommand(IPCCommand{Type: CmdGetStatus}, 3*time.Second)
		done <- err
	}()

	// Give the command time to be in flight, then check that status queries and
	// completion still return immediately.
	time.Sleep(50 * time.Millisecond)

	statusStart := time.Now()
	info := hub.Info()
	if elapsed := time.Since(statusStart); elapsed > 100*time.Millisecond {
		t.Fatalf("Info() blocked for %s while a command was in flight", elapsed)
	}
	if !info.Connected {
		t.Fatal("Info() should report the connected executor")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("slow command failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow command never finished")
	}
}

// A timed-out request abandons that request only: the executor session (and the
// next command) must keep working.
func TestExecutorHubTimeoutKeepsSession(t *testing.T) {
	hub := newHub(t, "")

	first := true
	executor := startFakeExecutor(t, hub, "", func(cmd IPCCommand) (*IPCResponse, bool) {
		if first {
			first = false
			return nil, false // never answer the first command
		}
		return &IPCResponse{Success: true}, true
	})
	defer executor.conn.Close()

	if _, err := hub.SendCommand(IPCCommand{Type: CmdGetStatus}, 150*time.Millisecond); err == nil {
		t.Fatal("expected a timeout error")
	}

	if !hub.HasClient() {
		t.Fatal("a timeout must not drop the executor connection")
	}

	response, err := hub.SendCommand(IPCCommand{Type: CmdMouseClick}, 2*time.Second)
	if err != nil {
		t.Fatalf("the executor session should still work after a timeout: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success after recovery, got %+v", response)
	}

	commands := executor.seenCommands()
	if len(commands) != 2 {
		t.Fatalf("expected both commands to reach the executor, got %d", len(commands))
	}
}

func TestExecutorHubReplacementClosesPreviousClient(t *testing.T) {
	hub := newHub(t, "")

	first := startFakeExecutor(t, hub, "", func(IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true}, true
	})
	second := startFakeExecutor(t, hub, "", func(IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true}, true
	})
	defer first.conn.Close()
	defer second.conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Info().Remote == second.conn.LocalAddr().String() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := hub.SendCommand(IPCCommand{Type: CmdMouseClick}, time.Second); err != nil {
		t.Fatalf("the newest executor should receive commands: %v", err)
	}
	if len(first.seenCommands()) != 0 {
		t.Fatal("commands should go to the newest executor only")
	}

	// The replacement must be visible to the operator: a reconnect storm
	// between two executors was previously only visible as log spam.
	info := hub.Info()
	if info.Replaced != 1 {
		t.Fatalf("replaced_connections = %d, want 1", info.Replaced)
	}
	if info.Connections != 2 {
		t.Fatalf("total_connections = %d, want 2", info.Connections)
	}
}

// Without a token the hub would accept any client as the executor, and the
// executor receives Neuro's commands. A hub that is reachable from the network
// must therefore refuse to start without a token. Nothing is bound here.
func TestExecutorHubRefusesANetworkListenWithoutAToken(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9876", ":9876", "192.168.1.20:9876"} {
		hub := NewExecutorHub(addr, "")
		err := hub.Start()
		if err == nil {
			hub.Close()
			t.Fatalf("%s: a hub with no token must not listen beyond loopback", addr)
		}
		if !strings.Contains(err.Error(), "NEURO_EXECUTOR_TOKEN") {
			t.Fatalf("%s: the refusal must name the setting: %v", addr, err)
		}
	}
}

func TestExecutorHubLoopbackWithoutATokenStillStarts(t *testing.T) {
	hub := NewExecutorHub("127.0.0.1:0", "")
	if err := hub.Start(); err != nil {
		t.Fatalf("a loopback hub without a token is the single-machine default: %v", err)
	}
	hub.Close()
}
