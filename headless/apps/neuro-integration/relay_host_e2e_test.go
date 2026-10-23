package main

import (
	"strings"
	"testing"
	"time"
)

// The bridge's own relay client (relay.go) against the Go relay host
// (relay_host.go). This is the check that the shipped pair actually works.
func TestRelayClientAgainstTheGoHost(t *testing.T) {
	srv := newRelayTestServer(t, "")

	watcher := dialRelay(t, srv.url, nil)
	register(t, watcher, "neuro-os", "Vedal OS", testRelayToken)
	readUntil(t, watcher, event("neuroos_connected"))

	_, relay := newRelayTestHarness(t, srv.url, testRelayToken)
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)

	waitUntilTrue(t, func() bool { return relay.status().Registered })
	arrived := readUntil(t, watcher, func(m map[string]interface{}) bool {
		return m["event"] == "integration_connected" && m["name"] == "Neuro Desktop"
	})
	if arrived["name"] != "Neuro Desktop" {
		t.Fatalf("watcher saw %v", arrived)
	}

	status := relay.status()
	if !status.Connected || !status.Registered || status.LastError != "" {
		t.Fatalf("status after registration = %+v", status)
	}
	if status.PeerCount != 0 {
		// The watcher is not an integration, so it is not a peer.
		t.Fatalf("peer count = %d", status.PeerCount)
	}
}

func TestRelayClientReportsAWrongTokenClearly(t *testing.T) {
	srv := newRelayTestServer(t, "")
	_, relay := newRelayTestHarness(t, srv.url, "not-the-right-token-0000")
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)

	waitUntilTrue(t, func() bool { return strings.Contains(relay.status().LastError, "invalid auth token") })
	if relay.status().Registered {
		t.Fatal("a wrong token must not register")
	}
}

func TestRelayClientReportsAnAbsentRelayAsUnreachable(t *testing.T) {
	// Port 1 is never a relay; the dial is refused straight away.
	_, relay := newRelayTestHarness(t, "ws://127.0.0.1:1", testRelayToken)
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if relay.status().LastEvent == "unreachable" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status = %+v, want last_event unreachable", relay.status())
}
