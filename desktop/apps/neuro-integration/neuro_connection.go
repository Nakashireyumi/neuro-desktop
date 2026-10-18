package main

import (
	"sync"
	"time"
)

// The HTTP server is available while Start retries the Neuro API. Report that
// connection separately so a healthy dashboard cannot imply Neuro is connected.
type neuroConnectionState struct {
	mu          sync.Mutex
	state       string
	attempts    int
	lastError   string
	connectedAt string
}

func (s *neuroConnectionState) connecting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = "connecting"
	s.attempts++
}

func (s *neuroConnectionState) connected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = "connected"
	s.lastError = ""
	s.connectedAt = time.Now().UTC().Format(time.RFC3339)
}

func (s *neuroConnectionState) disconnected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = "disconnected"
	// Dial errors can contain credentials from a WebSocket URL. Keep those in
	// the server log; dashboard responses carry only this actionable explanation.
	s.lastError = "Cannot reach the Neuro API. Check its WebSocket address and the server logs."
}

func (s *neuroConnectionState) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state == "" {
		state = "waiting"
	}
	return map[string]interface{}{
		"state": state, "connected": state == "connected", "attempts": s.attempts,
		"last_error": s.lastError, "connected_at": s.connectedAt,
	}
}
