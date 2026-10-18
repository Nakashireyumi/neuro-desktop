//go:build !neurodev

package main

import (
	"strings"
	"testing"
)

// A release build has no unsigned escape hatch. Setting the variable changes
// nothing, and an unsigned item is refused with the variable set.
func TestReleaseBuildIgnoresUnsignedEscapeHatch(t *testing.T) {
	if extensionsDevBuild {
		t.Fatal("a build without the neurodev tag must not set extensionsDevBuild")
	}
	for _, value := range []string{"1", "true", "yes", "on"} {
		t.Setenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED", value)
		if extensionAllowsUnsigned() {
			t.Fatalf("a release build must ignore NEURO_EXTENSIONS_ALLOW_UNSIGNED=%s", value)
		}
	}
}

func TestReleaseBuildStillRefusesUnsignedInstalls(t *testing.T) {
	installFixture(t, `[{"id":"mcp-bridge","name":"MCP","description":"x","repository":"https://example.com/m","commit":"`+commitA+`"}]`)
	setEnv(t, map[string]string{
		"NEURO_EXTENSION_INSTALL_MODE":    "git_clone",
		"NEURO_EXTENSIONS_ALLOW_UNSIGNED": "1",
	})

	result := (&NDIntegration{}).installExtension("mcp-bridge")
	if result.Successful || !strings.Contains(result.Message, "unsigned") {
		t.Fatalf("a release build must refuse an unsigned item even with the variable set, got: %+v", result)
	}
}
