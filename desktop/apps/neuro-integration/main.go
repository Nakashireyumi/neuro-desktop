package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

func NewNDIntegration(
	wsURL string,
	gameName string,
	ipcPath string,
	permissionsPath string,
) (*NDIntegration, error) {
	client, err := neuro.NewClient(neuro.ClientConfig{
		Game:         gameName,
		WebsocketURL: wsURL,
	})
	if err != nil {
		return nil, err
	}

	policy, err := loadPermissionPolicy(permissionsPath)
	if err != nil {
		return nil, err
	}

	integration := &NDIntegration{
		client:          client,
		ipcFilePath:     ipcPath,
		permissionsPath: permissionsPath,
		permissions:     policy,
		done:            make(chan struct{}),
		contextStopChan: make(chan struct{}),
	}

	integration.client.OnCommand("shutdown/graceful", integration.handleGracefulShutdown)
	integration.client.OnCommand("shutdown/immediate", integration.handleImmediateShutdown)

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
		if err := n.client.Connect(); err == nil {
			break
		} else {
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
			log.Printf("SDK error: %v", err)
			n.markDone()
		}
	}()

	if err := n.client.SendContext(
		"## Neuro Desktop ready\n\nYou can control the mouse, keyboard, and run scripts on the connected executor.",
		true,
	); err != nil {
		log.Printf("Warning: failed to send initial context: %v", err)
	}

	content, contentPath, err := loadActionScriptDocumentation()
	if err != nil {
		log.Printf("Warning: failed to load action script documentation: %v", err)
	} else {
		log.Printf("Loaded action script documentation from %s", contentPath)
		if err := n.client.SendContext(content, true); err != nil {
			log.Printf("Warning: failed to send action script documentation: %v", err)
		}
	}

	n.startContextLoop()

	return n.registerActions()
}

func (n *NDIntegration) Close() error {
	if err := n.client.SendContext(
		"Neuro Desktop integration is shutting down. WebSocket will close.",
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	wsURLFlag := flag.String("ws-url", "", "Neuro API websocket URL")
	ipcPathFlag := flag.String("ipc-file", "", "Legacy file-IPC path (fallback when no TCP executor)")
	permissionsPathFlag := flag.String("permissions-file", "", "Permissions policy file path")
	executorListenFlag := flag.String("executor-listen", "", "TCP listen addr for executor clients (default 0.0.0.0:9876)")
	adminListenFlag := flag.String("admin-listen", "", "HTTP admin dashboard addr (default 127.0.0.1:8300)")
	flag.Parse()

	wsURL := *wsURLFlag
	if wsURL == "" {
		wsURL = os.Getenv("NEURO_SDK_WS_URL")
	}
	if wsURL == "" {
		wsURL = "ws://localhost:8000"
	}

	ipcPath := *ipcPathFlag
	if ipcPath == "" {
		ipcPath = os.Getenv("NEURO_IPC_FILE")
	}
	if ipcPath == "" {
		ipcPath = "./neuro-integration-code-ipc.json"
	}

	permissionsPath := *permissionsPathFlag
	if permissionsPath == "" {
		permissionsPath = os.Getenv("NEURO_PERMISSIONS_FILE")
	}
	if permissionsPath == "" {
		permissionsPath = "./permissions.json"
	}

	executorListen := *executorListenFlag
	if executorListen == "" {
		executorListen = os.Getenv("NEURO_EXECUTOR_LISTEN")
	}
	if executorListen == "" {
		executorListen = "0.0.0.0:9876"
	}

	adminListen := *adminListenFlag
	if adminListen == "" {
		adminListen = os.Getenv("NEURO_ADMIN_LISTEN")
	}
	if adminListen == "" {
		adminListen = "127.0.0.1:8300"
	}

	log.Printf("Starting Neuro Desktop bridge (server)")
	log.Printf("- Neuro WebSocket: %s", wsURL)
	log.Printf("- Executor listen: %s", executorListen)
	log.Printf("- Admin HTTP:      http://%s/", adminListen)
	log.Printf("- File IPC fallback: %s", ipcPath)
	log.Printf("- Permissions file:  %s", permissionsPath)

	integration, err := NewNDIntegration(wsURL, "Neuro Desktop", ipcPath, permissionsPath)
	if err != nil {
		log.Fatalf("Failed to create integration: %v", err)
	}

	hub := NewExecutorHub(executorListen)
	if err := hub.Start(); err != nil {
		log.Fatalf("Failed to start executor hub: %v", err)
	}
	integration.executorHub = hub
	defer hub.Close()

	admin := NewAdminServer(integration, adminListen)
	_ = admin.Start()

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
