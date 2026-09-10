package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListCatalogItemsSuccess(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "index.json")

	content := `{
  "items": [
    {"id":"a","name":"Alpha","description":"First","tags":["desktop"]},
    {"id":"b","name":"Beta","description":"Second","tags":["relay"]}
  ]
}`

	if err := os.WriteFile(catalogPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog fixture: %v", err)
	}

	if err := os.Setenv("NEURO_CATALOG_FILE", catalogPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("NEURO_CATALOG_FILE")
	})

	result := (&NDIntegration{}).listCatalogItems()
	if !result.Successful {
		t.Fatalf("expected success, got failure: %s", result.Message)
	}

	if !strings.Contains(result.Message, "Alpha: First") {
		t.Fatalf("expected catalog summary in result, got: %s", result.Message)
	}
}

func TestFindCatalogItemsByTag(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "index.json")

	content := `{
  "items": [
    {"id":"neuro-desktop","name":"Neuro Desktop","description":"Windows integration","tags":["windows","desktop"]},
    {"id":"neuro-relay","name":"Neuro Relay","description":"Multiplexer","tags":["relay","backend"]}
  ]
}`

	if err := os.WriteFile(catalogPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog fixture: %v", err)
	}

	if err := os.Setenv("NEURO_CATALOG_FILE", catalogPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("NEURO_CATALOG_FILE")
	})

	result := (&NDIntegration{}).findCatalogItems("relay", 5)
	if !result.Successful {
		t.Fatalf("expected success, got failure: %s", result.Message)
	}

	if !strings.Contains(result.Message, "neuro-relay (Neuro Relay)") {
		t.Fatalf("unexpected search output: %s", result.Message)
	}
}

func TestGetCatalogItem(t *testing.T) {
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "index.json")

	content := `{
  "items": [
    {
      "id":"neuro-relay",
      "name":"Neuro Relay",
      "description":"Multiplexing relay",
      "type":"relay",
      "repository":"https://github.com/recassity/neuro-relay",
      "tags":["relay","backend"],
      "launch_hints":["set NEURO_RELAY_ENABLED=true"]
    }
  ]
}`

	if err := os.WriteFile(catalogPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog fixture: %v", err)
	}

	if err := os.Setenv("NEURO_CATALOG_FILE", catalogPath); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("NEURO_CATALOG_FILE")
	})

	result := (&NDIntegration{}).getCatalogItem("neuro-relay")
	if !result.Successful {
		t.Fatalf("expected success, got failure: %s", result.Message)
	}

	if !strings.Contains(result.Message, "type=relay") {
		t.Fatalf("expected type in result message, got: %s", result.Message)
	}
}

func TestListCatalogItemsMissingFile(t *testing.T) {
	if err := os.Setenv("NEURO_CATALOG_FILE", "./definitely-missing-file.json"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("NEURO_CATALOG_FILE")
	})

	result := (&NDIntegration{}).listCatalogItems()
	if result.Successful {
		t.Fatal("expected failure for missing catalog file")
	}
}
