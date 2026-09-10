// Package neuro provides a Go SDK for integrating games with Neuro-sama
package neuro

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message Types

// Message represents the base websocket message structure
type Message struct {
	Command string          `json:"command"`
	Game    string          `json:"game,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ActionSchema represents a JSON schema for action parameters
// Note: Must have Type "object" if provided. Many JSON schema keywords are not supported.
// See: https://github.com/VedalAI/neuro-sdk/blob/main/API/SPECIFICATION.md#action
type ActionSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// ActionDefinition represents an action that can be registered with Neuro
type ActionDefinition struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Schema      *ActionSchema `json:"schema,omitempty"`
}

// IncomingAction represents an action sent by Neuro
type IncomingAction struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Data is JSON-stringified action parameters (not raw JSON)
	Data string `json:"data,omitempty"`
}

// ExecutionResult represents the result of validating/executing an action
type ExecutionResult struct {
	Successful bool
	Message    string
}

// NewSuccessResult creates a successful execution result
func NewSuccessResult(message string) ExecutionResult {
	return ExecutionResult{Successful: true, Message: message}
}

// NewFailureResult creates a failed execution result
func NewFailureResult(message string) ExecutionResult {
	return ExecutionResult{Successful: false, Message: message}
}

// Priority levels for action forces
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// Action Handler Interface

// ActionHandler defines the interface for handling actions
// Important: Neuro can call actions at any time after registration,
// even before you send an action force. Always be prepared to handle actions.
type ActionHandler interface {
	// GetName returns the unique identifier for this action
	GetName() string
	// GetDescription returns a description of what this action does
	GetDescription() string
	// GetSchema returns the JSON schema for action parameters (can be nil)
	// If provided, schema must have Type "object"
	GetSchema() *ActionSchema
	// Validate checks if the incoming action data is valid
	// Data comes directly from Neuro and may be malformed - always validate!
	Validate(data json.RawMessage) (state interface{}, result ExecutionResult)
	// Execute performs the actual action using the validated state
	Execute(state interface{})
}

// Client Configuration

// ClientConfig holds configuration for the Neuro client
type ClientConfig struct {
	Game         string
	WebsocketURL string
	Logger       *log.Logger
}

// Client

// Client manages the websocket connection to Neuro
type Client struct {
	config ClientConfig
	conn   *websocket.Conn
	connMu sync.RWMutex

	// Registered actions
	actions   map[string]ActionHandler
	actionsMu sync.RWMutex

	// Raw command handlers (for commands outside action lifecycle)
	commandHandlers   map[string]func(json.RawMessage)
	commandHandlersMu sync.RWMutex

	// Channels
	actionChan chan IncomingAction
	errChan    chan error
	closeChan  chan struct{}

	// State
	connected bool
	closed    bool

	logger *log.Logger
}

// NewClient creates a new Neuro SDK client
func NewClient(config ClientConfig) (*Client, error) {
	if config.Game == "" {
		return nil, errors.New("game name is required")
	}
	if config.WebsocketURL == "" {
		return nil, errors.New("websocket URL is required")
	}

	c := &Client{
		config:          config,
		actions:         make(map[string]ActionHandler),
		commandHandlers: make(map[string]func(json.RawMessage)),
		actionChan:      make(chan IncomingAction, 16),
		errChan:         make(chan error, 8),
		closeChan:       make(chan struct{}),
		logger:          config.Logger,
	}

	if c.logger == nil {
		c.logger = log.Default()
	}

	return c, nil
}

// Connect establishes the websocket connection and starts the message loop
func (c *Client) Connect() error {
	c.connMu.Lock()
	locked := true
	defer func() {
		if locked {
			c.connMu.Unlock()
		}
	}()

	if c.closed {
		return errors.New("client is closed")
	}
	if c.connected {
		return errors.New("already connected")
	}

	u, err := url.Parse(c.config.WebsocketURL)
	if err != nil {
		return fmt.Errorf("invalid websocket URL: %w", err)
	}

	c.logger.Printf("Connecting to %s...", u.String())

	// Set connection timeout to prevent hanging
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to connect (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.logger.Printf("WebSocket connection established")

	c.conn = conn
	c.connected = true

	// Start reader goroutine
	go c.readLoop()

	// CRITICAL: Unlock BEFORE calling Startup() to avoid deadlock
	// Startup() calls send() which needs to acquire a read lock
	c.connMu.Unlock()
	locked = false

	// Send startup message
	if err := c.Startup(); err != nil {
		c.logger.Printf("Failed to send startup message: %v", err)
		return fmt.Errorf("failed to send startup: %w", err)
	}

	c.logger.Printf("Startup message sent successfully")

	return nil
}

// Message Reading

func (c *Client) readLoop() {
	c.logger.Printf("Read loop started")
	for {
		select {
		case <-c.closeChan:
			c.logger.Printf("Read loop stopping (close signal)")
			return
		default:
			_, msgBytes, err := c.conn.ReadMessage()
			if err != nil {
				if !c.closed {
					c.logger.Printf("Read error: %v", err)
					c.errChan <- fmt.Errorf("read error: %w", err)
				}
				return
			}

			c.logger.Printf("Received message: %s", string(msgBytes))

			if err := c.handleMessage(msgBytes); err != nil {
				c.logger.Printf("Error handling message: %v", err)
			}
		}
	}
}

func (c *Client) handleMessage(msgBytes []byte) error {
	var msg Message
	if err := json.Unmarshal(msgBytes, &msg); err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	c.logger.Printf("Received command: %s", msg.Command)

	switch msg.Command {
	case "action":
		var action IncomingAction
		if err := json.Unmarshal(msg.Data, &action); err != nil {
			return fmt.Errorf("failed to parse action data: %w", err)
		}

		// Legacy compatibility channel
		select {
		case c.actionChan <- action:
		default:
			c.logger.Printf("Action channel full, dropping action: %s", action.Name)
		}

		// Handle action in goroutine to avoid blocking the read loop
		go c.handleAction(action)

	case "actions/reregister_all":
		c.logger.Printf("Received reregister_all request")
		// Resend all registered actions
		go c.resendRegisteredActions()

	default:
		c.commandHandlersMu.RLock()
		handler, exists := c.commandHandlers[msg.Command]
		c.commandHandlersMu.RUnlock()

		if exists {
			go handler(msg.Data)
		} else {
			c.logger.Printf("Unhandled command: %s", msg.Command)
		}
	}

	return nil
}

func (c *Client) handleAction(action IncomingAction) {
	c.actionsMu.RLock()
	handler, exists := c.actions[action.Name]
	c.actionsMu.RUnlock()

	if !exists {
		c.logger.Printf("Unknown action: %s", action.Name)
		c.SendActionResult(action.ID, false, fmt.Sprintf("Unknown action: %s", action.Name))
		return
	}

	c.logger.Printf("Handling action: %s (ID: %s)", action.Name, action.ID)

	// Parse the JSON-stringified data from Neuro
	var actionData json.RawMessage
	if action.Data != "" {
		// Data comes as a JSON string, need to parse it
		if err := json.Unmarshal([]byte(action.Data), &actionData); err != nil {
			c.logger.Printf("Failed to parse action data JSON: %v", err)
			c.SendActionResult(action.ID, false, "Invalid JSON in action data")
			return
		}
	}

	// Validate (data may be malformed or not match schema)
	state, result := handler.Validate(actionData)

	c.logger.Printf("Action validation result: success=%v, message=%s", result.Successful, result.Message)

	// Always acknowledge the action with a result message.
	if err := c.SendActionResult(action.ID, result.Successful, result.Message); err != nil {
		c.logger.Printf("Failed to send action result: %v", err)
	}

	// Execute if successful
	if result.Successful {
		c.logger.Printf("Executing action: %s", action.Name)
		handler.Execute(state)
	}
}

// OnCommand registers a handler for non-action command messages.
func (c *Client) OnCommand(command string, handler func(json.RawMessage)) {
	c.commandHandlersMu.Lock()
	defer c.commandHandlersMu.Unlock()
	c.commandHandlers[command] = handler
}

// Message Sending

func (c *Client) send(msg Message) error {
	c.connMu.RLock()
	defer c.connMu.RUnlock()

	if !c.connected {
		return errors.New("not connected")
	}

	msg.Game = c.config.Game

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	c.logger.Printf("Sending: %s - %s", msg.Command, string(msgBytes))

	if err := c.conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Startup sends the initial startup message
func (c *Client) Startup() error {
	c.logger.Printf("Sending startup message...")
	return c.send(Message{Command: "startup", Game: c.config.Game})
}

// SendContext sends a context message to Neuro
func (c *Client) SendContext(message string, silent bool) error {
	data := map[string]interface{}{
		"message": message,
		"silent":  silent,
	}
	dataBytes, _ := json.Marshal(data)

	return c.send(Message{
		Command: "context",
		Data:    dataBytes,
	})
}

// SendShutdownReady notifies Neuro that the integration is ready to shut down
func (c *Client) SendShutdownReady() error {
	return c.send(Message{Command: "shutdown/ready"})
}

// Action Management

// RegisterAction registers a single action handler
func (c *Client) RegisterAction(handler ActionHandler) error {
	return c.RegisterActions([]ActionHandler{handler})
}

// RegisterActions registers multiple action handlers
func (c *Client) RegisterActions(handlers []ActionHandler) error {
	if len(handlers) == 0 {
		return nil
	}

	c.actionsMu.Lock()
	defer c.actionsMu.Unlock()

	actions := make([]ActionDefinition, 0, len(handlers))
	for _, h := range handlers {
		name := h.GetName()
		if name == "" {
			return errors.New("action name cannot be empty")
		}

		c.actions[name] = h

		actions = append(actions, ActionDefinition{
			Name:        name,
			Description: h.GetDescription(),
			Schema:      h.GetSchema(),
		})
	}

	data := map[string]interface{}{
		"actions": actions,
	}
	dataBytes, _ := json.Marshal(data)

	c.logger.Printf("Registering %d action(s)", len(actions))

	return c.send(Message{
		Command: "actions/register",
		Data:    dataBytes,
	})
}

// UnregisterAction unregisters a single action by name
func (c *Client) UnregisterAction(name string) error {
	return c.UnregisterActions([]string{name})
}

// UnregisterActions unregisters multiple actions by name
func (c *Client) UnregisterActions(names []string) error {
	if len(names) == 0 {
		return nil
	}

	c.actionsMu.Lock()
	defer c.actionsMu.Unlock()

	for _, name := range names {
		delete(c.actions, name)
	}

	data := map[string]interface{}{
		"action_names": names,
	}
	dataBytes, _ := json.Marshal(data)

	return c.send(Message{
		Command: "actions/unregister",
		Data:    dataBytes,
	})
}

func (c *Client) resendRegisteredActions() {
	c.actionsMu.RLock()
	handlers := make([]ActionHandler, 0, len(c.actions))
	for _, h := range c.actions {
		handlers = append(handlers, h)
	}
	c.actionsMu.RUnlock()

	if len(handlers) > 0 {
		c.logger.Printf("Re-registering %d action(s)", len(handlers))
		if err := c.RegisterActions(handlers); err != nil {
			c.logger.Printf("Failed to resend registered actions: %v", err)
		}
	}
}

// ForceActions forces Neuro to execute one of the specified actions
func (c *Client) ForceActions(query string, actionNames []string, opts ...ForceOption) error {
	if len(actionNames) == 0 {
		return errors.New("must specify at least one action name")
	}

	config := &forceConfig{
		priority:         PriorityLow,
		ephemeralContext: false,
	}

	for _, opt := range opts {
		opt(config)
	}

	data := map[string]interface{}{
		"query":             query,
		"action_names":      actionNames,
		"ephemeral_context": config.ephemeralContext,
		"priority":          config.priority,
	}

	if config.state != "" {
		data["state"] = config.state
	}

	dataBytes, _ := json.Marshal(data)

	return c.send(Message{
		Command: "actions/force",
		Data:    dataBytes,
	})
}

// ForceOption configures action forcing
type ForceOption func(*forceConfig)

type forceConfig struct {
	state            string
	ephemeralContext bool
	priority         Priority
}

// WithState adds state information to the action force
func WithState(state string) ForceOption {
	return func(c *forceConfig) {
		c.state = state
	}
}

// WithEphemeralContext marks the context as ephemeral
func WithEphemeralContext(ephemeral bool) ForceOption {
	return func(c *forceConfig) {
		c.ephemeralContext = ephemeral
	}
}

// WithPriority sets the priority level for the action force
func WithPriority(priority Priority) ForceOption {
	return func(c *forceConfig) {
		c.priority = priority
	}
}

// SendActionResult sends the result of an action execution
func (c *Client) SendActionResult(id string, success bool, message string) error {
	data := map[string]interface{}{
		"id":      id,
		"success": success,
		"message": message,
	}
	dataBytes, _ := json.Marshal(data)

	return c.send(Message{
		Command: "action/result",
		Data:    dataBytes,
	})
}

// Channels

// Actions returns a channel that receives incoming actions
// Deprecated: Actions are now handled automatically via ActionHandlers
func (c *Client) Actions() <-chan IncomingAction {
	return c.actionChan
}

// Errors returns a channel that receives errors
func (c *Client) Errors() <-chan error {
	return c.errChan
}

// Close closes the websocket connection
func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.closed {
		return nil
	}

	c.logger.Printf("Closing client...")
	c.closed = true
	close(c.closeChan)

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// Helper Functions

// WrapSchema wraps properties into a proper JSON schema object
// The resulting schema will have Type "object" as required by the Neuro API
func WrapSchema(properties map[string]interface{}, required []string) *ActionSchema {
	return &ActionSchema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

// ParseActionData is a helper to parse action data into a struct
// Note: Data from Neuro may be malformed - always check the error
func ParseActionData(data json.RawMessage, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// Action Window

// ActionWindow represents a temporary set of actions that will be forced
type ActionWindow struct {
	client     *Client
	actions    []ActionHandler
	forceOpts  []ForceOption
	query      string
	registered bool
	mu         sync.Mutex
}

// NewActionWindow creates a new action window
func (c *Client) NewActionWindow() *ActionWindow {
	return &ActionWindow{
		client:  c,
		actions: make([]ActionHandler, 0),
	}
}

// AddAction adds an action to the window
func (w *ActionWindow) AddAction(handler ActionHandler) *ActionWindow {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.registered {
		w.client.logger.Printf("Cannot add action to registered window")
		return w
	}

	w.actions = append(w.actions, handler)
	return w
}

// SetForce configures the action force parameters
func (w *ActionWindow) SetForce(query string, opts ...ForceOption) *ActionWindow {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.registered {
		w.client.logger.Printf("Cannot modify registered window")
		return w
	}

	w.query = query
	w.forceOpts = opts
	return w
}

// Register registers the action window and forces the actions
func (w *ActionWindow) Register() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.registered {
		return errors.New("window already registered")
	}
	if len(w.actions) == 0 {
		return errors.New("no actions in window")
	}

	w.registered = true

	// Register actions
	if err := w.client.RegisterActions(w.actions); err != nil {
		return fmt.Errorf("failed to register actions: %w", err)
	}

	// Get action names
	names := make([]string, len(w.actions))
	for i, a := range w.actions {
		names[i] = a.GetName()
	}

	// Force actions after a brief delay to ensure registration completes
	go func() {
		time.Sleep(100 * time.Millisecond)
		w.client.logger.Printf("Forcing actions in window: %v", names)
		if err := w.client.ForceActions(w.query, names, w.forceOpts...); err != nil {
			w.client.logger.Printf("Failed to force actions: %v", err)
		}
	}()

	return nil
}

// End unregisters the actions in this window
func (w *ActionWindow) End() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.registered {
		return nil
	}

	names := make([]string, len(w.actions))
	for i, a := range w.actions {
		names[i] = a.GetName()
	}

	return w.client.UnregisterActions(names)
}
