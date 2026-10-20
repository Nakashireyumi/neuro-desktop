package main

import (
	"os"
	"strings"
	"testing"
)

// TestMCPRealReferenceServer runs the published MCP reference memory server
// through npx. It needs Node and network access, so it only runs when
// NDTEST_REAL_MCP=1 (CI sets it in the mcp job).
func TestMCPRealReferenceServer(t *testing.T) {
	if os.Getenv("NDTEST_REAL_MCP") != "1" {
		t.Skip("set NDTEST_REAL_MCP=1 to run against the real npm MCP server")
	}
	spec := CatalogMCP{
		Command:        "npx",
		Args:           []string{"-y", "@modelcontextprotocol/server-memory@2026.8.31"},
		TimeoutSeconds: 60,
	}
	client, err := startMCPClient("memory", spec, "verified")
	if err != nil {
		t.Fatalf("the reference server did not start: %v", err)
	}
	defer client.stop()

	_, _, tools, _ := client.status()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"create_entities", "read_graph"} {
		if !names[want] {
			t.Fatalf("tool %s missing; got %v", want, names)
		}
	}

	if _, isError, err := client.callTool("create_entities", map[string]interface{}{
		"entities": []interface{}{map[string]interface{}{
			"name": "Vedal", "entityType": "person", "observations": []interface{}{"runs the stream"},
		}},
	}); err != nil || isError {
		t.Fatalf("create_entities failed: err=%v isError=%v", err, isError)
	}

	text, isError, err := client.callTool("read_graph", map[string]interface{}{})
	if err != nil || isError {
		t.Fatalf("read_graph failed: err=%v isError=%v", err, isError)
	}
	if !strings.Contains(text, "Vedal") {
		t.Fatalf("the created entity is not in the graph: %s", text)
	}
}
