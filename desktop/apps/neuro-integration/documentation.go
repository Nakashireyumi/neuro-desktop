package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func actionScriptDocCandidates() []string {
	return []string{
		filepath.Join("integration-docs", "Action Script Documentation.md"),
		filepath.Join("desktop", "apps", "neuro-integration", "integration-docs", "Action Script Documentation.md"),
	}
}

func loadFirstReadableFile(paths []string) (content string, path string, err error) {
	for _, candidate := range paths {
		data, readErr := os.ReadFile(candidate)
		if readErr == nil {
			return string(data), candidate, nil
		}

		if !errors.Is(readErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("failed to read %s: %w", candidate, readErr)
		}
	}

	return "", "", fmt.Errorf("file not found in expected paths: %v", paths)
}

func loadActionScriptDocumentation() (content string, path string, err error) {
	return loadFirstReadableFile(actionScriptDocCandidates())
}
