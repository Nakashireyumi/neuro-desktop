package main

// Neuro Relay host: the intermediary, written in Go.
//
// It speaks the wire protocol of Nakashireyumi/neuro-relay's intermediary for the
// parts Neuro Desktop uses, so existing integrations and Neuro-OS watchers keep
// working. It is one process of the same binary as the bridge:
//
//	neuro-integration relay --listen 127.0.0.1:8765
//
// Protocol (JSON text frames):
//
//	register    first frame: {"type": "integration" | "neuro-os", "name": "...", "auth_token": "..."}
//	            bad token -> {"error": "invalid auth token"} and close
//	integration {"event": "register_actions", "actions": {...}} is announced to watchers
//	            every other frame is delivered to watchers as integration_message
//	watcher     {"target": "<integration>", "cmd": {...}} is delivered to the integration
//	            as {"from_watcher": "<watcher>", "cmd": {...}}; the watcher gets {"status": "sent"}
//	            {"direct_to_neuro": true, "payload": {...}} goes to the Neuro API, enhanced watchers only
//	events      integration_connected, integration_disconnected, neuroos_connected,
//	            neuroos_disconnected, integration_message, integration_registered_actions,
//	            backend_message (Neuro API traffic, when a Neuro link is configured)
//
// Differences from the upstream intermediary, all deliberate:
//   - No default token. The host refuses the upstream sample token and any token
//     shorter than 16 characters; when none is set it generates one and writes it
//     to a 0600 file.
//   - Browsers are refused. Any request with an Origin header is rejected, so a web
//     page cannot drive the relay through the user's browser.
//   - Binary frames are refused. The upstream writes them to disk under a name the
//     client chose.
//   - Each connection is rate limited, frames are size limited, and idle
//     connections are dropped.
//   - Integration messages are not forwarded to a Neuro backend. The upstream
//     forwards them in a Nakurity-specific envelope; the Neuro API has no such
//     message. Neuro is driven by the bridge's own Neuro API connection.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	relayMaxFrameBytes       = 256 << 10
	relayRegisterTimeout     = 10 * time.Second
	relayIdleTimeout         = 90 * time.Second
	relayPingInterval        = 30 * time.Second
	relayWriteTimeout        = 10 * time.Second
	relayMaxIntegrations     = 64
	relayMaxWatchers         = 32
	relayFramesPerSecond     = 50
	relayMinTokenLength      = 16
	relayDefaultTokenFile    = "./relay-token"
	relayTokenFileEnv        = "NEURO_RELAY_TOKEN_FILE"
	relayUpstreamSampleToken = "super-secret-token"
)

var relayNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.\-]{0,63}$`)

// closeAfterAnswer closes a refused connection without losing the refusal. Closing
// a TCP socket at once can reset it before the peer has read what was written. On
// Windows the peer then reports "connection reset" instead of the reason. So the
// host waits, for at most a couple of seconds, for the peer to hang up first.
func closeAfterAnswer(conn *websocket.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	_ = conn.Close()
}

// relayPeer is one connected integration or watcher.
type relayPeer struct {
	kind     string // "integration" or "neuro-os"
	name     string
	enhanced bool // neuro-os only: connected with the enhanced token
	conn     *websocket.Conn
	writeMu  sync.Mutex
	since    time.Time

	// a simple per-second frame budget
	budgetMu    sync.Mutex
	budgetStart time.Time
	budgetCount int
}

func (p *relayPeer) send(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(relayWriteTimeout))
	return p.conn.WriteMessage(websocket.TextMessage, data)
}

// allowFrame enforces relayFramesPerSecond for this connection.
func (p *relayPeer) allowFrame(now time.Time) bool {
	p.budgetMu.Lock()
	defer p.budgetMu.Unlock()
	if now.Sub(p.budgetStart) >= time.Second {
		p.budgetStart = now
		p.budgetCount = 0
	}
	p.budgetCount++
	return p.budgetCount <= relayFramesPerSecond
}

// relayNeuroLink is the optional connection to a Neuro API server, used only for
// direct_to_neuro messages from enhanced watchers.
type relayNeuroLink struct {
	url     string
	mu      sync.Mutex
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (l *relayNeuroLink) connected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.conn != nil
}

func (l *relayNeuroLink) send(data []byte) error {
	l.mu.Lock()
	conn := l.conn
	l.mu.Unlock()
	if conn == nil {
		return errors.New("neuro backend not connected")
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(relayWriteTimeout))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// relayHost holds the live registry of peers.
type relayHost struct {
	token       string
	enhancedTok string
	neuro       *relayNeuroLink
	logger      *log.Logger
	startedAt   time.Time
	upgrader    websocket.Upgrader

	mu           sync.Mutex
	integrations map[string]*relayPeer
	watchers     map[string]*relayPeer
	actions      map[string][]string // integration -> action names it announced
}

func newRelayHost(token, enhancedToken, neuroURL string, logger *log.Logger) *relayHost {
	h := &relayHost{
		token:        token,
		enhancedTok:  enhancedToken,
		logger:       logger,
		startedAt:    time.Now(),
		integrations: map[string]*relayPeer{},
		watchers:     map[string]*relayPeer{},
		actions:      map[string][]string{},
	}
	if neuroURL != "" {
		h.neuro = &relayNeuroLink{url: neuroURL}
	}
	// Browsers always send an Origin header; refusing any origin keeps web pages out.
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  16 << 10,
		WriteBufferSize: 16 << 10,
		CheckOrigin:     func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
	}
	return h
}

// validateRelayToken refuses tokens that are too weak to protect a socket that
// other programs on the machine can reach.
func validateRelayToken(token string) error {
	if token == relayUpstreamSampleToken {
		return errors.New("refusing the upstream sample token; choose your own intermediary token")
	}
	if len(token) < relayMinTokenLength {
		return fmt.Errorf("the relay token must be at least %d characters", relayMinTokenLength)
	}
	return nil
}

// relayTokenFilePath is the file that holds the relay token. The relay host
// writes it and the server reads it, so both sides always use the same value.
func relayTokenFilePath() string {
	return envOr(relayTokenFileEnv, relayDefaultTokenFile)
}

// readRelayTokenFile returns the token in a relay token file. A missing file is
// reported as os.ErrNotExist. A file with a bad token is an error, and it is
// never silently replaced: the operator may have put that value there on purpose.
func readRelayTokenFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if err := validateRelayToken(token); err != nil {
		return "", fmt.Errorf("%s: %w (delete the file to generate a new token)", path, err)
	}
	return token, nil
}

// ensureRelayTokenFile returns the token in path, generating and storing one
// (mode 0600) when the file does not exist yet. created says which happened.
func ensureRelayTokenFile(path string) (token string, created bool, err error) {
	existing, err := readRelayTokenFile(path)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(raw)
	if err := writeSecretFile(path, token+"\n"); err != nil {
		return "", false, fmt.Errorf("could not write the generated relay token to %s: %w", path, err)
	}
	return token, true, nil
}

// resolveRelayToken returns the token the relay host should use. An explicitly
// configured token wins. Otherwise the token in tokenFile is used, and one is
// generated there when the file does not exist, so a fresh install is never
// open with a known password. The second result is the file in use, if any.
func resolveRelayToken(configured, tokenFile string) (token, path string, created bool, err error) {
	if configured != "" {
		if err := validateRelayToken(configured); err != nil {
			return "", "", false, err
		}
		return configured, "", false, nil
	}
	token, created, err = ensureRelayTokenFile(tokenFile)
	if err != nil {
		return "", "", false, err
	}
	return token, tokenFile, created, nil
}

func (h *relayHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser clients are not accepted by the relay", http.StatusForbidden)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written the HTTP error
	}
	conn.SetReadLimit(relayMaxFrameBytes)

	peer, err := h.register(conn, r.RemoteAddr)
	if err != nil {
		closeAfterAnswer(conn)
		return
	}
	h.serve(peer)
}

// register reads and checks the first frame.
func (h *relayHost) register(conn *websocket.Conn, remote string) (*relayPeer, error) {
	_ = conn.SetReadDeadline(time.Now().Add(relayRegisterTimeout))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	reject := func(message string) (*relayPeer, error) {
		h.logger.Printf("relay: refused %s: %s", remote, message)
		p := &relayPeer{conn: conn}
		_ = p.send(map[string]string{"error": message})
		return nil, errors.New(message)
	}

	var meta struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		AuthToken string `json:"auth_token"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return reject("registration must be JSON")
	}

	enhanced := false
	switch {
	case subtle.ConstantTimeCompare([]byte(meta.AuthToken), []byte(h.token)) == 1:
	case h.enhancedTok != "" && meta.Type == "neuro-os" &&
		subtle.ConstantTimeCompare([]byte(meta.AuthToken), []byte(h.enhancedTok)) == 1:
		enhanced = true
	default:
		return reject("invalid auth token")
	}

	if meta.Type != "integration" && meta.Type != "neuro-os" {
		return reject("unknown registration type")
	}
	if !relayNamePattern.MatchString(meta.Name) {
		return reject("name must be 1-64 letters, digits, spaces, dots, dashes or underscores")
	}

	peer := &relayPeer{kind: meta.Type, name: meta.Name, enhanced: enhanced, conn: conn, since: time.Now()}
	if err := h.add(peer); err != nil {
		return reject(err.Error())
	}
	h.logger.Printf("relay: %s %q registered from %s", peer.kind, peer.name, remote)
	return peer, nil
}

// add puts a peer in the registry and tells the watchers. A newer connection
// with the same name replaces the older one, which is told why it was closed.
func (h *relayHost) add(peer *relayPeer) error {
	h.mu.Lock()
	var replaced *relayPeer
	if peer.kind == "integration" {
		if old, exists := h.integrations[peer.name]; exists {
			replaced = old
		} else if len(h.integrations) >= relayMaxIntegrations {
			h.mu.Unlock()
			return errors.New("too many integrations are connected")
		}
		h.integrations[peer.name] = peer
	} else {
		if old, exists := h.watchers[peer.name]; exists {
			replaced = old
		} else if len(h.watchers) >= relayMaxWatchers {
			h.mu.Unlock()
			return errors.New("too many watchers are connected")
		}
		h.watchers[peer.name] = peer
	}
	h.mu.Unlock()

	if replaced != nil {
		_ = replaced.send(map[string]string{"error": "replaced by a newer connection with the same name"})
		_ = replaced.conn.Close()
	}

	if peer.kind == "integration" {
		h.notifyWatchers(map[string]interface{}{"event": "integration_connected", "name": peer.name})
	} else {
		privileges := "standard"
		if peer.enhanced {
			privileges = "enhanced"
		}
		h.notifyWatchers(map[string]interface{}{"event": "neuroos_connected", "name": peer.name, "privileges": privileges})
	}
	return nil
}

func (h *relayHost) remove(peer *relayPeer) {
	h.mu.Lock()
	if peer.kind == "integration" {
		if h.integrations[peer.name] == peer {
			delete(h.integrations, peer.name)
			delete(h.actions, peer.name)
		} else {
			peer = nil // a newer connection owns the name; nothing to announce
		}
	} else if h.watchers[peer.name] == peer {
		delete(h.watchers, peer.name)
	} else {
		peer = nil
	}
	h.mu.Unlock()

	if peer == nil {
		return
	}
	if peer.kind == "integration" {
		h.notifyWatchers(map[string]interface{}{"event": "integration_disconnected", "name": peer.name})
	} else {
		h.notifyWatchers(map[string]interface{}{"event": "neuroos_disconnected", "name": peer.name})
	}
}

// notifyWatchers sends an event to every watcher. A watcher that cannot be
// written to is dropped; its own read loop will clean it up as well.
func (h *relayHost) notifyWatchers(event map[string]interface{}) {
	h.mu.Lock()
	targets := make([]*relayPeer, 0, len(h.watchers))
	for _, w := range h.watchers {
		targets = append(targets, w)
	}
	h.mu.Unlock()
	for _, w := range targets {
		if err := w.send(event); err != nil {
			h.logger.Printf("relay: could not reach watcher %q: %v", w.name, err)
		}
	}
}

func (h *relayHost) integration(name string) *relayPeer {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.integrations[name]
}

func (h *relayHost) serve(peer *relayPeer) {
	defer func() {
		_ = peer.conn.Close()
		h.remove(peer)
	}()

	_ = peer.conn.SetReadDeadline(time.Now().Add(relayIdleTimeout))
	peer.conn.SetPongHandler(func(string) error {
		return peer.conn.SetReadDeadline(time.Now().Add(relayIdleTimeout))
	})

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(relayPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				peer.writeMu.Lock()
				_ = peer.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(relayWriteTimeout))
				peer.writeMu.Unlock()
			}
		}
	}()

	for {
		msgType, raw, err := peer.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = peer.conn.SetReadDeadline(time.Now().Add(relayIdleTimeout))

		if !peer.allowFrame(time.Now()) {
			_ = peer.send(map[string]string{"error": "too many messages; slow down"})
			continue
		}
		if msgType == websocket.BinaryMessage {
			_ = peer.send(map[string]string{"error": "binary frames are not accepted"})
			continue
		}
		if peer.kind == "integration" {
			h.handleIntegrationFrame(peer, raw)
		} else {
			h.handleWatcherFrame(peer, raw)
		}
	}
}

func (h *relayHost) handleIntegrationFrame(peer *relayPeer, raw []byte) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		payload = map[string]interface{}{"action": "raw_text", "raw": string(raw)}
	}

	h.notifyWatchers(map[string]interface{}{"event": "integration_message", "from": peer.name, "payload": payload})

	if payload["event"] != "register_actions" {
		return
	}
	names := []string{}
	if actions, ok := payload["actions"].(map[string]interface{}); ok {
		for name := range actions {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	h.mu.Lock()
	h.actions[peer.name] = names
	h.mu.Unlock()
	h.logger.Printf("relay: %q announced %d action(s)", peer.name, len(names))
	h.notifyWatchers(map[string]interface{}{"event": "integration_registered_actions", "from": peer.name, "actions": names})
}

func (h *relayHost) handleWatcherFrame(peer *relayPeer, raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		_ = peer.send(map[string]string{"error": "watcher messages must be JSON"})
		return
	}

	if direct, _ := msg["direct_to_neuro"].(bool); direct && msg["payload"] != nil {
		if !peer.enhanced {
			_ = peer.send(map[string]string{"error": "direct_to_neuro needs the enhanced Neuro-OS token"})
			return
		}
		if h.neuro == nil || !h.neuro.connected() {
			_ = peer.send(map[string]string{"error": "neuro backend not available"})
			return
		}
		data, err := json.Marshal(msg["payload"])
		if err != nil {
			_ = peer.send(map[string]string{"error": "payload is not JSON"})
			return
		}
		if err := h.neuro.send(data); err != nil {
			_ = peer.send(map[string]string{"error": "failed to forward to neuro backend"})
			return
		}
		h.logger.Printf("relay: %q sent a direct message to the Neuro backend", peer.name)
		_ = peer.send(map[string]string{"status": "forwarded_to_neuro"})
		return
	}

	target, _ := msg["target"].(string)
	cmd, cmdOK := msg["cmd"].(map[string]interface{})
	if target == "" || !cmdOK {
		_ = peer.send(map[string]string{"error": "invalid target/cmd"})
		return
	}
	dest := h.integration(target)
	if dest == nil {
		_ = peer.send(map[string]string{"error": "invalid target/cmd"})
		return
	}
	if err := dest.send(map[string]interface{}{"from_watcher": peer.name, "cmd": cmd}); err != nil {
		_ = peer.send(map[string]string{"error": "failed to deliver to integration"})
		return
	}
	_ = peer.send(map[string]string{"status": "sent"})
}

// runNeuroLink keeps the optional Neuro API connection up, with backoff, and
// relays what Neuro sends to the watchers as backend_message events.
func (h *relayHost) runNeuroLink(stop <-chan struct{}) {
	if h.neuro == nil {
		return
	}
	backoff := time.Second
	for {
		select {
		case <-stop:
			return
		default:
		}
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = 10 * time.Second
		conn, _, err := dialer.Dial(h.neuro.url, nil)
		if err != nil {
			h.logger.Printf("relay: Neuro backend %s not reachable yet: %v", h.neuro.url, err)
			select {
			case <-stop:
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		h.neuro.mu.Lock()
		h.neuro.conn = conn
		h.neuro.mu.Unlock()
		h.logger.Printf("relay: linked to the Neuro backend at %s", h.neuro.url)

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var data interface{}
			if json.Unmarshal(raw, &data) != nil {
				data = string(raw)
			}
			h.notifyWatchers(map[string]interface{}{"event": "backend_message", "data": data})
		}

		h.neuro.mu.Lock()
		h.neuro.conn = nil
		h.neuro.mu.Unlock()
		_ = conn.Close()
		h.logger.Printf("relay: lost the Neuro backend link; retrying")
	}
}

// snapshot is what the health endpoint reports.
func (h *relayHost) snapshot() map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	integrations := make([]string, 0, len(h.integrations))
	for name := range h.integrations {
		entry := name
		if acts := h.actions[name]; len(acts) > 0 {
			entry = fmt.Sprintf("%s (%d actions)", name, len(acts))
		}
		integrations = append(integrations, entry)
	}
	watchers := make([]string, 0, len(h.watchers))
	for name, w := range h.watchers {
		if w.enhanced {
			watchers = append(watchers, name+" (enhanced)")
		} else {
			watchers = append(watchers, name)
		}
	}
	sort.Strings(integrations)
	sort.Strings(watchers)

	link := "not_configured"
	if h.neuro != nil {
		link = "disconnected"
		if h.neuro.connected() {
			link = "connected"
		}
	}
	return map[string]interface{}{
		"ok":             true,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
		"integrations":   integrations,
		"watchers":       watchers,
		"neuro_link":     link,
	}
}

// healthHandler serves GET /health. It is meant for the dashboard and for
// supervisors; it reveals names and counts only.
func (h *relayHost) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.snapshot())
	})
	return mux
}

// runRelayCommand is the `neuro-integration relay` subcommand.
func runRelayCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", envOr("NEURO_RELAY_LISTEN", "127.0.0.1:8765"), "address for integrations and watchers (ws)")
	health := fs.String("health", envOr("NEURO_RELAY_HEALTH_LISTEN", "127.0.0.1:8766"), "address for GET /health (empty to disable)")
	tokenFile := fs.String("token-file", relayTokenFilePath(), "the relay token file (created if missing, mode 0600)")
	neuroURL := fs.String("neuro-url", envOr("NEURO_RELAY_NEURO_URL", ""), "optional Neuro API url for direct_to_neuro (enhanced watchers)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	logger := log.New(stderr, "[relay] ", log.LstdFlags)
	configuredToken := strings.TrimSpace(os.Getenv("NEURO_RELAY_AUTH_TOKEN"))
	token, tokenPath, tokenCreated, err := resolveRelayToken(configuredToken, *tokenFile)
	if err != nil {
		fmt.Fprintf(stderr, "relay: %v\n", err)
		return 2
	}
	if configuredToken != "" {
		// The server reads the file, so a different NEURO_RELAY_AUTH_TOKEN here
		// means the server will be refused. Say so now, where the operator is looking.
		if fileToken, readErr := readRelayTokenFile(*tokenFile); readErr == nil && fileToken != configuredToken {
			logger.Printf("WARNING: NEURO_RELAY_AUTH_TOKEN differs from %s; the server reads that file and will be refused. Unset NEURO_RELAY_AUTH_TOKEN or copy the file's value into it.", *tokenFile)
		}
	}
	enhanced := strings.TrimSpace(os.Getenv("NEURO_RELAY_NEURO_OS_TOKEN"))
	if enhanced != "" {
		if err := validateRelayToken(enhanced); err != nil {
			fmt.Fprintf(stderr, "relay: NEURO_RELAY_NEURO_OS_TOKEN: %v\n", err)
			return 2
		}
		if enhanced == token {
			fmt.Fprintln(stderr, "relay: NEURO_RELAY_NEURO_OS_TOKEN must differ from the intermediary token")
			return 2
		}
	}

	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		fmt.Fprintf(stderr, "relay: --listen must be host:port: %v\n", err)
		return 2
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		logger.Printf("WARNING: listening on %s; anyone who can reach this port and knows the token can drive the relay", host)
	}

	h := newRelayHost(token, enhanced, strings.TrimSpace(*neuroURL), logger)
	stop := make(chan struct{})
	go h.runNeuroLink(stop)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(stderr, "relay: cannot listen on %s: %v\n", *listen, err)
		return 1
	}
	mux := http.NewServeMux()
	mux.Handle("/", h)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(ln) }()
	logger.Printf("listening on ws://%s (integrations and watchers)", ln.Addr())

	var healthServer *http.Server
	if strings.TrimSpace(*health) != "" {
		hln, err := net.Listen("tcp", *health)
		if err != nil {
			fmt.Fprintf(stderr, "relay: cannot listen for health on %s: %v\n", *health, err)
			_ = server.Close()
			return 1
		}
		healthServer = &http.Server{Handler: h.healthHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = healthServer.Serve(hln) }()
		logger.Printf("health on http://%s/health", hln.Addr())
	}

	if configuredToken == "" && tokenPath != "" {
		if tokenCreated {
			logger.Printf("generated a new relay token in %s (mode 0600); the server reads the same file", tokenPath)
		} else {
			logger.Printf("no NEURO_RELAY_AUTH_TOKEN set: using the relay token in %s (mode 0600); the server reads the same file", tokenPath)
		}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	logger.Printf("shutting down")
	close(stop)
	_ = server.Close()
	if healthServer != nil {
		_ = healthServer.Close()
	}
	h.closeAll()
	return 0
}

// closeAll drops every connection on shutdown.
func (h *relayHost) closeAll() {
	h.mu.Lock()
	peers := make([]*relayPeer, 0, len(h.integrations)+len(h.watchers))
	for _, p := range h.integrations {
		peers = append(peers, p)
	}
	for _, p := range h.watchers {
		peers = append(peers, p)
	}
	h.mu.Unlock()
	for _, p := range peers {
		_ = p.send(map[string]string{"error": "the relay is shutting down"})
		_ = p.conn.Close()
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
