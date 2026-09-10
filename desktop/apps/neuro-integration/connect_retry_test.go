package main

import (
	"testing"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

func TestSDKConnectFailureCanRetryWithoutDeadlock(t *testing.T) {
	client, err := neuro.NewClient(neuro.ClientConfig{
		Game:         "neuro-desktop-test",
		WebsocketURL: "ws://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = client.Connect()
		_ = client.Connect()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Connect retry deadlocked after failed connection attempt")
	}
}
