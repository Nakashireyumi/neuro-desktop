package main

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// withLivenessSchedule compresses the ping schedule for one hub. Without it the
// keepalive tests would have to wait 90 seconds. The schedule lives on the hub,
// not in package state: a keepalive goroutine outliving the test used to read
// the package variables while the next test wrote them (`go test -race` caught
// it).
func withLivenessSchedule(t *testing.T, hub *ExecutorHub, ping, dead time.Duration) {
	t.Helper()
	hub.setLivenessSchedule(ping, dead)
}

// A quiet bridge is indistinguishable from a dead one to a client that reads
// with a socket timeout: every client we ship reconnected on a timer (visible
// as a rising total_connections). The hub must therefore keep talking.
func TestExecutorHubPingsIdleClients(t *testing.T) {
	hub := newHub(t, "")
	withLivenessSchedule(t, hub, 40*time.Millisecond, 5*time.Second)

	executor := startFakeExecutor(t, hub, "", nil)
	defer executor.conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if executor.seenPings() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hub never pinged a connected executor (total_connections would keep climbing)")
}

// A client that is gone must not hold the session: otherwise every action waits
// for the request timeout instead of failing fast.
func TestExecutorHubDropsClientThatStopsAnswering(t *testing.T) {
	hub := newHub(t, "")
	withLivenessSchedule(t, hub, 40*time.Millisecond, 120*time.Millisecond)

	// Raw client: complete the handshake, then read frames and never answer.
	conn, err := net.Dial("tcp", hub.Addr())
	if err != nil {
		t.Fatalf("failed to dial hub: %v", err)
	}
	defer conn.Close()

	hello, _ := json.Marshal(map[string]string{"type": "hello", "role": "executor", "version": "2"})
	if _, err := conn.Write(append(hello, '\n')); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read handshake reply: %v", err)
	}
	if !strings.Contains(line, "hello_ack") {
		t.Fatalf("expected hello_ack, got %q", line)
	}

	// Drain whatever the hub sends (pings) without answering.
	go func() {
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info := hub.Info()
		if !info.Connected {
			if !strings.Contains(info.LastError, "ping") {
				t.Fatalf("connection dropped without explaining why: %q", info.LastError)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hub kept a client that stopped answering pings")
}

// A shutdown request is not the only way a session ends: the hub must survive a
// client vanishing mid-session and accept the next one.
func TestExecutorHubAcceptsANewClientAfterADrop(t *testing.T) {
	hub := newHub(t, "")
	withLivenessSchedule(t, hub, 40*time.Millisecond, 120*time.Millisecond)

	first := startFakeExecutor(t, hub, "", nil)
	_ = first.conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !hub.HasClient() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	second := startFakeExecutor(t, hub, "", func(cmd IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true}, true
	})
	defer second.conn.Close()

	response, err := hub.SendCommand(IPCCommand{Type: CmdGetStatus}, 2*time.Second)
	if err != nil {
		t.Fatalf("SendCommand after reconnect failed: %v", err)
	}
	if !response.Success {
		t.Fatalf("unexpected response after reconnect: %+v", response)
	}
}
