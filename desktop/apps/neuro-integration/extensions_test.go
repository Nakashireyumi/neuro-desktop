package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionInstallEnableDisableUninstallMetadataOnly(t *testing.T) {
	tempDir := t.TempDir()

	catalogPath := filepath.Join(tempDir, "catalog.json")
	statePath := filepath.Join(tempDir, "state.json")

	content := `{
  "items": [
    {
      "id":"mcp-bridge",
      "name":"MCP Bridge",
      "description":"Model Context Protocol bridge",
      "repository":"https://github.com/modelcontextprotocol/servers"
    }
  ]
}`

	if err := os.WriteFile(catalogPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog fixture: %v", err)
	}

	if err := os.Setenv("NEURO_CATALOG_FILE", catalogPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("NEURO_EXTENSIONS_STATE_FILE", statePath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("NEURO_EXTENSION_INSTALL_MODE", "metadata_only"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("NEURO_CATALOG_FILE")
		_ = os.Unsetenv("NEURO_EXTENSIONS_STATE_FILE")
		_ = os.Unsetenv("NEURO_EXTENSION_INSTALL_MODE")
	})

	integration := &NDIntegration{}

	installResult := integration.installExtension("mcp-bridge")
	if !installResult.Successful {
		t.Fatalf("expected install success, got: %s", installResult.Message)
	}

	listResult := integration.listInstalledExtensions()
	if !listResult.Successful {
		t.Fatalf("expected list success, got: %s", listResult.Message)
	}
	if !strings.Contains(listResult.Message, "mcp-bridge (enabled)") {
		t.Fatalf("unexpected list output: %s", listResult.Message)
	}

	disableResult := integration.setExtensionEnabled("mcp-bridge", false)
	if !disableResult.Successful {
		t.Fatalf("expected disable success, got: %s", disableResult.Message)
	}

	enableResult := integration.setExtensionEnabled("mcp-bridge", true)
	if !enableResult.Successful {
		t.Fatalf("expected enable success, got: %s", enableResult.Message)
	}

	uninstallResult := integration.uninstallExtension("mcp-bridge")
	if !uninstallResult.Successful {
		t.Fatalf("expected uninstall success, got: %s", uninstallResult.Message)
	}
}
