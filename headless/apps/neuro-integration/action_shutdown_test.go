package main

import (
	"testing"
	"time"
)

// A backend shutdown must not wait for a slow executor command, and Neuro must
// see shutdown/ready before the SDK closes so the disconnect is not mistaken for
// a crash.
func TestBackendShutdownReadyDoesNotWaitForBusyExecutor(t *testing.T) {
	harness := newBridgeHarness(t, allowEverythingPolicy)
	harness.backend.waitFor("actions/register", 5*time.Second)

	// Simulate an action holding the serialized IPC gate for a long OS call.
	harness.integration.ipcMu.Lock()
	locked := true
	defer func() {
		if locked {
			harness.integration.ipcMu.Unlock()
		}
	}()

	finished := make(chan struct{})
	go func() {
		harness.integration.shutdownFromBackend(CmdShutdownGracefully)
		close(finished)
	}()

	harness.backend.waitFor("shutdown/ready", 2*time.Second)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked behind an in-flight executor command")
	}
	select {
	case <-harness.integration.done:
	default:
		t.Fatal("shutdown did not mark the bridge as done")
	}
	if harness.hub.HasClient() {
		t.Fatal("shutdown left the executor session open instead of closing it best-effort")
	}

	harness.integration.ipcMu.Unlock()
	locked = false
}

func TestBackendShutdownRequestsExecutorReleaseWhenIdle(t *testing.T) {
	harness := newBridgeHarness(t, allowEverythingPolicy)
	harness.backend.waitFor("actions/register", 5*time.Second)

	harness.integration.shutdownFromBackend(CmdShutdownGracefully)
	harness.backend.waitFor("shutdown/ready", 2*time.Second)
	harness.waitForExecutorCommand(CmdShutdownGracefully, 2*time.Second)
}
