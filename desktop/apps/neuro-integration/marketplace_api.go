package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

type marketplaceItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Repository   string   `json:"repository"`
	SourceCommit string   `json:"source_commit"`
	GameIDs      []string `json:"game_ids"`
	License      struct {
		Status string `json:"status"`
		SPDX   string `json:"spdx"`
	} `json:"license"`
	Permissions struct {
		Capabilities []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"capabilities"`
	} `json:"permissions"`
	Settings []struct {
		Key         string `json:"key"`
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"settings"`
	Raw json.RawMessage `json:"-"`
}
type marketplaceIndex struct {
	SchemaVersion int               `json:"schema_version"`
	Items         []marketplaceItem `json:"items"`
}

var marketplaceStageMu sync.Mutex

func marketplacePath() string {
	if path := strings.TrimSpace(os.Getenv("NEURO_MARKETPLACE_FILE")); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(catalogFilePath()), "marketplace.json")
}
func loadMarketplace() (marketplaceIndex, error) {
	var index marketplaceIndex
	data, err := os.ReadFile(marketplacePath())
	if os.IsNotExist(err) {
		return marketplaceIndex{SchemaVersion: 1, Items: []marketplaceItem{}}, nil
	}
	if err != nil {
		return index, err
	}
	if len(data) > 2<<20 {
		return index, errors.New("marketplace exceeds 2 MiB")
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, errors.New("invalid marketplace JSON")
	}
	if index.SchemaVersion != 1 || len(index.Items) > 500 {
		return index, errors.New("unsupported marketplace format")
	}
	var raw struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(data, &raw)
	seen := map[string]bool{}
	for i, item := range index.Items {
		if !extensionIDPattern.MatchString(item.ID) || seen[item.ID] || !gitCommitPattern.MatchString(item.SourceCommit) {
			return index, errors.New("marketplace items need unique ids and pinned source commits")
		}
		if _, err := requireHTTPSRepo(item.Repository); err != nil {
			return index, err
		}
		seen[item.ID] = true
		index.Items[i].Raw = raw.Items[i]
	}
	return index, nil
}
func marketplaceByID(id string) (marketplaceItem, error) {
	index, err := loadMarketplace()
	if err != nil {
		return marketplaceItem{}, err
	}
	for _, item := range index.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return marketplaceItem{}, errors.New("marketplace item not found")
}
func marketplaceDeclarations(id string) ([]ExtensionPermissionDeclaration, map[string]interface{}, error) {
	item, err := marketplaceByID(id)
	if err != nil {
		return nil, nil, err
	}
	decls := []ExtensionPermissionDeclaration{}
	seen := map[string]bool{}
	for _, cap := range item.Permissions.Capabilities {
		base := strings.SplitN(cap.ID, ".", 2)[0]
		switch base {
		case "docker", "tools":
			base = "system"
		case "editor":
			base = "filesystem"
		}
		if !extensionCapabilityIDs[base] {
			return nil, nil, fmt.Errorf("unmapped capability %s", cap.ID)
		}
		if !seen[base] {
			decls = append(decls, ExtensionPermissionDeclaration{ID: base, Description: cap.Reason, Required: true})
			seen[base] = true
		}
	}
	props := map[string]interface{}{}
	for _, setting := range item.Settings {
		props[setting.Key] = map[string]interface{}{"type": setting.Type, "description": setting.Description}
	}
	return decls, map[string]interface{}{"type": "object", "properties": props, "additionalProperties": false}, nil
}
func (a *AdminServer) handleMarketplace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	index, err := loadMarketplace()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	items := []map[string]interface{}{}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	for _, item := range index.Items {
		if query != "" && !strings.Contains(strings.ToLower(item.ID+" "+item.Name+" "+item.Description+" "+strings.Join(item.GameIDs, " ")), query) {
			continue
		}
		view := map[string]interface{}{}
		_ = json.Unmarshal(item.Raw, &view)
		view["source_staged"] = stagedMarketplacePath(item) != ""
		view["can_stage_source"] = item.License.Status == "standard"
		items = append(items, view)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "items": items, "schema_version": 1, "path": marketplacePath(), "integrations": a.integration.adapterSnapshot()})
}
func marketplaceSourceRoot() string { return filepath.Join(extensionRootPath(), "sources") }
func stagedMarketplacePath(item marketplaceItem) string {
	dest := filepath.Join(marketplaceSourceRoot(), item.ID, item.SourceCommit)
	marker, err := os.ReadFile(filepath.Join(dest, ".nd-source-commit"))
	if err == nil && strings.TrimSpace(string(marker)) == item.SourceCommit {
		return dest
	}
	return ""
}
func (n *NDIntegration) stageMarketplaceSource(itemID string) neuro.ExecutionResult {
	id, err := normalizeExtensionID(itemID)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	if reason := n.stop.blockReason("stage_integration_source"); reason != "" {
		return neuro.NewFailureResult(reason)
	}
	if err := n.checkExtensionManagement(id); err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	item, err := marketplaceByID(id)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	if item.License.Status != "standard" {
		return neuro.NewFailureResult("Source staging is unavailable until this integration's license is reviewed. Open its upstream instructions.")
	}
	marketplaceStageMu.Lock()
	defer marketplaceStageMu.Unlock()
	if path := stagedMarketplacePath(item); path != "" {
		return neuro.NewSuccessResult("Pinned source already downloaded to " + path + ". Game/mod setup is still required; no integration has been started.")
	}
	root := marketplaceSourceRoot()
	dest := filepath.Join(root, id, item.SourceCommit)
	if !pathInsideRoot(root, dest) {
		return neuro.NewFailureResult("invalid source directory")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	if err := fetchPinnedCommit(item.Repository, item.SourceCommit, dest); err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	if err := atomicWriteFile(filepath.Join(dest, ".nd-source-commit"), []byte(item.SourceCommit+"\n")); err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	n.audit.record("marketplace_source", map[string]interface{}{"item": id, "commit": item.SourceCommit, "decision": "staged"})
	return neuro.NewSuccessResult("Pinned source downloaded to " + dest + ". Follow its game/mod setup instructions; no install scripts were executed and no integration has been started.")
}
func (a *AdminServer) handleMarketplaceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/marketplace/"), "/")
	if len(parts) != 2 || parts[1] != "stage" {
		writeError(w, 400, "expected /api/marketplace/{id}/stage")
		return
	}
	result := a.integration.stageMarketplaceSource(parts[0])
	if !result.Successful {
		writeError(w, 409, result.Message)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": result.Message})
}
func (n *NDIntegration) marketplaceSearch(query string) neuro.ExecutionResult {
	index, err := loadMarketplace()
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	lines := []string{}
	for _, item := range index.Items {
		if needle == "" || strings.Contains(strings.ToLower(item.ID+" "+item.Name+" "+item.Description+" "+strings.Join(item.GameIDs, " ")), needle) {
			lines = append(lines, fmt.Sprintf("%s (%s): %s. Setup required; source %s at %s.", item.ID, item.Name, item.Description, item.Repository, item.SourceCommit))
			if len(lines) >= 12 {
				break
			}
		}
	}
	if len(lines) == 0 {
		return neuro.NewSuccessResult("No marketplace listings match. General desktop controls remain available.")
	}
	return neuro.NewSuccessResult(strings.Join(lines, "\n"))
}
