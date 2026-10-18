package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Neuro Relay support.
//
// Neuro Relay (https://github.com/Nakashireyumi/neuro-relay) multiplexes
// several integrations behind one Neuro connection. Neuro Desktop takes part in
// two ways:
//
//  1. As a Neuro API client pointed at the relay instead of the raw backend
//     (NEURO_SDK_WS_URL / --ws-url). In that mode the relay owns action
//     arbitration and namespaces each integration's actions.
//  2. As a relay *integration*: Neuro Desktop registers on the relay's
//     intermediary socket so Neuro-OS watchers can see it and drive it. That
//     registration is what this file implements.
//
// Neuro Desktop does not start, supervise, or spawn a relay process. The relay
// runs separately: the Go host in this binary (`neuro-integration relay`, see
// relay_host.go) or the upstream Python relay. Both speak the same intermediary
// socket, which is all this file uses. (An earlier version could start a relay
// process from an environment variable; that was removed with the Go host.)
type RelayConfig struct {
	Enabled         bool
	IntermediaryURL string
	Token           string
	// TokenError says why the relay cannot be used (no token, or the two token
	// sources disagree). When set, the link does not connect; the dashboard shows it.
	TokenError string
	Name       string
	// ReservedActions are action names owned by another integration. They are
	// left unregistered so Neuro Desktop cannot shadow a game integration.
	ReservedActions []string
}

func relayConfigFromEnv() RelayConfig {
	cfg := RelayConfig{
		Enabled:         getEnvBool("NEURO_RELAY_ENABLED", false),
		IntermediaryURL: strings.TrimSpace(os.Getenv("NEURO_RELAY_URL")),
		Token:           strings.TrimSpace(os.Getenv("NEURO_RELAY_TOKEN")),
		Name:            nonEmptyOr(strings.TrimSpace(os.Getenv("NEURO_RELAY_NAME")), "Neuro Desktop"),
	}

	if cfg.IntermediaryURL == "" {
		addr := strings.TrimSpace(os.Getenv("NEURO_RELAY_EMULATED_ADDR"))
		if addr == "" {
			addr = "127.0.0.1:8765"
		}
		cfg.IntermediaryURL = "ws://" + strings.TrimPrefix(addr, "ws://")
	}

	cfg.ReservedActions = append(cfg.ReservedActions, reservedActionsFromEnv()...)

	return cfg
}

// reservedActionsFromEnv reads the action names that belong to another
// integration (NEURO_RESERVED_ACTIONS, comma separated). Keeping this readable
// outside the relay config means reserved names work even when the relay link
// itself is switched off.
func reservedActionsFromEnv() []string {
	var out []string
	for _, name := range strings.Split(os.Getenv("NEURO_RESERVED_ACTIONS"), ",") {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// resolveRelayClientToken returns the token this server presents to the relay.
// The relay token file is the source of truth: the relay host reads the same
// file. NEURO_RELAY_TOKEN, when set, must equal it, because two different values
// would leave the server and the relay host disagreeing. With neither present
// there is no token, and the caller must not connect.
func resolveRelayClientToken(envToken, tokenFile string) (string, error) {
	envToken = strings.TrimSpace(envToken)

	fileToken, err := readRelayTokenFile(tokenFile)
	haveFile := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if envToken != "" {
		if err := validateRelayToken(envToken); err != nil {
			return "", fmt.Errorf("NEURO_RELAY_TOKEN: %w", err)
		}
		if haveFile && subtle.ConstantTimeCompare([]byte(envToken), []byte(fileToken)) != 1 {
			return "", fmt.Errorf("NEURO_RELAY_TOKEN differs from the relay token file %s, so the relay would refuse this server; unset NEURO_RELAY_TOKEN (the server reads the file), or copy the file's value into it", tokenFile)
		}
		return envToken, nil
	}
	if haveFile {
		return fileToken, nil
	}
	return "", nil
}

func (c RelayConfig) endpoint() string {
	return strings.TrimSuffix(c.IntermediaryURL, "/")
}

// RelayState is the observable status of the relay link.
// errRelayUnreachable marks a failed dial: the relay is not listening (yet).
var errRelayUnreachable = errors.New("relay dial failed")

type RelayState struct {
	mu         sync.Mutex
	cfg        RelayConfig
	connected  bool
	registered bool
	lastError  string
	lastEvent  string
	lastSeen   time.Time
	// Peers are integration names the relay told us about, and the action names
	// each peer registered (namespaced "<peer>.<action>" as the relay does).
	peers       map[string]string
	peerActions map[string][]string
	stop        chan struct{}
	done        chan struct{}
	// owner is the integration this relay link belongs to (set during wiring).
	owner *NDIntegration
}

func newRelayState(cfg RelayConfig) *RelayState {
	return &RelayState{
		cfg:         cfg,
		peers:       map[string]string{},
		peerActions: map[string][]string{},
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// RelayStatus is the JSON shape used by the dashboard.
type RelayStatus struct {
	Enabled     bool                `json:"enabled"`
	URL         string              `json:"url,omitempty"`
	Connected   bool                `json:"connected"`
	Registered  bool                `json:"registered"`
	PeerCount   int                 `json:"peer_count"`
	Peers       map[string]string   `json:"peers,omitempty"`
	PeerActions map[string][]string `json:"peer_actions,omitempty"`
	LastError   string              `json:"last_error,omitempty"`
	LastEvent   string              `json:"last_event,omitempty"`
	LastSeen    string              `json:"last_seen,omitempty"`
	Reserved    []string            `json:"reserved_actions,omitempty"`
}

func (r *RelayState) status() RelayStatus {
	if r == nil {
		return RelayStatus{Enabled: false}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	out := RelayStatus{
		Enabled:    r.cfg.Enabled,
		URL:        r.cfg.IntermediaryURL,
		Connected:  r.connected,
		Registered: r.registered,
		PeerCount:  len(r.peers),
		LastError:  r.lastError,
		LastEvent:  r.lastEvent,
		Reserved:   append([]string{}, r.cfg.ReservedActions...),
	}
	if len(r.peers) > 0 {
		out.Peers = map[string]string{}
		for name, kind := range r.peers {
			out.Peers[name] = kind
		}
	}
	if len(r.peerActions) > 0 {
		out.PeerActions = map[string][]string{}
		for name, actions := range r.peerActions {
			out.PeerActions[name] = append([]string{}, actions...)
		}
	}
	if !r.lastSeen.IsZero() {
		out.LastSeen = r.lastSeen.UTC().Format(time.RFC3339)
	}
	return out
}

func (r *RelayState) reservedSet() map[string]bool {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cfg.ReservedActions) == 0 {
		return nil
	}
	out := make(map[string]bool, len(r.cfg.ReservedActions))
	for _, name := range r.cfg.ReservedActions {
		out[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return out
}

func (r *RelayState) noteEvent(event string, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSeen = time.Now()
	if event != "" {
		r.lastEvent = event
	}
	switch {
	case err != nil:
		r.lastError = err.Error()
	case event == "registered":
		// A fresh registration means the link is healthy again; keeping the
		// old failure around made /api/relay read "connected, registered,
		// last_error: <stale>" which sends operators chasing ghosts.
		r.lastError = ""
	}
}

func (r *RelayState) notePeer(name, kind string) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[name] = kind
	r.lastSeen = time.Now()
}

func (r *RelayState) dropPeer(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, name)
	delete(r.peerActions, name)
}

// notePeerActions records the action names an integration told the relay about.
// The relay forwards them to watchers; knowing them lets this bridge tell Neuro
// which peer actions to call instead of trying to move the game itself.
func (r *RelayState) notePeerActions(name string, actions []string) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peerActions[name] = append([]string{}, actions...)
	r.lastSeen = time.Now()
}

// peerActionHint renders "integration owns actions a, b, c" for a message to
// Neuro, limited so the text stays short.
func (r *RelayState) peerActionHint(peer string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	own := r.peerActions[peer]
	if len(own) == 0 {
		// fall back to any registered peer actions, since integrations often
		// register before the watcher sees the connection event
		for _, actions := range r.peerActions {
			if len(actions) > 0 {
				own = actions
				break
			}
		}
	}
	if len(own) == 0 {
		return ""
	}

	const limit = 8
	shown := own
	suffix := ""
	if len(shown) > limit {
		shown = shown[:limit]
		suffix = fmt.Sprintf(" (and %d more)", len(own)-limit)
	}
	return "Its registered actions are: " + strings.Join(shown, ", ") + suffix + "."
}

// isSelf reports whether a relay peer name is this bridge's own registration,
// which the relay echoes back to every watcher.
func (r *RelayState) isSelf(name string) bool {
	if r == nil || name == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(r.cfg.Name))
}

// Start begins the client loop that keeps the intermediary link up. It is
// tolerant: a relay that is not running must not stop Neuro Desktop from working
// as a direct Neuro integration.
func (r *RelayState) Start() error {
	if r == nil || !r.cfg.Enabled {
		return nil
	}
	if r.cfg.TokenError != "" {
		r.mu.Lock()
		r.lastError = r.cfg.TokenError
		r.mu.Unlock()
		return nil
	}
	go r.clientLoop()
	return nil
}

// Stop ends the client loop. It does not touch any relay process, because
// Neuro Desktop does not run one.
func (r *RelayState) Stop() {
	if r == nil {
		return
	}
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

func (r *RelayState) clientLoop() {
	backoff := time.Second

	for {
		select {
		case <-r.stop:
			close(r.done)
			return
		default:
		}

		err := r.runSession()
		r.setConnectionState(false, false)
		if err != nil && !r.isStopping() && !isExpectedClose(err) {
			if errors.Is(err, errRelayUnreachable) {
				r.noteEvent("unreachable", err)
			} else {
				r.noteEvent("disconnected", err)
			}
		}
		if !r.sleep(backoff) {
			close(r.done)
			return
		}
		backoff = nextBackoff(backoff, 30*time.Second)
	}
}

func (r *RelayState) sleep(d time.Duration) bool {
	select {
	case <-r.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// isTimeout reports whether the error is a read deadline.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// isExpectedClose reports closes that are ours (shutdown/reconnect) or the
// relay's normal goodbye. They are not failures and must not be reported as
// "last error" to the dashboard.
func isExpectedClose(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, websocket.ErrCloseSent) {
		// We closed the socket (shutdown or reconnect); not the relay's fault.
		return true
	}
	if websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
		websocket.CloseAbnormalClosure,
	) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// isStopping reports whether Stop() has been requested.
func (r *RelayState) isStopping() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func (r *RelayState) setConnectionState(connected bool, registered bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connected = connected
	r.registered = registered
	if !connected {
		r.peers = map[string]string{}
		r.peerActions = map[string][]string{}
	}
}

// runSession registers with the relay intermediary and serves routed commands
// until the socket drops.
func (r *RelayState) runSession() error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(r.cfg.endpoint(), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", errRelayUnreachable, err)
	}
	defer func() { _ = conn.Close() }()

	registration := map[string]interface{}{
		"type":       "integration",
		"name":       r.cfg.Name,
		"auth_token": r.cfg.Token,
		"role":       "neuro-desktop",
		"platform":   platformKey(),
	}
	if err := conn.WriteJSON(registration); err != nil {
		return fmt.Errorf("relay registration failed: %w", err)
	}
	r.setConnectionState(true, true)
	r.noteEvent("registered", nil)
	log.Printf("Registered with Neuro Relay as %q at %s", r.cfg.Name, r.cfg.endpoint())

	// Announce which actions Neuro Desktop provides.
	//
	// The relay's intermediary keeps `actions` as a name -> schema mapping
	// (`for act_name, schema in actions.items()` in intermediary.py) and
	// namespaces it as "<integration>.<action>", so send exactly that shape.
	// Note: the relay only forwards registrations made by Neuro-SDK clients on
	// its backend socket to the real Neuro; this registration is what watchers
	// (Neuro-OS) see, and it also stops a peer from reusing our names.
	// Writes after the registration are not fatal on their own: when the relay
	// rejects the registration it answers with an error frame and closes, so a
	// broken write usually means "read the error frame first". The failure is
	// kept and used only if the read loop has nothing better to report.
	var writeErr error

	if actions := r.actionSchemas(); len(actions) > 0 {
		if err := conn.WriteJSON(map[string]interface{}{
			"event":   "register_actions",
			"actions": actions,
			"count":   len(actions),
		}); err != nil {
			writeErr = err
		}
	}

	if writeErr == nil {
		writeErr = conn.WriteJSON(map[string]interface{}{
			"event": "status",
			"payload": map[string]interface{}{
				"executor_connected": r.hasExecutor(),
				"game_profiles":      r.gameProfileCount(),
				"platform":           platformKey(),
			},
		})
	}

	// Do NOT validate the registration with a bounded read. gorilla stores the
	// first read error on the Conn and returns it from every later read, so a
	// probe that times out on an idle relay poisons the socket: the link then
	// reconnects forever. A rejection is reported by the read loop instead,
	// which is also where the relay's {"error": ...} frame is parsed.
	//
	// The relay is a mostly-silent peer: an idle integration socket only ever
	// carries control frames. A gorilla read deadline cannot be used as an
	// idle timeout, because the deadline is absolute per read and expires even
	// while pings keep arriving — that is what used to reconnect the link every
	// 90 seconds. Instead the bridge sends its own websocket pings and drops the
	// connection only when the peer stops answering them.
	var lastPong atomic.Int64
	lastPong.Store(time.Now().UnixNano())
	conn.SetPongHandler(func(string) error {
		lastPong.Store(time.Now().UnixNano())
		return nil
	})

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-r.stop:
				return
			case <-ticker.C:
				silentFor := time.Since(time.Unix(0, lastPong.Load()))
				if silentFor > 90*time.Second {
					// No pong for three intervals: treat the link as dead so the
					// client loop reconnects and re-registers.
					_ = conn.WriteControl(websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.CloseAbnormalClosure, "relay heartbeat missed"),
						time.Now().Add(2*time.Second))
					_ = conn.Close()
					return
				}
				_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			}
		}
	}()

	var relayRejection string

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if relayRejection != "" {
				return fmt.Errorf(
					"relay rejected the registration: %s — check NEURO_RELAY_TOKEN against "+
						"intermediary.auth_token in the relay's authentication.yaml", relayRejection)
			}
			if writeErr != nil {
				return fmt.Errorf(
					"relay connection failed right after the registration (%v); the relay may have "+
						"rejected it — check NEURO_RELAY_TOKEN against intermediary.auth_token in the "+
						"relay's authentication.yaml", writeErr)
			}
			return err
		}
		writeErr = nil

		var envelope map[string]interface{}
		if err := json.Unmarshal(message, &envelope); err != nil {
			continue
		}

		// The relay answers a bad registration with {"error": ...} and closes.
		// Report that instead of an unexplained disconnect.
		if raw, ok := envelope["error"]; ok {
			return fmt.Errorf("relay rejected the registration: %v "+
				"(check NEURO_RELAY_TOKEN against intermediary.auth_token in the relay's authentication.yaml)", raw)
		}

		if relayError := r.handleRelayMessage(envelope); relayError != "" {
			relayRejection = relayError
		}
	}
}

// handleRelayMessage applies one frame from the relay. It returns the message of
// an {"error": ...} frame (empty when there was none) so the read loop can
// report the relay's own explanation instead of a bare socket error.
func (r *RelayState) handleRelayMessage(envelope map[string]interface{}) string {
	if err, ok := envelope["error"].(string); ok && err != "" {
		r.noteEvent("error", fmt.Errorf("%s", err))
		log.Printf("Relay reported an error: %s", err)
		return err
	}

	if event, ok := envelope["event"].(string); ok {
		r.noteEvent(event, nil)
		switch event {
		case "integration_connected":
			name, _ := envelope["name"].(string)
			// The relay broadcasts every connection, including our own. The
			// relay's own answer to our registration carries our name, so
			// without this filter the bridge reported itself as a coexisting
			// integration in /api/relay and in the "external integration owns
			// this game" hints.
			if r.isSelf(name) {
				break
			}
			r.notePeer(name, "integration")
			log.Printf("Relay: integration %q connected", name)
		case "integration_disconnected":
			name, _ := envelope["name"].(string)
			if r.isSelf(name) {
				break
			}
			r.dropPeer(name)
			log.Printf("Relay: integration %q disconnected", name)
		case "neuroos_connected":
			name, _ := envelope["name"].(string)
			r.notePeer(name, "neuro-os")
			log.Printf("Relay: Neuro-OS watcher %q connected", name)
		case "neuroos_disconnected":
			name, _ := envelope["name"].(string)
			r.dropPeer(name)
		case "integration_message":
			from, _ := envelope["from"].(string)
			r.notePeer(from, "integration")
			// An integration's own register_actions message reaches watchers as
			// an integration_message; keep the action names.
			if payload, ok := envelope["payload"].(map[string]interface{}); ok {
				if payload["event"] == "register_actions" {
					r.notePeerActions(from, actionNamesFrom(payload["actions"]))
				}
			}
		case "integration_registered_actions":
			from, _ := envelope["from"].(string)
			r.notePeer(from, "integration")
			r.notePeerActions(from, actionNamesFrom(envelope["actions"]))
		}
		// Events still may carry a command (see below).
	}

	// A watcher (Neuro-OS / operator UI) can drive Neuro Desktop through the
	// relay. Commands are subject to the same permission policy as Neuro.
	if cmd, ok := envelope["cmd"].(map[string]interface{}); ok {
		from, _ := envelope["from_watcher"].(string)
		r.notePeer(nonEmptyOr(from, "watcher"), "neuro-os")
		go r.executeRelayCommand(from, cmd)
	}
	return ""
}

// executeRelayCommand runs a watcher-issued command, e.g.
// {"target": "neuro-desktop", "cmd": {"action": "type_text", "params": {...}}}
func (r *RelayState) executeRelayCommand(from string, cmd map[string]interface{}) {
	name, _ := cmd["action"].(string)
	if name == "" {
		name, _ = cmd["name"].(string)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		log.Printf("Relay: ignoring command from %q without an action name", from)
		return
	}

	integration := r.integration()
	if integration == nil {
		return
	}

	// Same gates as Neuro's actions: the operator brake, the policy, then the
	// per-scope rate limit. See denyRelayCommand.
	if reason := integration.denyRelayCommand(name, from); reason != "" {
		log.Printf("Relay: refused %q from %q (%s)", name, from, reason)
		return
	}

	params := map[string]interface{}{}
	if raw, ok := cmd["params"].(map[string]interface{}); ok {
		params = raw
	}

	command, err := buildIPCCommand(CommandType(name), params, true, true)
	if err != nil {
		log.Printf("Relay: command %q from %q is not supported: %v", name, from, err)
		return
	}

	if _, err := integration.sendToExecutorForAction(command, name); err != nil {
		log.Printf("Relay: command %q from %q failed: %v", name, from, err)
		return
	}
	log.Printf("Relay: executed %q for %q", name, from)
}

// integration back-references the owning integration (set during wiring).
func (r *RelayState) integration() *NDIntegration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.owner
}

func (r *RelayState) setOwner(integration *NDIntegration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owner = integration
}

func (r *RelayState) hasExecutor() bool {
	integration := r.integration()
	if integration == nil || integration.executorHub == nil {
		return false
	}
	return integration.executorHub.HasClient()
}

func (r *RelayState) gameProfileCount() int {
	integration := r.integration()
	if integration == nil || integration.games == nil {
		return 0
	}
	return len(integration.games.Profiles())
}

// actionSummary lists the Neuro-facing actions this build provides.
func (r *RelayState) actionSchemas() map[string]interface{} {
	integration := r.integration()
	if integration == nil {
		return nil
	}

	out := map[string]interface{}{}
	specs := append(append(append([]actionSpec{}, HLActionSpecs...), LLActionSpecs...), gameActionSpecs()...)
	specs = append(specs, alwaysRegisteredSpecs()...)
	for _, spec := range specs {
		if r.reservedSet()[strings.ToLower(string(spec.Name))] {
			continue
		}
		entry := map[string]interface{}{
			"name":        string(spec.Name),
			"description": spec.Description,
			"kind":        string(spec.Kind),
		}
		if spec.Schema != nil {
			entry["schema"] = spec.Schema
		}
		out[string(spec.Name)] = entry
	}
	return out
}

// ReservedActionNames returns the sorted list of actions left to other
// integrations (used by the dashboard and by registration filtering). It merges
// the environment list with the relay configuration.
func (r *RelayState) ReservedActionNames() []string {
	reserved := map[string]bool{}
	for _, name := range reservedActionsFromEnv() {
		reserved[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for name := range r.reservedSet() {
		reserved[name] = true
	}
	if len(reserved) == 0 {
		return nil
	}

	out := make([]string, 0, len(reserved))
	for name := range reserved {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// actionNamesFrom accepts either a list of names, a list of action objects, or
// the name -> schema mapping this bridge sends, and returns the names.
func actionNamesFrom(raw interface{}) []string {
	var names []string

	switch value := raw.(type) {
	case []interface{}:
		for _, item := range value {
			switch entry := item.(type) {
			case string:
				names = append(names, entry)
			case map[string]interface{}:
				if name, ok := entry["name"].(string); ok && name != "" {
					names = append(names, name)
				}
			}
		}
	case map[string]interface{}:
		for name := range value {
			names = append(names, name)
		}
	}

	sort.Strings(names)
	return names
}

// denyRelayCommand applies the gates a watcher command must pass before it can
// run, in order: the operator brake (pause or kill), the permission policy, and
// the per-scope rate limit shared with Neuro. It returns the refusal reason, or
// "" when the command may run. A refusal is counted and written to the audit log.
func (n *NDIntegration) denyRelayCommand(name, from string) string {
	refuse := func(reason, detail string) string {
		n.stats.noteDenied(name)
		n.audit.record("relay_command", map[string]interface{}{
			"action": name, "from": from, "decision": "refused", "reason": reason,
		})
		return detail
	}

	if reason := n.stop.blockReason(name); reason != "" {
		return refuse("stopped", reason)
	}

	policy := n.policy()
	if policy == nil {
		return ""
	}
	if !policy.IsAllowed(name) {
		return refuse("policy", "denied by policy")
	}
	scope, _ := scopeForAction(name)
	if limit := policy.ScopeRateLimit(scope); limit > 0 {
		if allowed, _ := n.rate.allow(scope, limit, time.Now()); !allowed {
			return refuse("rate_limit", "rate limit for "+string(scope))
		}
	}
	return ""
}
