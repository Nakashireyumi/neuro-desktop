//go:build !neurodev

package main

import (
	"log"
	"os"
	"strings"
	"sync"
)

// extensionsDevBuild is false in a release build: the unsigned escape hatch is
// not compiled in.
const extensionsDevBuild = false

var unsignedIgnoredOnce sync.Once

// extensionAllowsUnsigned always answers false in a release build. If
// NEURO_EXTENSIONS_ALLOW_UNSIGNED is set anyway, the server says once that the
// variable is ignored, so an operator who set it does not wonder why nothing
// installs.
func extensionAllowsUnsigned() bool {
	if strings.TrimSpace(os.Getenv("NEURO_EXTENSIONS_ALLOW_UNSIGNED")) != "" {
		unsignedIgnoredOnce.Do(func() {
			log.Printf("NEURO_EXTENSIONS_ALLOW_UNSIGNED is ignored: only a development build (-tags neurodev) accepts unsigned extensions")
		})
	}
	return false
}
