//go:build neurodev

package main

import (
	"os"
	"strings"
)

// extensionsDevBuild is true only in a build made with `-tags neurodev`, which
// the development bundle uses. A release build is compiled without the tag.
const extensionsDevBuild = true

// extensionAllowsUnsigned is the lab escape hatch: in a dev build,
// NEURO_EXTENSIONS_ALLOW_UNSIGNED=1 lets git_clone fetch items that have no
// verified signature. Each such install is logged.
func extensionAllowsUnsigned() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
