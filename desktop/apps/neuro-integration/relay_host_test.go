package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testRelayToken = "relay-test-token-0123456789"
const testEnhancedToken = "enhanced-test-token-9876543210"

type relayTestServer struct {
	host   *relayHost
	server *httptest.Server
	url    string
}

func newRelayTestServer(t *testing.T, neuroURL string) *relayTestServer {
	t.Helper()
	logger := log.New(io.Discard, "", 0)
	host := newRelayHost(testRelayToken, testEnhancedToken, neuroURL, logger)
	server := httptest.NewServer(host)
	t.Cleanup(server.Close)
	return &relayTestServer{host: host, server: server, url: "ws" + strings.TrimPrefix(server.URL, "http")}
}

func dialRelay(t *testing.T, url string, headers http.Header) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func register(t *testing.T, conn *websocket.Conn, kind, name, token string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]string{"type": kind, "name": name, "auth_token": token}); err != nil {
		t.Fatalf("registration write failed: %v", err)
	}
}

// readUntil reads frames until one satisfies match, or fails after a timeout.
func readUntil(t *testing.T, conn *websocket.Conn, match func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("no matching frame before the deadline: %v", err)
		}
		if match(msg) {
			return msg
		}
	}
}

func event(name string) func(map[string]interface{}) bool {
	return func(msg map[string]interface{}) bool { return msg["event"] == name }
}

func TestRelayAcceptsIntegrationsAndWatchersAndRoutesCommands(t *testing.T) {
	srv := newRelayTestServer(t, "")

	watcher := dialRelay(t, srv.url, nil)
	register(t, watcher, "neuro-os", "Vedal OS", testRelayToken)
	readUntil(t, watcher, func(m map[string]interface{}) bool {
		return m["event"] == "neuroos_connected" && m["name"] == "Vedal OS" && m["privileges"] == "standard"
	})

	integration := dialRelay(t, srv.url, nil)
	register(t, integration, "integration", "Spotify", testRelayToken)
	readUntil(t, watcher, func(m map[string]interface{}) bool {
		return m["event"] == "integration_connected" && m["name"] == "Spotify"
	})

	if err := integration.WriteJSON(map[string]interface{}{
		"event":   "register_actions",
		"actions": map[string]interface{}{"play": map[string]interface{}{"type": "object"}, "pause": map[string]interface{}{"type": "object"}},
	}); err != nil {
		t.Fatal(err)
	}
	announced := readUntil(t, watcher, event("integration_registered_actions"))
	if announced["from"] != "Spotify" || len(announced["actions"].([]interface{})) != 2 {
		t.Fatalf("announcement = %v", announced)
	}

	if err := integration.WriteJSON(map[string]string{"action": "hello"}); err != nil {
		t.Fatal(err)
	}
	seen := readUntil(t, watcher, event("integration_message"))
	if payload := seen["payload"].(map[string]interface{}); payload["action"] != "hello" {
		t.Fatalf("integration message = %v", seen)
	}

	if err := watcher.WriteJSON(map[string]interface{}{
		"target": "Spotify",
		"cmd":    map[string]interface{}{"action": "play", "params": map[string]interface{}{}},
	}); err != nil {
		t.Fatal(err)
	}
	ack := readUntil(t, watcher, func(m map[string]interface{}) bool { return m["status"] != nil || m["error"] != nil })
	if ack["status"] != "sent" {
		t.Fatalf("watcher ack = %v", ack)
	}
	delivered := readUntil(t, integration, func(m map[string]interface{}) bool { return m["from_watcher"] != nil })
	if delivered["from_watcher"] != "Vedal OS" || delivered["cmd"].(map[string]interface{})["action"] != "play" {
		t.Fatalf("delivered = %v", delivered)
	}

	// Disconnecting the integration tells the watcher.
	_ = integration.Close()
	readUntil(t, watcher, event("integration_disconnected"))
}

func TestRelayRefusesBadRegistrations(t *testing.T) {
	srv := newRelayTestServer(t, "")

	cases := []struct {
		name   string
		frame  string
		expect string
	}{
		{"wrong token", `{"type":"integration","name":"x","auth_token":"nope"}`, "invalid auth token"},
		{"not JSON", `not json at all`, "registration must be JSON"},
		{"unknown type", `{"type":"toaster","name":"x","auth_token":"` + testRelayToken + `"}`, "unknown registration type"},
		{"bad name", `{"type":"integration","name":"../../etc","auth_token":"` + testRelayToken + `"}`, "name must be"},
		{"enhanced token on an integration", `{"type":"integration","name":"x","auth_token":"` + testEnhancedToken + `"}`, "invalid auth token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := dialRelay(t, srv.url, nil)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(tc.frame)); err != nil {
				t.Fatal(err)
			}
			msg := readUntil(t, conn, func(m map[string]interface{}) bool { return m["error"] != nil })
			if !strings.Contains(msg["error"].(string), tc.expect) {
				t.Fatalf("error = %v, want it to mention %q", msg["error"], tc.expect)
			}
		})
	}
}

func TestRelayRefusesBrowsersAndBinaryFrames(t *testing.T) {
	srv := newRelayTestServer(t, "")

	headers := http.Header{}
	headers.Set("Origin", "https://evil.example")
	if _, resp, err := websocket.DefaultDialer.Dial(srv.url, headers); err == nil {
		t.Fatal("a browser origin must not connect")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for an Origin header, got %v / %v", resp, err)
	}

	integration := dialRelay(t, srv.url, nil)
	register(t, integration, "integration", "Binary", testRelayToken)
	if err := integration.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	msg := readUntil(t, integration, func(m map[string]interface{}) bool { return m["error"] != nil })
	if !strings.Contains(msg["error"].(string), "binary") {
		t.Fatalf("binary error = %v", msg)
	}
}

func TestRelayWatcherCommandsFailClearly(t *testing.T) {
	srv := newRelayTestServer(t, "")
	watcher := dialRelay(t, srv.url, nil)
	register(t, watcher, "neuro-os", "Watcher", testRelayToken)
	readUntil(t, watcher, event("neuroos_connected"))

	_ = watcher.WriteMessage(websocket.TextMessage, []byte(`{"target":"ghost","cmd":{}}`))
	msg := readUntil(t, watcher, func(m map[string]interface{}) bool { return m["error"] != nil })
	if msg["error"] != "invalid target/cmd" {
		t.Fatalf("unknown target = %v", msg)
	}

	_ = watcher.WriteMessage(websocket.TextMessage, []byte(`{"target":"ghost","cmd":"not an object"}`))
	msg = readUntil(t, watcher, func(m map[string]interface{}) bool { return m["error"] != nil })
	if msg["error"] != "invalid target/cmd" {
		t.Fatalf("string cmd = %v", msg)
	}

	_ = watcher.WriteMessage(websocket.TextMessage, []byte(`{"direct_to_neuro":true,"payload":{"command":"startup"}}`))
	msg = readUntil(t, watcher, func(m map[string]interface{}) bool { return m["error"] != nil })
	if !strings.Contains(msg["error"].(string), "enhanced") {
		t.Fatalf("a standard watcher must not reach Neuro: %v", msg)
	}
}

func TestRelayDirectToNeuroNeedsTheEnhancedTokenAndALink(t *testing.T) {
	neuro := newFakeNeuroAPI(t)
	srv := newRelayTestServer(t, neuro.url)
	go srv.host.runNeuroLink(make(chan struct{}))

	waitUntilTrue(t, func() bool { return srv.host.neuro.connected() })

	watcher := dialRelay(t, srv.url, nil)
	register(t, watcher, "neuro-os", "Operator", testEnhancedToken)
	enhanced := readUntil(t, watcher, event("neuroos_connected"))
	if enhanced["privileges"] != "enhanced" {
		t.Fatalf("privileges = %v", enhanced["privileges"])
	}

	if err := watcher.WriteJSON(map[string]interface{}{
		"direct_to_neuro": true,
		"payload":         map[string]interface{}{"command": "context", "data": map[string]interface{}{"message": "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	ack := readUntil(t, watcher, func(m map[string]interface{}) bool { return m["status"] != nil || m["error"] != nil })
	if ack["status"] != "forwarded_to_neuro" {
		t.Fatalf("ack = %v", ack)
	}
	got := neuro.next(t)
	if got["command"] != "context" {
		t.Fatalf("the Neuro backend received %v", got)
	}
}

func TestRelayDirectToNeuroWithoutALinkSaysSo(t *testing.T) {
	srv := newRelayTestServer(t, "")
	watcher := dialRelay(t, srv.url, nil)
	register(t, watcher, "neuro-os", "Operator", testEnhancedToken)
	readUntil(t, watcher, event("neuroos_connected"))
	_ = watcher.WriteJSON(map[string]interface{}{"direct_to_neuro": true, "payload": map[string]string{"command": "startup"}})
	msg := readUntil(t, watcher, func(m map[string]interface{}) bool { return m["error"] != nil })
	if msg["error"] != "neuro backend not available" {
		t.Fatalf("error = %v", msg)
	}
}

func TestRelayReplacesAnIntegrationWithTheSameName(t *testing.T) {
	srv := newRelayTestServer(t, "")
	first := dialRelay(t, srv.url, nil)
	register(t, first, "integration", "Twin", testRelayToken)
	waitUntilTrue(t, func() bool { return srv.host.integration("Twin") != nil })

	second := dialRelay(t, srv.url, nil)
	register(t, second, "integration", "Twin", testRelayToken)

	msg := readUntil(t, first, func(m map[string]interface{}) bool { return m["error"] != nil })
	if !strings.Contains(msg["error"].(string), "replaced") {
		t.Fatalf("the old connection should be told why: %v", msg)
	}
	waitUntilTrue(t, func() bool {
		peer := srv.host.integration("Twin")
		return peer != nil && peer.conn != nil
	})
	// The new connection must survive the old one going away.
	_ = first.Close()
	time.Sleep(100 * time.Millisecond)
	if srv.host.integration("Twin") == nil {
		t.Fatal("closing the replaced connection removed the new one")
	}
}

func TestRelayRateLimitsAChattyPeer(t *testing.T) {
	srv := newRelayTestServer(t, "")
	integration := dialRelay(t, srv.url, nil)
	register(t, integration, "integration", "Chatty", testRelayToken)
	waitUntilTrue(t, func() bool { return srv.host.integration("Chatty") != nil })

	limited := false
	for i := 0; i < relayFramesPerSecond+20; i++ {
		_ = integration.WriteMessage(websocket.TextMessage, []byte(`{"action":"spam"}`))
	}
	for i := 0; i < relayFramesPerSecond+20; i++ {
		_ = integration.SetReadDeadline(time.Now().Add(3 * time.Second))
		var msg map[string]interface{}
		if err := integration.ReadJSON(&msg); err != nil {
			break
		}
		if msg["error"] != nil && strings.Contains(msg["error"].(string), "slow down") {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("a flood of frames should get a slow-down reply")
	}
}

func TestRelayHealthReportsNamesAndCountsOnly(t *testing.T) {
	srv := newRelayTestServer(t, "")
	integration := dialRelay(t, srv.url, nil)
	register(t, integration, "integration", "Spotify", testRelayToken)
	waitUntilTrue(t, func() bool { return srv.host.integration("Spotify") != nil })

	rec := httptest.NewRecorder()
	srv.host.healthHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Spotify") || strings.Contains(body, testRelayToken) {
		t.Fatalf("health body = %s", body)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["neuro_link"] != "not_configured" {
		t.Fatalf("neuro_link = %v", parsed["neuro_link"])
	}
}

func TestRelayTokenPolicy(t *testing.T) {
	if err := validateRelayToken("super-secret-token"); err == nil {
		t.Fatal("the upstream sample token must be refused")
	}
	if err := validateRelayToken("short"); err == nil {
		t.Fatal("short tokens must be refused")
	}
	if err := validateRelayToken(testRelayToken); err != nil {
		t.Fatalf("a good token was refused: %v", err)
	}
}

func TestRelayGeneratesATokenWhenNoneIsSet(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "relay-token")
	token, where, created, err := resolveRelayToken("", file)
	if err != nil {
		t.Fatal(err)
	}
	if where != file || len(token) < 32 || !created {
		t.Fatalf("token = %q, where = %q, created = %v", token, where, created)
	}
	again, _, reused, err := resolveRelayToken("", file)
	if err != nil || again != token || reused {
		t.Fatalf("the generated token should persist across restarts: %q vs %q (created again: %v, %v)", token, again, reused, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("token file mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

// fakeNeuroAPI stands in for the Neuro API server.
type fakeNeuroAPI struct {
	url      string
	received chan map[string]interface{}
}

func newFakeNeuroAPI(t *testing.T) *fakeNeuroAPI {
	t.Helper()
	fake := &fakeNeuroAPI{received: make(chan map[string]interface{}, 8)}
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			fake.received <- msg
		}
	}))
	t.Cleanup(server.Close)
	fake.url = "ws" + strings.TrimPrefix(server.URL, "http")
	return fake
}

func (f *fakeNeuroAPI) next(t *testing.T) map[string]interface{} {
	t.Helper()
	select {
	case msg := <-f.received:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("the fake Neuro backend got nothing")
		return nil
	}
}

func waitUntilTrue(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// A watcher is a second driver of the desktop. It must not get around the
// per-scope rate limit that Neuro's own actions are held to.
func TestWatcherCommandsShareTheRateLimit(t *testing.T) {
	policy, err := loadPermissionPolicy(writeTempPolicy(t, `{
	  "default_allow": true,
	  "scopes": {"input": {"allowed": true, "limits": {"max_actions_per_minute": 2}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	integration := &NDIntegration{
		stats: newBridgeStats(),
		relay: newRelayState(RelayConfig{}),
		stop:  newStopSwitch(),
		audit: newAuditor(),
	}
	integration.setPolicy(policy)

	for i := 1; i <= 2; i++ {
		if reason := integration.denyRelayCommand("mouse_click", "watcher"); reason != "" {
			t.Fatalf("command %d within the limit was refused: %s", i, reason)
		}
	}
	if reason := integration.denyRelayCommand("mouse_click", "watcher"); reason == "" {
		t.Fatal("a watcher must not exceed the input rate limit")
	}
	if reason := integration.denyRelayCommand("get_status", "watcher"); reason != "" {
		t.Fatalf("an action outside the limited scope should not be throttled: %s", reason)
	}
}

func TestWatcherCommandsObeyThePauseBrake(t *testing.T) {
	integration := &NDIntegration{
		stats: newBridgeStats(),
		relay: newRelayState(RelayConfig{}),
		stop:  newStopSwitch(),
		audit: newAuditor(),
	}
	integration.stop.setPaused(true)
	if reason := integration.denyRelayCommand("mouse_click", "watcher"); reason == "" {
		t.Fatal("a paused bridge must refuse watcher input")
	}
}
