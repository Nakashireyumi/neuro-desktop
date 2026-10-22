package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Version is the bridge build version, reported to the executor during the
// hello handshake and to the dashboard.
const Version = "0.2.0-dev"

type IntegrationOptions struct {
	WSURL           string
	GameName        string
	Persona         string
	IPCPath         string
	PermissionsPath string
	ExecutorListen  string
	ExecutorToken   string
	AdminListen     string
	AdminToken      string
	Relay           RelayConfig
	WebSocketHeader map[string]string
}

func NewNDIntegration(opts IntegrationOptions) (*NDIntegration, error) {
	persona, err := normalizedPersona(opts.Persona)
	if err != nil {
		return nil, err
	}
	client, err := neuro.NewClient(neuro.ClientConfig{
		Game:         opts.GameName,
		WebsocketURL: opts.WSURL,
		Headers:      httpHeader(opts.WebSocketHeader),
	})
	if err != nil {
		return nil, err
	}

	policy, err := loadPermissionPolicy(opts.PermissionsPath)
	if err != nil {
		return nil, err
	}
	executionSession, err := newExecutionSession()
	if err != nil {
		return nil, err
	}
	if err := requireUnusedIPCPaths(opts.IPCPath); err != nil {
		return nil, err
	}

	integration := &NDIntegration{
		client:           client,
		relay:            newRelayState(opts.Relay),
		ipcFilePath:      opts.IPCPath,
		executionSession: executionSession,
		permissionsPath:  opts.PermissionsPath,
		persona:          persona,
		permissions:      policy,
		done:             make(chan struct{}),
		contextStopChan:  make(chan struct{}),
		games:            newGameRuntime(loadGameRegistry()),
		stats:            newBridgeStats(),
		stop:             newStopSwitch(),
		audit:            newAuditor(),
		requests:         newRequestBook(),
		mcp:              newMCPManager(),
		startedAt:        time.Now(),
	}

	integration.client.OnCommand("shutdown/graceful", integration.handleGracefulShutdown)
	integration.client.OnCommand("shutdown/immediate", integration.handleImmediateShutdown)

	// The relay link belongs to the integration from the start: reserved action
	// names (owned by other integrations) come from it, even when the relay
	// socket itself is disabled.
	integration.relay.setOwner(integration)
	// Invalid persisted settings keep capture off, while leaving the operator
	// dashboard available to repair them with fresh approval.
	if service := integration.visionServiceSnapshot(); service.Reason != "" {
		log.Printf("Vision Service: %s", service.Reason)
	}

	return integration, nil
}

func (n *NDIntegration) Start() error {
	retryDelaySeconds := 5
	if rawRetryDelay := strings.TrimSpace(os.Getenv("NEURO_WS_RETRY_SECONDS")); rawRetryDelay != "" {
		if parsedDelay, err := strconv.Atoi(rawRetryDelay); err == nil && parsedDelay > 0 {
			retryDelaySeconds = parsedDelay
		}
	}

	maxRetries := 0
	if rawMaxRetries := strings.TrimSpace(os.Getenv("NEURO_WS_MAX_RETRIES")); rawMaxRetries != "" {
		if parsedMax, err := strconv.Atoi(rawMaxRetries); err == nil && parsedMax >= 0 {
			maxRetries = parsedMax
		}
	}

	attempt := 0
	for {
		n.neuroConnection.connecting()
		if err := n.client.Connect(); err == nil {
			n.neuroConnection.connected()
			break
		} else {
			n.neuroConnection.disconnected()
			attempt++
			log.Printf("Connect attempt %d failed: %v", attempt, err)

			if maxRetries > 0 && attempt >= maxRetries {
				return fmt.Errorf("failed to connect after %d attempts", attempt)
			}

			log.Printf("Retrying connection in %d second(s)...", retryDelaySeconds)
			time.Sleep(time.Duration(retryDelaySeconds) * time.Second)
		}
	}

	go func() {
		for err := range n.client.Errors() {
			n.neuroConnection.disconnected()
			log.Printf("SDK error: %v", err)
			n.stats.noteReconnect()
			n.markDone()
		}
	}()

	if err := n.registerActions(); err != nil {
		return err
	}
	// Register before describing the surface. Detailed help is requested through
	// desktop_guide; a working-directory manual is never injected automatically.
	n.sendStartupGuide()
	n.startContextLoop()
	// Enabled MCP servers start in the background; their tools are added to
	// Neuro as each one comes up.
	go n.syncMCPServers()

	return nil
}

func (n *NDIntegration) Close() error {
	n.cancelVisionServiceRequests()
	n.stopGrantExpiryTimers()
	n.neuroConnection.disconnected()
	n.stopAllMCPServers()

	if err := n.client.SendContext(
		"Neurcro integration is shutting down. WebSocket will close.",
		true,
	); err != nil {
		log.Printf("Warning: failed to send shutdown context: %v", err)
	}

	if err := n.unregisterActions(); err != nil {
		log.Printf("Warning: failed to unregister actions: %v", err)
	}

	n.markDone()
	return n.client.Close()
}

func main() {
	// `neuro-integration catalog ...` is an operator tool (keygen, sign, verify),
	// not the bridge. It never starts a connection.
	// `neuro-integration relay ...` runs the Neuro Relay intermediary (see relay_host.go).
	if len(os.Args) > 1 && os.Args[1] == "relay" {
		os.Exit(runRelayCommand(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "catalog" {
		os.Exit(runCatalogCommand(os.Args[2:], os.Stdout, os.Stderr))
	}
	// `neuro-integration setup` is the first-run step: relay token, dashboard
	// token, and a check that the two sides agree (see setup.go).
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		os.Exit(runSetupCommand(os.Args[2:], os.Stdout, os.Stderr))
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	wsURLFlag := flag.String("ws-url", "", "Neuro API websocket URL")
	personaFlag := flag.String("persona", "", "Persona for this connection: neuro, evil or shared (NEURO_PERSONA; default shared)")
	ipcPathFlag := flag.String("ipc-file", "", "Legacy file-IPC path (fallback when no TCP executor)")
	permissionsPathFlag := flag.String("permissions-file", "", "Permissions policy file path")
	executorListenFlag := flag.String("executor-listen", "", "TCP listen addr for executor clients (default 127.0.0.1:9876)")
	executorTokenFlag := flag.String("executor-token", "", "Shared secret an executor must present (NEURO_EXECUTOR_TOKEN)")
	adminListenFlag := flag.String("admin-listen", "", "HTTP dashboard addr (default 127.0.0.1:8300)")
	adminTokenFlag := flag.String("admin-token", "", "Token required for destructive dashboard calls (NEURO_ADMIN_TOKEN)")
	relayURLFlag := flag.String("relay-url", "", "Neuro Relay intermediary WebSocket URL (enables relay participation)")
	relayNameFlag := flag.String("relay-name", "", "Name Neurcro registers with the relay as")
	gameActionsFlag := flag.Bool("game-actions", RegisterGameActionsOnStartup, "Register the high-level game interface actions")
	flag.Parse()

	RegisterGameActionsOnStartup = *gameActionsFlag

	wsURL := firstNonEmpty(*wsURLFlag, os.Getenv("NEURO_SDK_WS_URL"), "ws://localhost:8000")
	persona := firstNonEmpty(*personaFlag, os.Getenv("NEURO_PERSONA"), "shared")

	ipcPath := firstNonEmpty(*ipcPathFlag, os.Getenv("NEURO_IPC_FILE"), "./neuro-integration-code-ipc.json")
	permissionsPath := firstNonEmpty(*permissionsPathFlag, os.Getenv("NEURO_PERMISSIONS_FILE"), "./permissions.json")
	executorListen := firstNonEmpty(*executorListenFlag, os.Getenv("NEURO_EXECUTOR_LISTEN"), "127.0.0.1:9876")
	executorToken := firstNonEmpty(*executorTokenFlag, os.Getenv("NEURO_EXECUTOR_TOKEN"))
	adminListen := firstNonEmpty(*adminListenFlag, os.Getenv("NEURO_ADMIN_LISTEN"), "127.0.0.1:8300")
	adminToken := firstNonEmpty(*adminTokenFlag, os.Getenv("NEURO_ADMIN_TOKEN"))

	relayConfig := relayConfigFromEnv()
	if *relayURLFlag != "" {
		relayConfig.IntermediaryURL = *relayURLFlag
		relayConfig.Enabled = true
	}
	if *relayNameFlag != "" {
		relayConfig.Name = *relayNameFlag
	}
	if relayConfig.Enabled {
		// The relay token comes from the same file the relay host reads. A
		// missing or disagreeing token disables the link and says why; the rest
		// of the server keeps running.
		token, tokenErr := resolveRelayClientToken(relayConfig.Token, relayTokenFilePath())
		switch {
		case tokenErr != nil:
			relayConfig.TokenError = tokenErr.Error()
		case token == "":
			relayConfig.TokenError = "no relay token: run `neuro-integration setup` (or set NEURO_RELAY_TOKEN)"
		}
		relayConfig.Token = token
	}

	// The dashboard credential. Without one, the API refuses everything until
	// `neuro-integration setup` has run; a mismatch is reported and also refused.
	dashboardCred, credErr := loadDashboardCredential(adminToken, dashboardTokenFilePath())
	if credErr != nil {
		log.Printf("WARNING: dashboard token problem, the API refuses every request until it is fixed: %v", credErr)
		dashboardCred = dashboardCredential{}
	}

	log.Printf("Starting Neurcro bridge (server) v%s", Version)
	log.Printf("- Neuro WebSocket: %s", wsURL)
	log.Printf("- Executor listen: %s", executorListen)
	if executorToken != "" {
		log.Printf("- Executor auth:   shared token required")
	}
	dashboardTokenState := dashboardCred.label()
	if credErr != nil {
		dashboardTokenState = "UNUSABLE, see the warning above"
	}
	log.Printf("- Admin dashboard: http://%s/ui/ (token %s)", adminListen, dashboardTokenState)
	log.Printf("- File IPC fallback: %s", ipcPath)
	log.Printf("- Permissions file:  %s", permissionsPath)
	log.Printf("- Game interface:    %s", enabledLabel(RegisterGameActionsOnStartup))
	if relayConfig.Enabled {
		log.Printf("- Neuro Relay:       %s (as %q)", relayConfig.IntermediaryURL, relayConfig.Name)
		if relayConfig.TokenError != "" {
			log.Printf("  relay link disabled: %s", relayConfig.TokenError)
		}
	}

	integration, err := NewNDIntegration(IntegrationOptions{
		WSURL:           wsURL,
		GameName:        "Neurcro",
		Persona:         persona,
		IPCPath:         ipcPath,
		PermissionsPath: permissionsPath,
		ExecutorListen:  executorListen,
		ExecutorToken:   executorToken,
		AdminListen:     adminListen,
		AdminToken:      adminToken,
		Relay:           relayConfig,
		WebSocketHeader: webSocketHeadersFromEnv(),
	})
	if err != nil {
		log.Fatalf("Failed to create integration: %v", err)
	}

	hub := NewExecutorHub(executorListen, executorToken)
	if err := hub.Start(); err != nil {
		log.Fatalf("Failed to start executor hub: %v", err)
	}
	integration.executorHub = hub
	defer hub.Close()

	relay := integration.relay
	defer relay.Stop()

	admin := NewAdminServer(integration, adminListen, dashboardCred)
	if credErr != nil {
		admin.credentialProblem = credErr.Error()
	}
	if err := admin.Start(); err != nil {
		log.Printf("Warning: admin dashboard did not start: %v", err)
	}

	if err := relay.Start(); err != nil {
		log.Printf("Warning: relay link did not start: %v", err)
	}

	if err := integration.Start(); err != nil {
		log.Fatalf("Failed to start integration: %v", err)
	}
	defer integration.Close()

	log.Println("Bridge running — wait for an executor client, or use co-located file IPC")
	log.Println("Press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Interrupt signal received")
	case <-integration.done:
		log.Println("Shutdown requested by command channel")
	}
}

// httpHeader adapts the env-provided map for the websocket handshake.
func httpHeader(headers map[string]string) http.Header {
	if len(headers) == 0 {
		return nil
	}
	out := http.Header{}
	for name, value := range headers {
		out.Set(name, value)
	}
	return out
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// webSocketHeadersFromEnv lets Neurcro authenticate against a relay or a
// backend that wants a bearer token: NEURO_WS_TOKEN or NEURO_WS_HEADERS
// ("X-Api-Key=abc;X-Other=def").
func webSocketHeadersFromEnv() map[string]string {
	headers := map[string]string{}

	if token := strings.TrimSpace(os.Getenv("NEURO_WS_TOKEN")); token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	if raw := strings.TrimSpace(os.Getenv("NEURO_WS_HEADERS")); raw != "" {
		for _, pair := range strings.Split(raw, ";") {
			name, value, found := strings.Cut(pair, "=")
			name = strings.TrimSpace(name)
			if !found || name == "" {
				continue
			}
			headers[name] = strings.TrimSpace(value)
		}
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}
