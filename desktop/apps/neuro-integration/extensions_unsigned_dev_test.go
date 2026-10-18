//go:build neurodev

package main

import "testing"

// In a dev build the unsigned escape hatch is compiled in and follows the variable.
func TestDevBuildHonoursUnsignedEscapeHatch(t *testing.T) {
	if !extensionsDevBuild {
		t.Fatal("the neurodev tag must set extensionsDevBuild")
	}
	t.Setenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED", "1")
	if !extensionAllowsUnsigned() {
		t.Fatal("a dev build must honour NEURO_EXTENSIONS_ALLOW_UNSIGNED=1")
	}
	t.Setenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED", "")
	if extensionAllowsUnsigned() {
		t.Fatal("an empty variable must not enable the escape hatch")
	}
}
