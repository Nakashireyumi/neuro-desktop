package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

type ExtensionStateFile struct {
	Installed map[string]ExtensionInstallState `json:"installed"`
}

type ExtensionInstallState struct {
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at"`
	Source      string `json:"source,omitempty"`
	Path        string `json:"path,omitempty"`
}

func extensionStatePath() string {
	path := strings.TrimSpace(os.Getenv("NEURO_EXTENSIONS_STATE_FILE"))
	if path != "" {
		return path
	}
	return "./catalog/extensions-state.json"
}

func extensionInstallMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("NEURO_EXTENSION_INSTALL_MODE")))
	if mode == "" {
		return "metadata_only"
	}
	return mode
}

func extensionRootPath() string {
	root := strings.TrimSpace(os.Getenv("NEURO_EXTENSION_DIR"))
	if root != "" {
		return root
	}
	return "./plugins"
}

func loadExtensionState(path string) (ExtensionStateFile, error) {
	state := ExtensionStateFile{
		Installed: map[string]ExtensionInstallState{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return ExtensionStateFile{}, fmt.Errorf("failed to read extension state file: %w", err)
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return ExtensionStateFile{}, fmt.Errorf("failed to parse extension state file: %w", err)
	}

	if state.Installed == nil {
		state.Installed = map[string]ExtensionInstallState{}
	}
	return state, nil
}

func saveExtensionState(path string, state ExtensionStateFile) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create extension state directory: %w", err)
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize extension state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write extension state file: %w", err)
	}

	return nil
}

func sanitizeExtensionID(itemID string) string {
	normalized := strings.ToLower(strings.TrimSpace(itemID))
	normalized = strings.ReplaceAll(normalized, "..", "")
	normalized = strings.ReplaceAll(normalized, "/", "-")
	normalized = strings.ReplaceAll(normalized, "\\", "-")
	return normalized
}

func findCatalogItemByID(index CatalogIndex, itemID string) *CatalogItem {
	target := strings.ToLower(strings.TrimSpace(itemID))
	if target == "" {
		return nil
	}

	for i := range index.Items {
		if strings.ToLower(strings.TrimSpace(index.Items[i].ID)) == target {
			return &index.Items[i]
		}
	}

	return nil
}

func (n *NDIntegration) listInstalledExtensions() neuro.ExecutionResult {
	state, err := loadExtensionState(extensionStatePath())
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	if len(state.Installed) == 0 {
		return neuro.NewSuccessResult("No installed extensions")
	}

	lines := make([]string, 0, len(state.Installed))
	for itemID, install := range state.Installed {
		status := "disabled"
		if install.Enabled {
			status = "enabled"
		}
		line := fmt.Sprintf("%s (%s)", itemID, status)
		if install.Source != "" {
			line += " source=" + install.Source
		}
		lines = append(lines, line)
	}

	return neuro.NewSuccessResult("Installed extensions -> " + strings.Join(lines, " | "))
}

func (n *NDIntegration) installExtension(itemID string) neuro.ExecutionResult {
	catalog, err := loadCatalogIndex(catalogFilePath())
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	item := findCatalogItemByID(catalog, itemID)
	if item == nil {
		return neuro.NewFailureResult("Catalog extension not found")
	}

	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	safeID := sanitizeExtensionID(item.ID)
	mode := extensionInstallMode()
	source := item.Repository
	installPath := ""

	if mode == "git_clone" {
		if strings.TrimSpace(item.Repository) == "" {
			return neuro.NewFailureResult("Extension has no repository URL in catalog")
		}

		root := extensionRootPath()
		installPath = filepath.Join(root, safeID)

		if err := os.MkdirAll(root, 0755); err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to create extension directory: %v", err))
		}

		if _, statErr := os.Stat(installPath); os.IsNotExist(statErr) {
			cmd := exec.Command("git", "clone", "--depth", "1", item.Repository, installPath)
			output, cmdErr := cmd.CombinedOutput()
			if cmdErr != nil {
				return neuro.NewFailureResult(fmt.Sprintf("git clone failed: %v (%s)", cmdErr, string(output)))
			}
		}
	}

	if source == "" {
		source = "catalog"
	}

	state.Installed[safeID] = ExtensionInstallState{
		Enabled:     true,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      source,
		Path:        installPath,
	}

	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	return neuro.NewSuccessResult(fmt.Sprintf("Installed extension %s in mode %s", safeID, mode))
}

func (n *NDIntegration) uninstallExtension(itemID string) neuro.ExecutionResult {
	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	safeID := sanitizeExtensionID(itemID)
	install, exists := state.Installed[safeID]
	if !exists {
		return neuro.NewFailureResult("Extension is not installed")
	}

	if install.Path != "" {
		absInstallPath, err := filepath.Abs(install.Path)
		if err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to resolve extension path: %v", err))
		}

		absRoot, err := filepath.Abs(extensionRootPath())
		if err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to resolve extension root: %v", err))
		}

		if !strings.HasPrefix(absInstallPath, absRoot) {
			return neuro.NewFailureResult("Refusing to remove extension path outside extension root")
		}

		if err := os.RemoveAll(absInstallPath); err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to remove extension files: %v", err))
		}
	}

	delete(state.Installed, safeID)
	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	return neuro.NewSuccessResult(fmt.Sprintf("Uninstalled extension %s", safeID))
}

func (n *NDIntegration) setExtensionEnabled(itemID string, enabled bool) neuro.ExecutionResult {
	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	safeID := sanitizeExtensionID(itemID)
	install, exists := state.Installed[safeID]
	if !exists {
		return neuro.NewFailureResult("Extension is not installed")
	}

	install.Enabled = enabled
	state.Installed[safeID] = install

	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	status := "disabled"
	if enabled {
		status = "enabled"
	}
	return neuro.NewSuccessResult(fmt.Sprintf("Extension %s is now %s", safeID, status))
}
