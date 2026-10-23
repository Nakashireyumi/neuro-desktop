package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// The fake MCP server runs as this test binary re-executed with an env flag, so
// the stdio path is exercised without Node, Python or network access.
const fakeMCPEnv = "NDTEST_FAKE_MCP"

func TestMain(m *testing.M) {
	if os.Getenv(fakeMCPEnv) == "1" {
		runFakeMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeMCPServer() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 {
			continue // notification
		}
		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "fake-mcp", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "echo",
						"description": "Echo the text back.",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
							"required":   []interface{}{"text"},
						},
					},
					map[string]interface{}{"name": "fail", "description": "Always reports an error.", "inputSchema": map[string]interface{}{"type": "object"}},
					map[string]interface{}{"name": "slow", "description": "Takes a while.", "inputSchema": map[string]interface{}{"type": "object"}},
					map[string]interface{}{"name": "env", "description": "Lists the environment variable names.", "inputSchema": map[string]interface{}{"type": "object"}},
				},
			}
		case "tools/call":
			switch req.Params.Name {
			case "echo":
				text, _ := req.Params.Arguments["text"].(string)
				result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": text}}}
			case "fail":
				result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "it broke"}}, "isError": true}
			case "slow":
				time.Sleep(300 * time.Millisecond)
				result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "finally"}}}
			case "env":
				var names []string
				for _, pair := range os.Environ() {
					names = append(names, strings.SplitN(pair, "=", 2)[0])
				}
				sort.Strings(names)
				result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": strings.Join(names, ",")}}}
			default:
				_ = enc.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "error": map[string]interface{}{"code": -32602, "message": "unknown tool"}})
				continue
			}
		case "ping":
			result = map[string]interface{}{}
		default:
			_ = enc.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "error": map[string]interface{}{"code": -32601, "message": "no such method"}})
			continue
		}
		_ = enc.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

func fakeMCPSpec(t *testing.T, env ...string) CatalogMCP {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot find the test binary: %v", err)
	}
	t.Setenv(fakeMCPEnv, "1")
	// The flag reaches the child the same way a catalog item would ask for it.
	return CatalogMCP{Command: exe, Env: append([]string{fakeMCPEnv}, env...), TimeoutSeconds: 10}
}

func startFakeMCP(t *testing.T, env ...string) *mcpClient {
	t.Helper()
	client, err := startMCPClient("fake", fakeMCPSpec(t, env...), "verified")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	t.Cleanup(client.stop)
	return client
}

func TestMCPHandshakeListsToolsAndCallsThem(t *testing.T) {
	client := startFakeMCP(t)
	state, lastError, tools, _ := client.status()
	if state != "running" || lastError != "" {
		t.Fatalf("state = %q, error = %q", state, lastError)
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "echo,fail,slow,env" {
		t.Fatalf("tools = %v", names)
	}

	text, isError, err := client.callTool("echo", map[string]interface{}{"text": "hello there"})
	if err != nil || isError || text != "hello there" {
		t.Fatalf("echo = %q, isError=%v, err=%v", text, isError, err)
	}

	text, isError, err = client.callTool("fail", nil)
	if err != nil || !isError || text != "it broke" {
		t.Fatalf("fail = %q, isError=%v, err=%v", text, isError, err)
	}
}

func TestMCPChildGetsOnlyTheEnvironmentItAskedFor(t *testing.T) {
	t.Setenv("NDTEST_SECRET_FOR_MCP", "bridge-only")
	t.Setenv("MCP_ALLOWED_FOR_TEST", "yes")
	client := startFakeMCP(t, "MCP_ALLOWED_FOR_TEST")

	names, _, err := client.callTool("env", nil)
	if err != nil {
		t.Fatalf("env call failed: %v", err)
	}
	if !strings.Contains(names, "MCP_ALLOWED_FOR_TEST") {
		t.Fatalf("the requested variable was not passed through: %s", names)
	}
	if strings.Contains(names, "NDTEST_SECRET_FOR_MCP") {
		t.Fatalf("a bridge variable leaked into the MCP child: %s", names)
	}
}

func TestMCPSpecRejectsBridgeSecretsAndBadInput(t *testing.T) {
	cases := map[string]CatalogMCP{
		"empty command":  {Command: "  "},
		"bridge secret":  {Command: "x", Env: []string{"NEURO_ADMIN_TOKEN"}},
		"bad env name":   {Command: "x", Env: []string{"lower-case"}},
		"too many args":  {Command: "x", Args: make([]string, 65)},
		"absurd timeout": {Command: "x", TimeoutSeconds: 10000},
	}
	for name, spec := range cases {
		if err := spec.validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if err := (CatalogMCP{Command: "npx", Args: []string{"-y", "pkg@1.0.0"}, Env: []string{"HTTP_PROXY"}}).validate(); err != nil {
		t.Fatalf("a normal definition was rejected: %v", err)
	}
}

func TestMCPStartFailureIsReportedNotHung(t *testing.T) {
	_, err := startMCPClient("broken", CatalogMCP{Command: "/definitely/not/a/real/binary-xyz"}, "verified")
	if err == nil {
		t.Fatal("a missing binary must fail to start")
	}
}

func TestMCPToolNamesAreSafeActionNames(t *testing.T) {
	cases := map[string]string{
		mcpToolName("My Server", "Get-Time!"):    "mcp_my_server_get_time",
		mcpToolName("memory", "create_entities"): "mcp_memory_create_entities",
		mcpToolName("a/b", "../../etc"):          "mcp_a_b_etc",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("mcpToolName = %q, want %q", got, want)
		}
	}
}

func TestMCPTrustGate(t *testing.T) {
	t.Setenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED", "")
	if !mcpTrustAllowed("verified") {
		t.Fatal("verified servers may run")
	}
	for _, state := range []string{"unsigned", "untrusted", "invalid", ""} {
		if mcpTrustAllowed(state) {
			t.Errorf("state %q must not run without the escape hatch", state)
		}
	}
	// The escape hatch exists only in a dev build (-tags neurodev); a release
	// build ignores the variable, so unsigned servers stay blocked there.
	t.Setenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED", "1")
	if got := mcpTrustAllowed("unsigned"); got != extensionsDevBuild {
		t.Fatalf("unsigned servers run only in a dev build with the escape hatch set (dev build: %v, got %v)", extensionsDevBuild, got)
	}
	if mcpTrustAllowed("invalid") {
		t.Fatal("an invalid signature never runs, even with the escape hatch")
	}
}

func TestMCPToolsFallUnderTheExtensionsScope(t *testing.T) {
	if scope, ok := scopeForAction("mcp_memory_create"); !ok || scope != ScopeExtensions {
		t.Fatalf("scopeForAction(mcp_*) = %q, %v", scope, ok)
	}
	if !scopeRequiresExplicitConsent(ScopeExtensions) {
		t.Fatal("the extensions scope must need explicit consent")
	}
	policy := defaultPermissionPolicy()
	if policy.IsAllowed("mcp_memory_create") {
		t.Fatal("an MCP tool must not run under the default policy")
	}
	policy.DefaultAllow = true
	if policy.IsAllowed("mcp_memory_create") {
		t.Fatal("default_allow must not hand out MCP tools (extensions need explicit consent)")
	}
}

func newMCPTestIntegration(t *testing.T, policyJSON string) *NDIntegration {
	t.Helper()
	integration := &NDIntegration{
		stats: newBridgeStats(),
		stop:  newStopSwitch(),
		mcp:   newMCPManager(),
		relay: newRelayState(RelayConfig{}),
	}
	if policyJSON != "" {
		policy, err := loadPermissionPolicy(writeTempPolicy(t, policyJSON))
		if err != nil {
			t.Fatalf("policy: %v", err)
		}
		integration.setPolicy(policy)
	}
	return integration
}

// attachFakeServer puts a running fake server into the manager, the way a
// successful start does, and returns the handler for its echo tool.
func attachFakeServer(t *testing.T, integration *NDIntegration, id string) *mcpToolAction {
	t.Helper()
	dir := installFixture(t, fmt.Sprintf(`[{"id":%q,"name":"Fake MCP"}]`, id))
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", dir+"/extension-permissions.json")
	grantExtensionFixture(t, id, ExtensionPermissionPolicy{RunAllowed: true})
	client := startFakeMCP(t)
	integration.mcp.mu.Lock()
	integration.mcp.clients[id] = client
	integration.mcp.mu.Unlock()
	for _, handler := range integration.mcp.handlersFor(integration, id) {
		if action, ok := handler.(*mcpToolAction); ok && action.toolName == "echo" {
			return action
		}
	}
	t.Fatal("no echo handler was built")
	return nil
}

func TestMCPToolActionIsGatedByPolicyThenRuns(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"default_allow": false, "scopes": {"extensions": false}}`)
	action := attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })

	if action.GetName() != "mcp_memory_echo" {
		t.Fatalf("action name = %q", action.GetName())
	}
	if action.GetSchema() == nil || action.GetSchema().Type != "object" {
		t.Fatal("tool schema must be an object schema")
	}

	_, result := action.Validate(json.RawMessage(`{"text":"hi"}`))
	if result.Successful {
		t.Fatal("the call must be refused while the extensions scope is off")
	}
	if !strings.Contains(result.Message, "extensions") {
		t.Fatalf("the refusal should name the scope: %q", result.Message)
	}

	policy := integration.policy()
	policy.scopes[ScopeExtensions] = ScopeConfig{Allowed: true}

	client := integration.mcp.client("memory")
	before := atomic.LoadInt64(&client.nextID)
	state, result := action.Validate(json.RawMessage(`{"text":"hi"}`))
	if !result.Successful || !strings.Contains(result.Message, "Accepted") {
		t.Fatalf("with the scope on the call should be acknowledged: %+v", result)
	}
	job, ok := state.(*mcpCallJob)
	if !ok || atomic.LoadInt64(&client.nextID) != before {
		t.Fatal("validation must not call even a fast MCP tool")
	}
	action.Execute(job)
	if job.failed || job.text != "hi" || atomic.LoadInt64(&client.nextID) != before+1 {
		t.Fatalf("accepted tool did not execute exactly once: text=%q failed=%v", job.text, job.failed)
	}
	action.Execute(job)
	if atomic.LoadInt64(&client.nextID) != before+1 {
		t.Fatal("reusing a job replayed its tool call")
	}
}

func TestMCPToolActionRejectsNonObjectArguments(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes": {"extensions": true}}`)
	action := attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })

	if _, result := action.Validate(json.RawMessage(`["not", "an", "object"]`)); result.Successful {
		t.Fatal("arguments must be a JSON object")
	}
}

func TestMCPSlowCallDefersThenDelivers(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes": {"extensions": true}}`)
	attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })

	var slow *mcpToolAction
	for _, handler := range integration.mcp.handlersFor(integration, "memory") {
		if action, ok := handler.(*mcpToolAction); ok && action.toolName == "slow" {
			slow = action
		}
	}
	if slow == nil {
		t.Fatal("no slow tool")
	}

	client := integration.mcp.client("memory")
	before := atomic.LoadInt64(&client.nextID)
	state, result := slow.Validate(json.RawMessage(`{}`))
	if !result.Successful || !strings.Contains(result.Message, "result will follow") {
		t.Fatalf("a slow call should be acknowledged, got %+v", result)
	}
	job, ok := state.(*mcpCallJob)
	if !ok {
		t.Fatalf("a slow call should return its job, got %T", state)
	}
	if atomic.LoadInt64(&client.nextID) != before {
		t.Fatal("slow tool started during validation, before acknowledgement")
	}
	// Execute starts the job and then delivers it. With no client attached it
	// must still finish without panicking.
	done := make(chan struct{})
	go func() {
		slow.Execute(job)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not finish")
	}
	if job.failed || job.text != "finally" {
		t.Fatalf("job = %q failed=%v", job.text, job.failed)
	}
}

func TestMCPDeferredCallRechecksApprovalBeforeIO(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes":{"extensions":true}}`)
	action := attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })
	client := integration.mcp.client("memory")
	state, result := action.Validate(json.RawMessage(`{"text":"private result"}`))
	if !result.Successful {
		t.Fatal(result.Message)
	}
	before := atomic.LoadInt64(&client.nextID)
	policy, err := loadPermissionPolicy(writeTempPolicy(t, `{"scopes":{"extensions":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	integration.setPolicy(policy)
	job := state.(*mcpCallJob)
	action.Execute(job)
	if !job.failed || strings.Contains(job.text, "private result") || atomic.LoadInt64(&client.nextID) != before {
		t.Fatalf("revoked queued work ran or disclosed output: %q failed=%v", job.text, job.failed)
	}
}

func TestMCPTemporaryReapprovalCannotReusePendingCall(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes":{"extensions":{"allowed":false,"requestable":true}}}`)
	action := attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })
	policy := integration.policy()
	grant := ScopeGrant{Scope: ScopeExtensions, RequestID: "first", GrantedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	policy.grants.put(grant)
	state, result := action.Validate(json.RawMessage(`{"text":"stale"}`))
	if !result.Successful {
		t.Fatal(result.Message)
	}
	client := integration.mcp.client("memory")
	before := atomic.LoadInt64(&client.nextID)
	grant.RequestID = "replacement"
	policy.grants.put(grant)
	job := state.(*mcpCallJob)
	action.Execute(job)
	if !job.failed || atomic.LoadInt64(&client.nextID) != before {
		t.Fatal("replacement approval inherited a previously accepted tool call")
	}
}

func TestMCPDiscardsOutputRevokedDuringCall(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes":{"extensions":true}}`)
	attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })
	var action *mcpToolAction
	for _, handler := range integration.mcp.handlersFor(integration, "memory") {
		if candidate, ok := handler.(*mcpToolAction); ok && candidate.toolName == "slow" {
			action = candidate
		}
	}
	if action == nil {
		t.Fatal("slow fixture tool missing")
	}
	state, result := action.Validate(json.RawMessage(`{}`))
	if !result.Successful {
		t.Fatal(result.Message)
	}
	job := state.(*mcpCallJob)
	client := integration.mcp.client("memory")
	before := atomic.LoadInt64(&client.nextID)
	go job.run()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt64(&client.nextID) == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if atomic.LoadInt64(&client.nextID) == before {
		t.Fatal("fixture tool did not start")
	}
	policy, err := loadPermissionPolicy(writeTempPolicy(t, `{"scopes":{"extensions":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	integration.setPolicy(policy)
	<-job.done
	if !job.failed || strings.Contains(job.text, "finally") {
		t.Fatalf("revoked tool output survived: %q failed=%v", job.text, job.failed)
	}
}

func TestMCPStopSwitchBlocksToolCalls(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes": {"extensions": true}}`)
	action := attachFakeServer(t, integration, "memory")
	t.Cleanup(func() { integration.mcp.forget("memory") })

	integration.stop.setPaused(true)
	if _, result := action.Validate(json.RawMessage(`{"text":"x"}`)); result.Successful {
		t.Fatal("a paused bridge must refuse MCP calls")
	}
}

func TestMCPMissingServerIsAHelpfulRefusal(t *testing.T) {
	integration := newMCPTestIntegration(t, `{"scopes": {"extensions": true}}`)
	action := &mcpToolAction{integration: integration, serverID: "ghost", toolName: "x", name: "mcp_ghost_x"}
	_, result := action.Validate(json.RawMessage(`{}`))
	if result.Successful || !strings.Contains(result.Message, "not running") {
		t.Fatalf("result = %+v", result)
	}
}

func TestMCPSnapshotShowsStartingAndFailedServers(t *testing.T) {
	manager := newMCPManager()
	if !manager.markStarting("memory") {
		t.Fatal("first start should be allowed")
	}
	if manager.markStarting("memory") {
		t.Fatal("a second start of the same server must be refused")
	}
	manager.finishStart("memory", nil, fmt.Errorf("boom"))
	manager.markStarting("time")
	manager.finishStart("time", nil, nil) // a nil client with no error is not a running server

	snapshot := manager.snapshot()
	byID := map[string]mcpServerStatus{}
	for _, entry := range snapshot {
		byID[entry.ID] = entry
	}
	if byID["memory"].State != "failed" || !strings.Contains(byID["memory"].LastError, "boom") {
		t.Fatalf("memory = %+v", byID["memory"])
	}
	if byID["time"].State == "running" {
		t.Fatalf("time should not report running: %+v", byID["time"])
	}
}

func TestMCPCancelledStartIsDiscarded(t *testing.T) {
	manager := newMCPManager()
	manager.markStarting("memory")
	if !manager.cancelStart("memory") {
		t.Fatal("a start in flight must be cancellable")
	}
	if !manager.wasCancelled("memory") {
		t.Fatal("the cancelled start must be detected when it finishes")
	}
	if manager.wasCancelled("memory") {
		t.Fatal("the cancellation is consumed once")
	}
}

// compile-time check that the handler satisfies the SDK interface
var _ neuro.ActionHandler = (*mcpToolAction)(nil)
