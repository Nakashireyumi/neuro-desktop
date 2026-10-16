package main

// Extension policy is operator-owned. A model can use delegated management
// actions, but no model action writes this file or receives the operator token.
// These grants gate managed starts and calls; they are not an OS process sandbox.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type ExtensionPermissionDeclaration struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type ExtensionPermissionPolicy struct {
	ManageAllowed bool                   `json:"manage_allowed"`
	RunAllowed    bool                   `json:"run_allowed"`
	Grants        map[string]bool        `json:"grants"`
	Settings      map[string]interface{} `json:"settings"`
}

type extensionPermissionFile struct {
	Version    int                                  `json:"version"`
	Extensions map[string]ExtensionPermissionPolicy `json:"extensions"`
}

var extensionCapabilityIDs = map[string]bool{
	"input": true, "game": true, "vision": true, "shell": true,
	"filesystem": true, "process": true, "network": true, "system": true,
	"extensions": true, "discord.viewer": true,
}

func extensionPermissionsPath() string {
	if path := strings.TrimSpace(os.Getenv("NEURO_EXTENSION_PERMISSIONS_FILE")); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(extensionStatePath()), "extension-permissions.json")
}

func emptyExtensionPermissionPolicy() ExtensionPermissionPolicy {
	return ExtensionPermissionPolicy{Grants: map[string]bool{}, Settings: map[string]interface{}{}}
}

func loadExtensionPermissions(path string) (extensionPermissionFile, error) {
	file := extensionPermissionFile{Version: 1, Extensions: map[string]ExtensionPermissionPolicy{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("cannot read extension permissions: %w", err)
	}
	if len(data) > 1024*1024 {
		return file, errors.New("extension permissions file exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, &file); err != nil || file.Version != 1 || file.Extensions == nil {
		return extensionPermissionFile{}, errors.New("extension permissions file is invalid or has an unsupported version")
	}
	return file, nil
}

func saveExtensionPermissions(path string, file extensionPermissionFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("extension permissions file exceeds 1 MiB")
	}
	return atomicWriteFile(path, append(data, '\n'))
}

func validateExtensionDeclarations(declarations []ExtensionPermissionDeclaration) error {
	if len(declarations) > len(extensionCapabilityIDs) {
		return errors.New("too many extension permission declarations")
	}
	seen := map[string]bool{}
	for _, declaration := range declarations {
		if !extensionCapabilityIDs[declaration.ID] || seen[declaration.ID] {
			return fmt.Errorf("unknown or repeated extension capability %q", declaration.ID)
		}
		if strings.TrimSpace(declaration.Description) == "" || !cleanActivityText(declaration.Description, 1000) {
			return fmt.Errorf("capability %s needs a description of at most 1000 characters without control characters", declaration.ID)
		}
		seen[declaration.ID] = true
	}
	return nil
}

// extensionPermissionSnapshot provides a narrowly scoped migration for the
// previously shipped, already-enabled signed Memory extension. It never grants
// management, applies to no new install, and stops applying once declarations or
// an explicit policy exist. Explicit revocation therefore always wins.
func (n *NDIntegration) extensionPermissionSnapshot(id string) (ExtensionPermissionPolicy, bool, error) {
	n.extensionPermissionsMu.RLock()
	file, err := loadExtensionPermissions(extensionPermissionsPath())
	n.extensionPermissionsMu.RUnlock()
	if err != nil {
		return emptyExtensionPermissionPolicy(), false, err
	}
	if policy, ok := file.Extensions[id]; ok {
		if policy.Grants == nil {
			policy.Grants = map[string]bool{}
		}
		if policy.Settings == nil {
			policy.Settings = map[string]interface{}{}
		}
		return policy, false, nil
	}
	policy := emptyExtensionPermissionPolicy()
	if id != "memory" {
		return policy, false, nil
	}
	state, err := loadExtensionState(extensionStatePath())
	if err != nil || !state.Installed[id].Enabled {
		return policy, false, nil
	}
	index, err := loadCatalogIndex(catalogFilePath())
	if err != nil {
		return policy, false, nil
	}
	item := findCatalogItemByID(index, id)
	if item != nil && item.MCP != nil && len(item.Permissions) == 0 && verifyCatalogItem(*item, loadPublishersOrEmpty()).State == "verified" {
		policy.RunAllowed = true
		return policy, true, nil
	}
	return policy, false, nil
}

func (n *NDIntegration) checkExtensionManagement(id string) error {
	if _, err := normalizeExtensionID(id); err != nil {
		return err
	}
	if policy := n.policy(); policy == nil || !policy.scopeOn(ScopeExtensions) {
		return errors.New("extension management needs the global extensions permission")
	}
	policy, _, err := n.extensionPermissionSnapshot(id)
	if err != nil {
		return err
	}
	if !policy.ManageAllowed {
		return fmt.Errorf("management of %s has not been delegated; ask for per-extension management permission", id)
	}
	return nil
}

func (n *NDIntegration) extensionRuntimeAllowed(id string, declarations []ExtensionPermissionDeclaration) error {
	if _, err := normalizeExtensionID(id); err != nil {
		return err
	}
	if err := validateExtensionDeclarations(declarations); err != nil {
		return err
	}
	globalPolicy := n.policy()
	if globalPolicy == nil || !globalPolicy.scopeOn(ScopeExtensions) {
		return errors.New("running extensions needs the global extensions permission")
	}
	policy, _, err := n.extensionPermissionSnapshot(id)
	if err != nil {
		return err
	}
	if !policy.RunAllowed {
		return fmt.Errorf("%s has no permission to run; approve it in the manager's extension permissions", id)
	}
	for _, declaration := range declarations {
		if declaration.Required && !policy.Grants[declaration.ID] {
			return fmt.Errorf("%s needs an explicit %s grant", id, declaration.ID)
		}
		if declaration.Required {
			scope := PermissionScope(declaration.ID)
			if _, known := scopeDescriptions[scope]; known {
				allowed := globalPolicy.ScopeAllowed(scope)
				if scopeRequiresExplicitConsent(scope) {
					allowed = globalPolicy.scopeOn(scope)
				}
				if !allowed {
					return fmt.Errorf("%s also needs the global %s permission", id, declaration.ID)
				}
			}
		}
	}
	return nil
}

func (n *NDIntegration) extensionItemRuntimeAllowed(id string) error {
	declarations, _, err := n.extensionDeclarations(id)
	if err != nil {
		return err
	}
	return n.extensionRuntimeAllowed(id, declarations)
}

func (n *NDIntegration) extensionDeclarations(id string) ([]ExtensionPermissionDeclaration, map[string]interface{}, error) {
	index, err := loadCatalogIndex(catalogFilePath())
	if err == nil {
		if item := findCatalogItemByID(index, id); item != nil {
			return item.Permissions, item.SettingsSchema, nil
		}
	}
	if declarations, schema, err := n.adapterDeclarations(id); err == nil {
		return declarations, schema, nil
	}
	return marketplaceDeclarations(id)
}

func validateExtensionPolicy(item CatalogItem, policy ExtensionPermissionPolicy) error {
	if err := validateExtensionDeclarations(item.Permissions); err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, declaration := range item.Permissions {
		declared[declaration.ID] = true
	}
	for id, granted := range policy.Grants {
		if !declared[id] {
			return fmt.Errorf("capability %q is not declared by this extension", id)
		}
		if granted && !extensionCapabilityIDs[id] {
			return fmt.Errorf("capability %q is not supported", id)
		}
	}
	if err := validateExtensionSettings(policy.Settings, item.SettingsSchema, 0); err != nil {
		return fmt.Errorf("invalid extension settings: %w", err)
	}
	return nil
}

// validateExtensionSettings implements the small settings-schema subset exposed
// by the SDK: types, enum, required, properties, additionalProperties, items,
// numeric bounds and string length. Values are JSON data, never executable code.
func validateExtensionSettings(value interface{}, schema map[string]interface{}, depth int) error {
	if depth > 12 {
		return errors.New("settings are nested too deeply")
	}
	if choices, ok := schema["enum"].([]interface{}); ok {
		found := false
		for _, choice := range choices {
			found = found || reflect.DeepEqual(choice, value)
		}
		if !found {
			return errors.New("value is not in the declared enum")
		}
	}
	typeName, _ := schema["type"].(string)
	typeOK := typeName == ""
	switch typed := value.(type) {
	case map[string]interface{}:
		typeOK = typeOK || typeName == "object"
		properties, _ := schema["properties"].(map[string]interface{})
		if required, ok := schema["required"].([]interface{}); ok {
			for _, raw := range required {
				key, _ := raw.(string)
				if _, exists := typed[key]; !exists {
					return fmt.Errorf("setting %q is required", key)
				}
			}
		}
		for key, child := range typed {
			compact := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
			secret := strings.Contains(compact, "token") || strings.Contains(compact, "password") || strings.Contains(compact, "secret") || strings.Contains(compact, "apikey") || strings.Contains(compact, "authorization") || strings.Contains(compact, "credential")
			if secret {
				name, reference := child.(string)
				if !strings.HasSuffix(strings.ToLower(key), "_env") || !reference || !mcpEnvNamePattern.MatchString(name) || strings.HasPrefix(name, "NEURO_") {
					return fmt.Errorf("setting %q may contain only an *_env reference, never a credential", key)
				}
			}
			childSchema, known := properties[key].(map[string]interface{})
			if schema["additionalProperties"] == false && !known {
				return fmt.Errorf("setting %q is not declared", key)
			}
			if err := validateExtensionSettings(child, childSchema, depth+1); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	case []interface{}:
		typeOK = typeOK || typeName == "array"
		childSchema, _ := schema["items"].(map[string]interface{})
		for _, child := range typed {
			if err := validateExtensionSettings(child, childSchema, depth+1); err != nil {
				return err
			}
		}
	case string:
		typeOK = typeOK || typeName == "string"
		if max, ok := schema["maxLength"].(float64); ok && float64(len([]rune(typed))) > max {
			return errors.New("string exceeds maxLength")
		}
	case bool:
		typeOK = typeOK || typeName == "boolean"
	case float64:
		typeOK = typeOK || typeName == "number" || (typeName == "integer" && math.Trunc(typed) == typed)
		if min, ok := schema["minimum"].(float64); ok && typed < min {
			return errors.New("number is below minimum")
		}
		if max, ok := schema["maximum"].(float64); ok && typed > max {
			return errors.New("number exceeds maximum")
		}
	case nil:
		typeOK = typeOK || typeName == "null"
	default:
		return errors.New("settings contain a non-JSON value")
	}
	if !typeOK {
		return fmt.Errorf("value must have type %s", typeName)
	}
	return nil
}

func (a *AdminServer) handleExtensionPermissions(w http.ResponseWriter, r *http.Request, id string) {
	id, err := normalizeExtensionID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	declarations, schema, err := a.integration.extensionDeclarations(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "extension is not in the catalog, marketplace, or integration registry")
		return
	}
	if r.Method == http.MethodPut {
		// The manager must prompt for this value for every approval; it must
		// never copy its in-memory session token into this header automatically.
		if !a.credential.matches(strings.TrimSpace(r.Header.Get("X-ND-Operator-Token"))) {
			a.integration.audit.record("extension_permissions", map[string]interface{}{"id": id, "decision": "refused", "reason": "operator re-entry required"})
			writeError(w, http.StatusForbidden, "re-enter your operator token to change extension permissions or settings")
			return
		}
		var payload *ExtensionPermissionPolicy
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload == nil {
			writeError(w, http.StatusBadRequest, "body must be an extension permission policy JSON object")
			return
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			writeError(w, http.StatusBadRequest, "body must contain exactly one JSON object")
			return
		}
		policy := *payload
		if policy.Grants == nil {
			policy.Grants = map[string]bool{}
		}
		if policy.Settings == nil {
			policy.Settings = map[string]interface{}{}
		}
		if err := validateExtensionPolicy(CatalogItem{Permissions: declarations, SettingsSchema: schema}, policy); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.integration.extensionPermissionsMu.Lock()
		file, loadErr := loadExtensionPermissions(extensionPermissionsPath())
		if loadErr == nil {
			file.Extensions[id] = policy
			loadErr = saveExtensionPermissions(extensionPermissionsPath(), file)
		}
		a.integration.extensionPermissionsMu.Unlock()
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, loadErr.Error())
			return
		}
		a.integration.audit.record("extension_permissions", map[string]interface{}{"id": id, "decision": "updated", "manage_allowed": policy.ManageAllowed, "run_allowed": policy.RunAllowed})
		go a.integration.syncMCPServers()
	} else if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	policy, legacy, err := a.integration.extensionPermissionSnapshot(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": id, "policy": policy, "permissions": declarations, "settings_schema": schema, "legacy": legacy})
}
