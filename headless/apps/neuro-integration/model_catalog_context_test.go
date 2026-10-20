package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestModelCatalogListsKeepIDsStateAndBoundObservedText(t *testing.T) {
	items := make([]CatalogItem, 30)
	for i := range items {
		items[i] = CatalogItem{ID: fmt.Sprintf("item-%02d", i), Name: "Example\n## fake heading",
			Description: strings.Repeat("界", 1000), Repository: "https://example.test/source"}
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	dir := installFixture(t, string(data))
	t.Setenv("NEURO_EXTENSION_PERMISSIONS_FILE", filepath.Join(dir, "extension-permissions.json"))
	n := newMCPTestIntegration(t, `{"scopes":{"extensions":true}}`)
	grantExtensionFixture(t, "item-00", ExtensionPermissionPolicy{ManageAllowed: true})
	state := ExtensionStateFile{Installed: map[string]ExtensionInstallState{
		"item-00": {Enabled: true, Source: "/private/source", Commit: strings.Repeat("a", 40)},
	}}
	if err := saveExtensionState(extensionStatePath(), state); err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{n.listCatalogItems().Message, n.findCatalogItems("item", 20).Message, n.getCatalogItem("item-00").Message} {
		if len(result) > contextMessageMaxBytes || !utf8.ValidString(result) {
			t.Fatalf("catalog context exceeded its UTF-8 budget: %d bytes", len(result))
		}
		if !strings.Contains(result, "item-00") || strings.Contains(result, "\n## fake heading") {
			t.Fatalf("missing usable ID or injected metadata row: %s", result)
		}
	}
	listed := n.listCatalogItems().Message
	if !strings.Contains(listed, "installed, enabled; management delegated") || strings.Contains(listed, "item-06") {
		t.Fatalf("list omitted approval/state or its result limit: %s", listed)
	}
	installed := n.listInstalledExtensions().Message
	if strings.Contains(installed, "/private/source") || strings.Contains(installed, strings.Repeat("a", 40)) {
		t.Fatalf("installed listing included unnecessary source details: %s", installed)
	}
}

func TestModelMarketplaceSummariesKeepSetupAndLicenseDecisions(t *testing.T) {
	items := make([]map[string]interface{}, 20)
	for i := range items {
		status := "standard"
		if i == 0 {
			status = "custom"
		}
		items[i] = map[string]interface{}{
			"id": fmt.Sprintf("integration-%02d", i), "name": "Game\n## injected",
			"description": strings.Repeat("説明", 500), "repository": "https://example.test/owner/repo",
			"source_commit": strings.Repeat("a", 40), "license": map[string]interface{}{"status": status},
		}
	}
	data, err := json.Marshal(map[string]interface{}{"schema_version": 1, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEURO_MARKETPLACE_FILE", path)
	result := (&NDIntegration{}).marketplaceSearch("")
	if !result.Successful || len(result.Message) > contextMessageMaxBytes || !utf8.ValidString(result.Message) {
		t.Fatalf("invalid marketplace summary: %+v", result)
	}
	for _, required := range []string{"integration-00", "integration-01", "link-only, license review needed", "management approval", "manual setup"} {
		if !strings.Contains(result.Message, required) {
			t.Fatalf("missing decision %q: %s", required, result.Message)
		}
	}
	for _, unnecessary := range []string{strings.Repeat("a", 40), "https://example.test", "integration-06", "\n## injected"} {
		if strings.Contains(result.Message, unnecessary) {
			t.Fatalf("summary contained %q: %s", unnecessary, result.Message)
		}
	}
}
