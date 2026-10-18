package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The catalog that ships in the repository must verify. CI runs this, so a
// hand-edit that breaks a signature, an unlisted publisher, or a stale hint
// fails the build instead of reaching a user.
func TestShippedCatalogVerifies(t *testing.T) {
	const indexPath = "../../catalog/index.json"
	const publishersPath = "../../catalog/publishers.json"

	index, err := loadCatalogIndex(indexPath)
	if err != nil {
		t.Fatalf("the shipped catalog does not load: %v", err)
	}
	publishers, err := loadPublishers(publishersPath)
	if err != nil {
		t.Fatalf("the shipped publishers file does not load: %v", err)
	}
	if len(index.Items) == 0 {
		t.Fatal("the shipped catalog has no items")
	}

	exeHint := regexp.MustCompile(`(?i)\.exe\b`)
	for _, item := range index.Items {
		trust := verifyCatalogItem(item, publishers)
		if trust.State != "verified" {
			t.Errorf("catalog item %q is %s (%s); sign it with `neuro-integration catalog sign`", item.ID, trust.State, trust.Detail)
		}
		if !normalizeExtensionIDLoose(item.ID) {
			t.Errorf("catalog item id %q is not a valid extension id", item.ID)
		}
		for _, hint := range item.LaunchHints {
			if exeHint.MatchString(hint) {
				t.Errorf("catalog item %q has a Windows-only launch hint: %q", item.ID, hint)
			}
		}
		if item.Repository != "" {
			if _, err := requireHTTPSRepo(item.Repository); err != nil {
				t.Errorf("catalog item %q: %v", item.ID, err)
			}
		}
		if item.MCP != nil {
			if err := item.MCP.validate(); err != nil {
				t.Errorf("catalog item %q has an invalid mcp block: %v", item.ID, err)
			}
			if !mcpServerIDPattern.MatchString(item.ID) {
				t.Errorf("mcp item id %q must match %s so its tools get clean action names", item.ID, mcpServerIDPattern)
			}
			if !strings.Contains(strings.Join(item.MCP.Args, " "), "@") {
				t.Errorf("mcp item %q should pin its package version with @<version>", item.ID)
			}
		}
	}
}

// The index file must be valid JSON with the documented top-level shape.
func TestShippedCatalogIsWellFormedJSON(t *testing.T) {
	data, err := os.ReadFile("../../catalog/index.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
}

func normalizeExtensionIDLoose(id string) bool {
	_, err := normalizeExtensionID(id)
	return err == nil
}
