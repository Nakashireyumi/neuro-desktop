package main

// MCP bridge: runs Model Context Protocol servers over stdio and exposes their
// tools to Neuro as actions named mcp_<server>_<tool>.
//
// Safety rules, all enforced here rather than trusted to the caller:
//   - A server only starts when it is an enabled, installed catalog item whose
//     signature is acceptable and its operator-owned runtime grants allow it.
//   - The child process gets a minimal environment. Only the variables the
//     catalog item names are passed through, and never NEURO_* variables, so the
//     bridge's own tokens stay in the bridge.
//   - Every tool call goes through the same policy, stop switch, rate limit and
//     audit trail as the built-in actions, under the "extensions" scope.
//   - Calls have a timeout. Output is size-capped before it reaches Neuro.
//   - Servers are started with exec (no shell), so arguments are never re-parsed.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

const (
	mcpActionPrefix      = "mcp_"
	mcpProtocolVersion   = "2025-03-26"
	mcpMaxToolsPerServer = 100
	mcpMaxResultChars    = 4000
	mcpMaxArgBytes       = 64 * 1024
	mcpStartupTimeout    = 90 * time.Second // first run may download the package
	mcpListTimeout       = 20 * time.Second
	mcpDefaultCallSecs   = 30
	mcpMaxCallSecs       = 120
	mcpStopGrace         = 2 * time.Second
)

var mcpSupportedVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

var (
	mcpEnvNamePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	mcpActionNameSanity = regexp.MustCompile(`[^a-z0-9_]+`)
	mcpServerIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

// CatalogMCP describes how to run an MCP server. It lives in the signed catalog
// item, so the publisher pins the exact command and version.
type CatalogMCP struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	Env            []string `json:"env,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// validate rejects definitions that could not be run safely or that ask for
// bridge secrets.
func (spec CatalogMCP) validate() error {
	if strings.TrimSpace(spec.Command) == "" {
		return errors.New("mcp.command is required")
	}
	if len(spec.Args) > 64 {
		return errors.New("mcp.args has too many entries")
	}
	for _, name := range spec.Env {
		if !mcpEnvNamePattern.MatchString(name) {
			return fmt.Errorf("mcp.env entry %q is not a valid variable name", name)
		}
		if strings.HasPrefix(name, "NEURO_") {
			return fmt.Errorf("mcp.env may not pass %s: NEURO_* variables belong to the bridge", name)
		}
	}
	if spec.TimeoutSeconds < 0 || spec.TimeoutSeconds > mcpMaxCallSecs {
		return fmt.Errorf("mcp.timeout_seconds must be between 1 and %d", mcpMaxCallSecs)
	}
	return nil
}

func (spec CatalogMCP) callTimeout() time.Duration {
	if spec.TimeoutSeconds <= 0 {
		return mcpDefaultCallSecs * time.Second
	}
	return time.Duration(spec.TimeoutSeconds) * time.Second
}

// mcpTrustAllowed says whether a server with this signature state may run.
// Invalid and untrusted items never run. Unsigned ones run only with the same
// escape hatch the installer uses.
func mcpTrustAllowed(state string) bool {
	switch state {
	case "verified":
		return true
	case "unsigned":
		return extensionAllowsUnsigned()
	default:
		return false
	}
}

// mcpEnvironment builds the child's environment: a small base set plus the
// variables the catalog item asked for.
func mcpEnvironment(passThrough []string) []string {
	base := []string{"PATH", "HOME", "USER", "USERNAME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL"}
	if runtime.GOOS == "windows" {
		base = append(base, "SYSTEMROOT", "APPDATA", "LOCALAPPDATA", "USERPROFILE", "PATHEXT", "COMSPEC")
	}
	env := make([]string, 0, len(base)+len(passThrough))
	seen := map[string]bool{}
	for _, name := range append(base, passThrough...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// tailBuffer keeps the last few kilobytes of a child's stderr for diagnostics.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailBufferMax = 4096

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailBufferMax {
		t.buf = t.buf[len(t.buf)-tailBufferMax:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

type mcpRPCResponse struct {
	result json.RawMessage
	err    error
}

// mcpClient is one running stdio MCP server.
type mcpClient struct {
	id      string
	spec    CatalogMCP
	trust   string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  *tailBuffer
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[int64]chan mcpRPCResponse
	nextID    int64

	readerDone chan struct{}
	waitDone   chan struct{}

	mu        sync.Mutex
	state     string // starting | running | failed | stopped
	lastError string
	startedAt time.Time
	tools     []mcpTool
	version   string
}

func (c *mcpClient) status() (string, string, []mcpTool, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tools := append([]mcpTool(nil), c.tools...)
	return c.state, c.lastError, tools, c.startedAt
}

func (c *mcpClient) setFailed(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != "stopped" {
		c.state = "failed"
		c.lastError = reason
	}
}

func (c *mcpClient) write(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// call sends one JSON-RPC request and waits for its response.
func (c *mcpClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	ch := make(chan mcpRPCResponse, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	request := map[string]interface{}{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if err := c.write(request); err != nil {
		return nil, fmt.Errorf("could not write to the MCP server: %w", err)
	}

	select {
	case resp := <-ch:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s timed out", method)
	case <-c.readerDone:
		return nil, errors.New("the MCP server exited before answering")
	}
}

func (c *mcpClient) notify(method string, params interface{}) error {
	msg := map[string]interface{}{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.write(msg)
}

// readLoop routes responses to their waiting calls. It answers the two server
// requests the spec defines for clients (ping) and rejects the rest.
func (c *mcpClient) readLoop(stdout io.Reader) {
	defer close(c.readerDone)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // stray non-JSON output on stdout: ignore it, keep reading
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 {
				if msg.Method == "ping" {
					_ = c.write(map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID), "result": map[string]interface{}{}})
				} else {
					_ = c.write(map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      json.RawMessage(msg.ID),
						"error":   map[string]interface{}{"code": -32601, "message": "method not supported by Neuro Desktop"},
					})
				}
			}
			continue
		}
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch := c.pending[id]
		c.pendingMu.Unlock()
		if ch == nil {
			continue
		}
		if msg.Error != nil {
			ch <- mcpRPCResponse{err: fmt.Errorf("%s (code %d)", msg.Error.Message, msg.Error.Code)}
		} else {
			ch <- mcpRPCResponse{result: msg.Result}
		}
	}
}

// startMCPClient launches the server and performs the handshake and tool listing.
// It returns a client in state "running" or an error. A failed client is still
// stopped so that no process is left behind.
func startMCPClient(id string, spec CatalogMCP, trust string) (*mcpClient, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}

	client := &mcpClient{
		id:         id,
		spec:       spec,
		trust:      trust,
		stderr:     &tailBuffer{},
		pending:    map[int64]chan mcpRPCResponse{},
		readerDone: make(chan struct{}),
		waitDone:   make(chan struct{}),
		state:      "starting",
		startedAt:  time.Now(),
	}

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = mcpEnvironment(spec.Env)
	cmd.Stderr = client.stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %s: %w", spec.Command, err)
	}
	client.cmd = cmd
	client.stdin = stdin

	go client.readLoop(stdout)
	go func() {
		<-client.readerDone
		_ = cmd.Wait()
		close(client.waitDone)
		client.setFailed("the MCP server process exited")
	}()

	fail := func(err error) (*mcpClient, error) {
		detail := client.stderr.String()
		client.stop()
		if detail != "" {
			err = fmt.Errorf("%w (stderr: %s)", err, truncateText(detail, 400))
		}
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
	defer cancel()

	raw, err := client.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "neuro-desktop", "version": "1"},
	})
	if err != nil {
		return fail(fmt.Errorf("initialize failed: %w", err))
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &initResult); err != nil {
		return fail(fmt.Errorf("initialize returned something unexpected: %w", err))
	}
	if !mcpSupportedVersions[initResult.ProtocolVersion] {
		return fail(fmt.Errorf("protocol version %q is not supported", initResult.ProtocolVersion))
	}
	client.version = strings.TrimSpace(initResult.ServerInfo.Name + " " + initResult.ServerInfo.Version)

	if err := client.notify("notifications/initialized", nil); err != nil {
		return fail(err)
	}

	tools, err := client.listTools()
	if err != nil {
		return fail(err)
	}

	client.mu.Lock()
	client.tools = tools
	client.state = "running"
	client.mu.Unlock()
	return client, nil
}

func (c *mcpClient) listTools() ([]mcpTool, error) {
	var tools []mcpTool
	cursor := ""
	for page := 0; page < 10; page++ {
		ctx, cancel := context.WithTimeout(context.Background(), mcpListTimeout)
		params := map[string]interface{}{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("tools/list failed: %w", err)
		}
		var resp struct {
			Tools      []mcpTool `json:"tools"`
			NextCursor string    `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("tools/list returned something unexpected: %w", err)
		}
		tools = append(tools, resp.Tools...)
		if len(tools) >= mcpMaxToolsPerServer {
			tools = tools[:mcpMaxToolsPerServer]
			break
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	return tools, nil
}

// callTool runs one tool. The text it returns is what Neuro sees.
func (c *mcpClient) callTool(name string, args map[string]interface{}) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.spec.callTimeout())
	defer cancel()
	raw, err := c.call(ctx, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	if err != nil {
		return "", true, err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", true, fmt.Errorf("tools/call returned something unexpected: %w", err)
	}
	var parts []string
	for _, item := range result.Content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		} else {
			parts = append(parts, "["+item.Type+" content omitted]")
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		text = "(the tool returned no text)"
	}
	return truncateText(text, mcpMaxResultChars), result.IsError, nil
}

// stop ends the server. It closes stdin first, so a well-behaved server exits,
// then kills the process if it does not.
func (c *mcpClient) stop() {
	c.mu.Lock()
	if c.state == "stopped" {
		c.mu.Unlock()
		return
	}
	c.state = "stopped"
	c.mu.Unlock()

	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	select {
	case <-c.waitDone:
		return
	case <-time.After(mcpStopGrace):
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	select {
	case <-c.waitDone:
	case <-time.After(mcpStopGrace):
	}
}

func truncateText(text string, limit int) string {
	return boundedText(text, limit)
}

// mcpToolName builds the Neuro action name for one tool: mcp_<server>_<tool>,
// lowercase, with only letters, digits and underscores.
func mcpToolName(serverID, tool string) string {
	clean := func(s string) string {
		return strings.Trim(mcpActionNameSanity.ReplaceAllString(strings.ToLower(s), "_"), "_")
	}
	return mcpActionPrefix + clean(serverID) + "_" + clean(tool)
}

// mcpSchema turns an MCP input schema into the object schema Neuro requires.
func mcpSchema(input map[string]interface{}) *neuro.ActionSchema {
	properties := map[string]interface{}{}
	if raw, ok := input["properties"].(map[string]interface{}); ok {
		properties = raw
	}
	var required []string
	if raw, ok := input["required"].([]interface{}); ok {
		for _, item := range raw {
			if name, ok := item.(string); ok {
				required = append(required, name)
			}
		}
	}
	return &neuro.ActionSchema{Type: "object", Properties: properties, Required: required}
}

// mcpCallJob performs no tool I/O until Execute, after the action acknowledgement.
// Consent and the exact managed process are rechecked before calling and before
// disclosure; reapproval cannot recover the output of an older process/session.
type mcpCallJob struct {
	action      *mcpToolAction
	args        map[string]interface{}
	client      *mcpClient
	policy      *PermissionPolicy
	grant       ScopeGrant
	temporary   bool
	runOnce     sync.Once
	deliverOnce sync.Once
	done        chan struct{}
	text        string
	failed      bool
}

func extensionScopeGrant(policy *PermissionPolicy) ScopeGrant {
	if policy == nil || policy.grants == nil {
		return ScopeGrant{}
	}
	policy.grants.mu.Lock()
	defer policy.grants.mu.Unlock()
	return policy.grants.grants[ScopeExtensions]
}

func (job *mcpCallJob) checkAllowed() error {
	if reason := job.action.integration.stop.blockReason(job.action.name); reason != "" {
		return errors.New(reason)
	}
	policy := job.action.integration.policy()
	if policy == nil || policy != job.policy || !policy.IsAllowed(job.action.name) || !policy.ScopeAllowed(ScopeExtensions) {
		return errors.New("the tool approval changed; request a new call after approval")
	}
	if job.temporary && !sameScopeGrant(job.grant, extensionScopeGrant(policy)) {
		return errors.New("the temporary tool approval changed; request a new call")
	}
	if err := job.action.integration.extensionItemRuntimeAllowed(job.action.serverID); err != nil {
		return err
	}
	client := job.action.integration.mcp.client(job.action.serverID)
	if client == nil || client != job.client {
		return errors.New("the MCP server stopped or was replaced; request a new call")
	}
	if state, _, _, _ := client.status(); state != "running" {
		return errors.New("the MCP server is no longer running")
	}
	return nil
}

func (job *mcpCallJob) run() {
	job.runOnce.Do(func() {
		defer close(job.done)
		if err := job.checkAllowed(); err != nil {
			job.text, job.failed = err.Error(), true
			return
		}
		text, isError, err := job.client.callTool(job.action.toolName, job.args)
		if approvalErr := job.checkAllowed(); approvalErr != nil {
			job.text, job.failed = approvalErr.Error(), true
		} else if err != nil {
			job.text, job.failed = err.Error(), true
		} else {
			job.text, job.failed = text, isError
		}
	})
}

// mcpToolAction is one MCP tool exposed to Neuro. It implements neuro.ActionHandler.
type mcpToolAction struct {
	integration *NDIntegration
	serverID    string
	toolName    string
	name        string
	description string
	schema      *neuro.ActionSchema
}

func (a *mcpToolAction) GetName() string                { return a.name }
func (a *mcpToolAction) GetDescription() string         { return a.description }
func (a *mcpToolAction) GetSchema() *neuro.ActionSchema { return a.schema }

func (a *mcpToolAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
	n := a.integration
	n.stats.noteAction(a.name)

	if reason := n.stop.blockReason(a.name); reason != "" {
		n.stats.noteDenied(a.name)
		return nil, neuro.NewFailureResult(reason)
	}
	policy := n.policy()
	if policy != nil && !policy.IsAllowed(a.name) {
		n.stats.noteDenied(a.name)
		n.audit.record("action", map[string]interface{}{"action": a.name, "decision": "refused", "reason": "policy"})
		return nil, neuro.NewFailureResult(policyDenialMessage(a.name, policy))
	}
	if policy != nil {
		if limit := policy.ScopeRateLimit(ScopeExtensions); limit > 0 {
			if allowed, retryAfter := n.rate.allow(ScopeExtensions, limit, time.Now()); !allowed {
				n.stats.noteDenied(a.name)
				return nil, neuro.NewFailureResult(rateLimitDenial(ScopeExtensions, limit, retryAfter))
			}
		}
	}

	if len(data) > mcpMaxArgBytes {
		return nil, neuro.NewFailureResult(fmt.Sprintf("arguments are larger than %d bytes", mcpMaxArgBytes))
	}
	args := map[string]interface{}{}
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && string(trimmed) != "null" {
		if err := json.Unmarshal(trimmed, &args); err != nil {
			return nil, neuro.NewFailureResult("the arguments must be a JSON object that matches the tool's schema")
		}
	}
	if reason := validateActionParameters(actionSpec{Name: CommandType(a.name), Schema: a.schema, Kind: "mcp"}, args); reason != "" {
		return nil, neuro.NewFailureResult(reason)
	}

	client := n.mcp.client(a.serverID)
	if client == nil {
		return nil, neuro.NewFailureResult(fmt.Sprintf("the %s MCP server is not running. Vedal can start it from the dashboard's Extensions page.", a.serverID))
	}
	if state, lastError, _, _ := client.status(); state != "running" {
		return nil, neuro.NewFailureResult(fmt.Sprintf("the %s MCP server is %s: %s", a.serverID, state, lastError))
	}
	if err := n.extensionItemRuntimeAllowed(a.serverID); err != nil {
		n.stats.noteDenied(a.name)
		n.audit.record("action", map[string]interface{}{"action": a.name, "decision": "refused", "reason": "extension permissions"})
		return nil, neuro.NewFailureResult(err.Error())
	}

	job := &mcpCallJob{action: a, args: args, client: client, policy: policy, done: make(chan struct{})}
	if policy != nil && !policy.scopes[ScopeExtensions].Allowed {
		job.temporary, job.grant = true, extensionScopeGrant(policy)
	}
	n.audit.record("action", map[string]interface{}{"action": a.name, "decision": "accepted", "server": a.serverID})
	return job, neuro.NewSuccessResult("Accepted. The tool result will follow as context.")
}

func (a *mcpToolAction) Execute(state interface{}) {
	job, ok := state.(*mcpCallJob)
	if !ok {
		return
	}
	job.run()
	job.deliverOnce.Do(func() {
		if err := job.checkAllowed(); err != nil {
			job.text, job.failed = err.Error(), true
		}
		heading := "## " + a.name + " finished"
		if job.failed {
			heading = "## " + a.name + " failed"
			a.integration.stats.noteFailure(a.name)
		}
		if a.integration.client != nil {
			_ = a.integration.client.SendContext(boundedContextMessage(heading+"\n"+job.text), false)
		}
	})
}

// mcpManager keeps the running servers and the actions they registered.
type mcpManager struct {
	mu        sync.Mutex
	clients   map[string]*mcpClient
	starting  map[string]bool
	cancelled map[string]bool // stopped while still starting: discard when ready
	failed    map[string]mcpFailure
	tools     map[string][]string // server id -> action names registered for it
}

type mcpFailure struct {
	reason string
	at     time.Time
}

func newMCPManager() *mcpManager {
	return &mcpManager{
		clients:   map[string]*mcpClient{},
		starting:  map[string]bool{},
		cancelled: map[string]bool{},
		failed:    map[string]mcpFailure{},
		tools:     map[string][]string{},
	}
}

func (m *mcpManager) client(id string) *mcpClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients[id]
}

func (m *mcpManager) isStarting(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.starting[id]
}

func (m *mcpManager) markStarting(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.starting[id] || m.clients[id] != nil {
		return false
	}
	m.starting[id] = true
	delete(m.failed, id)
	delete(m.cancelled, id)
	return true
}

// cancelStart is called when a server is stopped during startup. It returns
// true when a start is in flight, so the start can discard itself.
func (m *mcpManager) cancelStart(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.starting[id] {
		return false
	}
	m.cancelled[id] = true
	return true
}

func (m *mcpManager) wasCancelled(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelled[id] {
		delete(m.cancelled, id)
		delete(m.starting, id)
		return true
	}
	return false
}

func (m *mcpManager) finishStart(id string, client *mcpClient, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.starting, id)
	if err != nil {
		m.failed[id] = mcpFailure{reason: err.Error(), at: time.Now()}
		return
	}
	m.clients[id] = client
}

// activeIDs returns the ids of servers that are running or starting.
func (m *mcpManager) activeIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for id := range m.clients {
		seen[id] = true
	}
	for id := range m.starting {
		seen[id] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *mcpManager) forget(id string) (*mcpClient, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	client := m.clients[id]
	delete(m.clients, id)
	delete(m.failed, id)
	if m.starting[id] {
		m.cancelled[id] = true
	}
	names := m.tools[id]
	delete(m.tools, id)
	return client, names
}

// mcpServerStatus is what the runtime page shows for one server.
type mcpServerStatus struct {
	ID        string   `json:"id"`
	State     string   `json:"state"`
	Trust     string   `json:"trust,omitempty"`
	Server    string   `json:"server,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	LastError string   `json:"last_error,omitempty"`
	StartedAt string   `json:"started_at,omitempty"`
}

func (m *mcpManager) snapshot() []mcpServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := map[string]bool{}
	for id := range m.clients {
		ids[id] = true
	}
	for id := range m.starting {
		ids[id] = true
	}
	for id := range m.failed {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	out := make([]mcpServerStatus, 0, len(sorted))
	for _, id := range sorted {
		entry := mcpServerStatus{ID: id, State: "starting"}
		if client := m.clients[id]; client != nil {
			state, lastError, tools, startedAt := client.status()
			entry.State = state
			entry.LastError = lastError
			entry.Trust = client.trust
			entry.Server = client.version
			entry.StartedAt = startedAt.UTC().Format(time.RFC3339)
			for _, tool := range tools {
				entry.Tools = append(entry.Tools, mcpToolName(id, tool.Name))
			}
		} else if failure, ok := m.failed[id]; ok && !m.starting[id] {
			entry.State = "failed"
			entry.LastError = failure.reason
		}
		out = append(out, entry)
	}
	return out
}

// runningCount is used by the runtime endpoint.
func (m *mcpManager) runningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, client := range m.clients {
		if state, _, _, _ := client.status(); state == "running" {
			count++
		}
	}
	return count
}

// handlers returns the action handlers for every running server. A mode switch
// rebuilds the action list, so registerActions includes these too.
func (m *mcpManager) handlers(n *NDIntegration) []neuro.ActionHandler {
	return m.handlersFor(n, "")
}

// handlersFor returns the handlers for one server, or for all when only is "".
func (m *mcpManager) handlersFor(n *NDIntegration, only string) []neuro.ActionHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []neuro.ActionHandler
	ids := make([]string, 0, len(m.clients))
	for id := range m.clients {
		if only == "" || only == id {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		client := m.clients[id]
		_, _, tools, _ := client.status()
		for _, tool := range tools {
			name := mcpToolName(id, tool.Name)
			desc := strings.TrimSpace(tool.Description)
			if desc == "" {
				desc = "Tool " + tool.Name + " of the " + id + " MCP server."
			}
			out = append(out, &mcpToolAction{
				integration: n,
				serverID:    id,
				toolName:    tool.Name,
				name:        name,
				description: truncateText("["+id+" MCP server] "+desc, 500),
				schema:      mcpSchema(tool.InputSchema),
			})
		}
	}
	return out
}

// syncMCPServers starts the enabled, trusted MCP extensions and stops the ones
// that are no longer enabled. It returns at once; startups run in the background.
func (n *NDIntegration) syncMCPServers() {
	if n == nil || n.mcp == nil {
		return
	}
	want := n.desiredMCPServers()

	for _, id := range n.mcp.activeIDs() {
		if _, keep := want[id]; !keep {
			n.stopMCPServer(id)
		}
	}
	for id, entry := range want {
		if !n.mcp.markStarting(id) {
			continue
		}
		go n.startMCPServer(id, entry.spec, entry.trust)
	}
}

type desiredMCP struct {
	spec  CatalogMCP
	trust string
}

// desiredMCPServers reads the install state and the catalog. Anything that is
// not enabled, not in the catalog, lacks an mcp block, or fails trust is left out.
func (n *NDIntegration) desiredMCPServers() map[string]desiredMCP {
	out := map[string]desiredMCP{}
	state, err := loadExtensionState(extensionStatePath())
	if err != nil {
		return out
	}
	index, err := loadCatalogIndex(catalogFilePath())
	if err != nil {
		return out
	}
	publishers := loadPublishersOrEmpty()

	for id, install := range state.Installed {
		if !install.Enabled || !mcpServerIDPattern.MatchString(id) {
			continue
		}
		item := findCatalogItemByID(index, id)
		if item == nil || item.MCP == nil {
			continue
		}
		if err := item.MCP.validate(); err != nil {
			continue
		}
		status := verifyCatalogItem(*item, publishers)
		if !mcpTrustAllowed(status.State) {
			continue
		}
		if err := n.extensionRuntimeAllowed(id, item.Permissions); err != nil {
			continue
		}
		out[id] = desiredMCP{spec: *item.MCP, trust: status.State}
	}
	return out
}

func (n *NDIntegration) startMCPServer(id string, spec CatalogMCP, trust string) {
	client, err := startMCPClient(id, spec, trust)
	if err != nil {
		n.mcp.finishStart(id, nil, err)
		log.Printf("MCP server %s did not start: %v", id, err)
		return
	}

	if n.mcp.wasCancelled(id) {
		// Stopped while it was starting: do not leave the process behind.
		client.stop()
		log.Printf("MCP server %s was stopped during startup", id)
		return
	}

	_, _, tools, _ := client.status()
	var names []string
	for _, tool := range tools {
		names = append(names, mcpToolName(id, tool.Name))
	}

	n.mcp.mu.Lock()
	n.mcp.clients[id] = client
	delete(n.mcp.starting, id)
	n.mcp.tools[id] = names
	n.mcp.mu.Unlock()

	handlers := n.mcp.handlersFor(n, id)
	if len(handlers) > 0 && n.client != nil {
		currentActionListMu.Lock()
		for _, handler := range handlers {
			currentActionList[handler.GetName()] = handler
		}
		currentActionListMu.Unlock()
		if err := n.client.RegisterActions(handlers); err != nil {
			log.Printf("MCP server %s: could not register its tools: %v", id, err)
		}
	}
	log.Printf("MCP server %s is running with %d tool(s)", id, len(tools))
}

// stopMCPServer stops one server and withdraws its actions from Neuro.
func (n *NDIntegration) stopMCPServer(id string) {
	client, names := n.mcp.forget(id)
	n.mcp.cancelStart(id)
	if len(names) > 0 {
		currentActionListMu.Lock()
		for _, name := range names {
			delete(currentActionList, name)
		}
		currentActionListMu.Unlock()
		if n.client != nil {
			if err := n.client.UnregisterActions(names); err != nil {
				log.Printf("MCP server %s: could not withdraw its tools: %v", id, err)
			}
		}
	}
	if client != nil {
		client.stop()
		log.Printf("MCP server %s stopped", id)
	}
}

func (n *NDIntegration) stopAllMCPServers() {
	if n == nil || n.mcp == nil {
		return
	}
	for _, id := range n.mcp.activeIDs() {
		n.stopMCPServer(id)
	}
}

// mcpSnapshotForRuntime is nil-safe for the admin server.
func (n *NDIntegration) mcpSnapshotForRuntime() []mcpServerStatus {
	if n == nil || n.mcp == nil {
		return []mcpServerStatus{}
	}
	out := n.mcp.snapshot()
	if out == nil {
		return []mcpServerStatus{}
	}
	return out
}
