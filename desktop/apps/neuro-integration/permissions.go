package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PermissionScope groups actions for Vedal's capability management UI.
type PermissionScope string

const (
	ScopeInput      PermissionScope = "input"
	ScopeFilesystem PermissionScope = "filesystem"
	ScopeProcess    PermissionScope = "process"
	ScopeNetwork    PermissionScope = "network"
	ScopeSystem     PermissionScope = "system"
	ScopeVision     PermissionScope = "vision"
)

type ScopeConfig struct {
	Allowed bool `json:"allowed"`
}

type PermissionPolicy struct {
	DefaultAllow bool
	allowed      map[string]struct{}
	denied       map[string]struct{}
	scopes       map[PermissionScope]ScopeConfig
}

type permissionPolicyFile struct {
	Version        string                          `json:"version"`
	DefaultAllow   *bool                           `json:"default_allow"`
	AllowedActions []string                        `json:"allowed_actions"`
	DeniedActions  []string                        `json:"denied_actions"`
	Scopes         map[PermissionScope]ScopeConfig `json:"scopes"`
}

// actionScope maps Neuro action names to capability scopes.
var actionScope = map[string]PermissionScope{
	string(CmdMouseMove):           ScopeInput,
	string(CmdMouseClick):          ScopeInput,
	string(CmdKeyPress):            ScopeInput,
	string(CmdTypeText):            ScopeInput,
	string(CmdRunScript):           ScopeInput,
	string(CmdExecuteQueue):        ScopeInput,
	string(CmdClearActionQueue):    ScopeInput,
	string(EnableLLControls):       ScopeInput,
	string(DisableLLControls):      ScopeInput,
	string(CmdOpenWindowsMenu):     ScopeInput,
	string(CmdShowDesktop):         ScopeInput,
	string(CmdMinimizeAll):         ScopeInput,
	string(CmdCloseForeground):     ScopeInput,
	string(CmdOpenTaskManager):     ScopeProcess,
	string(CmdCloseAllApps):        ScopeProcess,
	string(CmdOpenExplorer):        ScopeFilesystem,
	string(CmdOpenRunDialog):       ScopeProcess,
	string(CmdOpenSearch):          ScopeInput,
	string(CmdSnapWindowLeft):      ScopeInput,
	string(CmdSnapWindowRight):     ScopeInput,
	string(CmdOpenSettings):        ScopeSystem,
	string(CmdOpenNotification):    ScopeInput,
	string(CmdOpenClipboard):       ScopeInput,
	string(CmdLockWorkstation):     ScopeSystem,
	string(CmdSwitchAppNext):       ScopeInput,
	string(CmdSwitchAppPrevious):   ScopeInput,
	string(CmdOpenPowerUserMenu):   ScopeSystem,
	string(CmdTakeScreenSnip):      ScopeVision,
	string(CmdListCatalogItems):    ScopeNetwork,
	string(CmdFindCatalogItems):    ScopeNetwork,
	string(CmdGetCatalogItem):      ScopeNetwork,
	string(CmdGetDesktopContext):   ScopeVision,
	string(CmdSendDesktopContext):  ScopeVision,
	string(CmdListInstalledExts):   ScopeFilesystem,
	string(CmdInstallExtension):    ScopeFilesystem,
	string(CmdUninstallExtension):  ScopeFilesystem,
	string(CmdEnableExtension):     ScopeFilesystem,
	string(CmdDisableExtension):    ScopeFilesystem,
	string(CmdGetStatus):           ScopeVision,
	string(CmdShutdownGracefully):  ScopeSystem,
	string(CmdShutdownImmediately): ScopeSystem,
}

func defaultPermissionPolicy() *PermissionPolicy {
	// Safer default: deny unless allowed. Missing policy file still uses this
	// for production-minded installs; set default_allow true explicitly for labs.
	return &PermissionPolicy{
		DefaultAllow: false,
		allowed:      map[string]struct{}{},
		denied:       map[string]struct{}{},
		scopes: map[PermissionScope]ScopeConfig{
			ScopeInput:      {Allowed: true},
			ScopeFilesystem: {Allowed: false},
			ScopeProcess:    {Allowed: true},
			ScopeNetwork:    {Allowed: true},
			ScopeSystem:     {Allowed: false},
			ScopeVision:     {Allowed: true},
		},
	}
}

func loadPermissionPolicy(path string) (*PermissionPolicy, error) {
	if path == "" {
		return defaultPermissionPolicy(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultPermissionPolicy(), nil
		}
		return nil, fmt.Errorf("failed to read permissions file %s: %w", path, err)
	}

	var parsed permissionPolicyFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse permissions file %s: %w", path, err)
	}

	policy := defaultPermissionPolicy()
	if parsed.DefaultAllow != nil {
		policy.DefaultAllow = *parsed.DefaultAllow
	}

	for _, action := range parsed.AllowedActions {
		policy.allowed[strings.TrimSpace(action)] = struct{}{}
	}
	for _, action := range parsed.DeniedActions {
		policy.denied[strings.TrimSpace(action)] = struct{}{}
	}
	for scope, cfg := range parsed.Scopes {
		policy.scopes[scope] = cfg
	}

	return policy, nil
}

func (p *PermissionPolicy) IsAllowed(action string) bool {
	// Deny list always wins.
	if _, denied := p.denied[action]; denied {
		return false
	}

	// Explicit allow list wins next.
	if _, allowed := p.allowed[action]; allowed {
		return true
	}

	// Scope gates: Vedal toggles capability categories in the management UI.
	if scope, ok := actionScope[action]; ok {
		if cfg, ok := p.scopes[scope]; ok {
			return cfg.Allowed
		}
	}

	return p.DefaultAllow
}
