package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The relay host and the server must read one value. These tests cover the
// file both of them use, and the cases where the environment disagrees.

func TestRelayClientTokenPrefersTheFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "relay-token")
	token, created, err := ensureRelayTokenFile(file)
	if err != nil || !created {
		t.Fatalf("first call should create the token: %v (created %v)", err, created)
	}

	got, err := resolveRelayClientToken("", file)
	if err != nil || got != token {
		t.Fatalf("the server should read the relay host's token from the file: %q, %v", got, err)
	}
	// The same value in the environment is fine.
	got, err = resolveRelayClientToken(token, file)
	if err != nil || got != token {
		t.Fatalf("an agreeing NEURO_RELAY_TOKEN should be accepted: %q, %v", got, err)
	}
}

func TestRelayClientTokenRefusesDisagreement(t *testing.T) {
	file := filepath.Join(t.TempDir(), "relay-token")
	if _, _, err := ensureRelayTokenFile(file); err != nil {
		t.Fatal(err)
	}
	_, err := resolveRelayClientToken("0123456789abcdef0123456789abcdef", file)
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("a different NEURO_RELAY_TOKEN must be refused, got %v", err)
	}
}

func TestRelayClientTokenWithNoFileIsEmptyNotAnError(t *testing.T) {
	got, err := resolveRelayClientToken("", filepath.Join(t.TempDir(), "absent"))
	if err != nil || got != "" {
		t.Fatalf("no token anywhere should be an empty token for the caller to report, got %q, %v", got, err)
	}
}

func TestRelayClientTokenRefusesTheUpstreamSample(t *testing.T) {
	file := filepath.Join(t.TempDir(), "relay-token")
	if err := os.WriteFile(file, []byte(relayUpstreamSampleToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRelayClientToken("", file); err == nil {
		t.Fatal("the upstream sample token in the file must be refused")
	}
}

func TestInvalidRelayTokenFileIsNeverOverwritten(t *testing.T) {
	file := filepath.Join(t.TempDir(), "relay-token")
	const operatorValue = "too-short"
	if err := os.WriteFile(file, []byte(operatorValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ensureRelayTokenFile(file); err == nil {
		t.Fatal("an invalid token must be reported, not replaced")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != operatorValue {
		t.Fatalf("the operator's file was changed: %q", data)
	}
}

func TestRelayStartDoesNotDialWithoutAToken(t *testing.T) {
	state := newRelayState(RelayConfig{Enabled: true, IntermediaryURL: "ws://127.0.0.1:1", TokenError: "no relay token: run setup"})
	if err := state.Start(); err != nil {
		t.Fatal(err)
	}
	if got := state.status().LastError; got != "no relay token: run setup" {
		t.Fatalf("the reason should be visible on the dashboard, got %q", got)
	}
	state.Stop()
}
