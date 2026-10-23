package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFirstReadableFileReturnsFirstExistingFile(t *testing.T) {
	tempDir := t.TempDir()
	missingPath := filepath.Join(tempDir, "missing.md")
	existingPath := filepath.Join(tempDir, "Action Script Documentation.md")

	expected := "doc-content"
	if err := os.WriteFile(existingPath, []byte(expected), 0644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	content, resolvedPath, err := loadFirstReadableFile([]string{missingPath, existingPath})
	if err != nil {
		t.Fatalf("expected file to be loaded, got error: %v", err)
	}

	if content != expected {
		t.Fatalf("unexpected content: got %q, want %q", content, expected)
	}

	if resolvedPath != existingPath {
		t.Fatalf("unexpected path: got %q, want %q", resolvedPath, existingPath)
	}
}

func TestLoadFirstReadableFileReturnsErrorWhenAllMissing(t *testing.T) {
	tempDir := t.TempDir()
	paths := []string{
		filepath.Join(tempDir, "missing-one.md"),
		filepath.Join(tempDir, "missing-two.md"),
	}

	_, _, err := loadFirstReadableFile(paths)
	if err == nil {
		t.Fatal("expected error when no files exist")
	}

	if !strings.Contains(err.Error(), "file not found in expected paths") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
