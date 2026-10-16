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
	// ScopeGame covers the high-level game interface (playing a game that has
	// no dedicated integration).
	ScopeGame PermissionScope = "game"
	// ScopeShell covers running command lines (headless machines, terminal work).
	ScopeShell PermissionScope = "shell"
	// ScopeExtensions covers listing, installing, enabling, and removing catalog
	// extensions. It is explicit consent: listing the actions does not enable it.
	ScopeExtensions PermissionScope = "extensions"
)

// scopeDescriptions are the one-line explanations the dashboard shows next to
// each switch. They are part of the operator's decision, so keep them literal.
var scopeDescriptions = map[PermissionScope]string{
	ScopeInput:      "Keyboard and mouse input, window management",
	ScopeGame:       "The high-level game interface (play a game with no integration)",
	ScopeShell:      "Run command lines (explicit consent; also needs NEURO_SHELL_ALLOWLIST)",
	ScopeFilesystem: "Open file manager and similar file-level shortcuts",
	ScopeProcess:    "Task manager, close apps, run dialog",
	ScopeNetwork:    "Browse the extension catalog (read-only)",
	ScopeSystem:     "Lock, settings, power menu, launching programs (explicit consent)",
	ScopeVision:     "Screenshots, desktop context and game observation (read-only)",
	ScopeExtensions: "Install, enable, disable and remove extensions (explicit consent)",
}

// alwaysAllowed actions are never refused by the policy itself. Neuro must be
// able to ask for a permission it lacks. The deny list and NEURO_DENY_ACTIONS
// still apply, so the operator can switch the request path off.
// alwaysAllowed actions never need a scope, a grant, or a switch. They read
// help or release input and cannot start anything, so a restrictive policy
// must not be able to lock Neuro out of its own recovery path.
var alwaysAllowed = map[string]bool{
	string(CmdRequestPermission): true,
	string(CmdDesktopGuide):      true,
	string(CmdResetControls):     true,
}

// ScopeLimits are the per-scope knobs the dashboard exposes. Only the rate
// limit is enforced today; it is persisted so a policy survives a round trip
// through the UI unchanged.
type ScopeLimits struct {
	MaxActionsPerMinute int `json:"max_actions_per_minute,omitempty"`
}

type ScopeConfig struct {
	Allowed bool `json:"allowed"`
	// Requestable lets Neuro ask the operator for this scope with
	// request_permission. It grants nothing by itself, and switching it off also
	// ends any approval that was given for the scope.
	Requestable bool        `json:"requestable,omitempty"`
	Limits      ScopeLimits `json:"limits,omitempty"`
}

// UnmarshalJSON accepts both the documented shape ({"allowed": true}) and the
// shorthand people reach for when hand-editing a policy ("game": true).
func (s *ScopeConfig) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "true" {
		s.Allowed = true
		return nil
	}
	if trimmed == "false" {
		s.Allowed = false
		return nil
	}

	var raw struct {
		Allowed     *flexibleBool `json:"allowed"`
		Requestable *flexibleBool `json:"requestable"`
		Limits      ScopeLimits   `json:"limits"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("a scope must be true/false or {\"allowed\": true/false}")
	}
	if raw.Allowed != nil {
		s.Allowed = bool(*raw.Allowed)
	}
	if raw.Requestable != nil {
		s.Requestable = bool(*raw.Requestable)
	}
	if raw.Limits.MaxActionsPerMinute < 0 {
		return fmt.Errorf("max_actions_per_minute must not be negative")
	}
	s.Limits = raw.Limits
	return nil
}

// flexibleBool accepts true/false and the string spellings people use in configs.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = flexibleBool(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		switch strings.ToLower(strings.TrimSpace(asString)) {
		case "true", "yes", "1", "on", "allow", "allowed":
			*b = true
			return nil
		case "false", "no", "0", "off", "deny", "denied":
			*b = false
			return nil
		}
	}

	return fmt.Errorf("expected true/false (or a boolean-like string)")
}

type PermissionPolicy struct {
	DefaultAllow bool
	allowed      map[string]struct{}
	denied       map[string]struct{}
	scopes       map[PermissionScope]ScopeConfig
	// hardDeny comes from NEURO_DENY_ACTIONS and outranks both lists: it is the
	// operator's "not even if the dashboard says so".
	hardDeny map[string]bool
	// grants are operator approvals of requests. This is runtime state, not
	// policy: setPolicy carries it across dashboard saves.
	grants *grantStore
}

type permissionPolicyFile struct {
	// Version is accepted but never parsed: it is documentation for humans, and
	// a policy using "1" as well as "1.0.0" must both load.
	Version        json.RawMessage                 `json:"version"`
	DefaultAllow   *flexibleBool                   `json:"default_allow"`
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
	string(CmdMarketplaceSearch):   ScopeNetwork,
	string(CmdStageIntegration):    ScopeExtensions,
	string(CmdViewerVisibility):    ScopeExtensions,
	string(CmdGetCatalogItem):      ScopeNetwork,
	string(CmdGetDesktopContext):   ScopeVision,
	string(CmdSendDesktopContext):  ScopeVision,
	string(CmdListInstalledExts):   ScopeExtensions,
	string(CmdInstallExtension):    ScopeExtensions,
	string(CmdUninstallExtension):  ScopeExtensions,
	string(CmdEnableExtension):     ScopeExtensions,
	string(CmdDisableExtension):    ScopeExtensions,
	string(CmdGetStatus):           ScopeVision,
	string(CmdShutdownGracefully):  ScopeSystem,
	string(CmdShutdownImmediately): ScopeSystem,

	// The guide is read-only information, so it rides the vision scope (on by
	// default) — a weak model must always be able to ask how to use this thing.
	string(CmdDesktopGuide): ScopeVision,

	// Shell: denied in the shipped example policy, and additionally fenced by
	// the shell allowlist/denylist firewall.
	string(CmdShellCommand): ScopeShell,

	// Game interface. Observing is read-only (vision); driving the game is
	// input; launching a game is system (denied by default).
	string(CmdGameListProfiles): ScopeGame,
	string(CmdGameDetect):       ScopeVision,
	string(CmdGameStartSession): ScopeGame,
	string(CmdGameEndSession):   ScopeGame,
	string(CmdGameStatus):       ScopeVision,
	string(CmdGameMove):         ScopeGame,
	string(CmdGameLook):         ScopeGame,
	string(CmdGameAction):       ScopeGame,
	string(CmdGamePress):        ScopeGame,
	string(CmdGameReleaseAll):   ScopeGame,
	string(CmdGameObserve):      ScopeVision,
	string(CmdGameLaunch):       ScopeSystem,
}

func defaultPermissionPolicy() *PermissionPolicy {
	// Safer default: deny unless allowed. Missing policy file still uses this
	// for production-minded installs; set default_allow true explicitly for labs.
	return &PermissionPolicy{
		DefaultAllow: false,
		allowed:      map[string]struct{}{},
		denied:       map[string]struct{}{},
		grants:       newGrantStore(),
		scopes: map[PermissionScope]ScopeConfig{
			ScopeInput:      {Allowed: true},
			ScopeFilesystem: {Allowed: false},
			ScopeProcess:    {Allowed: true},
			ScopeNetwork:    {Allowed: true},
			ScopeSystem:     {Allowed: false},
			ScopeVision:     {Allowed: true},
			ScopeGame:       {Allowed: true},
			// A default install must not hand out a shell or extension installs.
			ScopeShell:      {Allowed: false},
			ScopeExtensions: {Allowed: false},
		},
	}
}

func loadPermissionPolicy(path string) (*PermissionPolicy, error) {
	if path == "" {
		policy := defaultPermissionPolicy()
		policy.applyHardDeny(hardDeniedActions())
		return policy, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			policy := defaultPermissionPolicy()
			policy.applyHardDeny(hardDeniedActions())
			return policy, nil
		}
		return nil, fmt.Errorf("failed to read permissions file %s: %w", path, err)
	}

	var parsed permissionPolicyFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse permissions file %s: %w", path, err)
	}

	policy := defaultPermissionPolicy()
	policy.applyHardDeny(hardDeniedActions())
	if parsed.DefaultAllow != nil {
		policy.DefaultAllow = bool(*parsed.DefaultAllow)
	}

	for _, action := range parsed.AllowedActions {
		policy.allowed[strings.TrimSpace(action)] = struct{}{}
	}
	for _, action := range parsed.DeniedActions {
		policy.denied[strings.TrimSpace(action)] = struct{}{}
	}
	for scope, cfg := range parsed.Scopes {
		if !knownScope(scope) {
			// A typo ("games" instead of "game") would otherwise silently grant
			// or drop a capability; say so instead.
			fmt.Fprintf(os.Stderr, "[warn] permissions file %s: unknown scope %q ignored\n", path, scope)
			continue
		}
		policy.scopes[scope] = cfg
	}

	return policy, nil
}

// ScopeAllowed answers the scope question directly, which is what script-level
// checks need (a script is one action but may contain several capabilities).
func (p *PermissionPolicy) ScopeAllowed(scope PermissionScope) bool {
	if p == nil {
		return false
	}
	if _, ok := p.scopes[scope]; ok {
		return p.scopeOn(scope)
	}
	return p.DefaultAllow
}

// ScopeRateLimit returns the scope's actions-per-minute budget, or 0 when the
// scope is unlimited.
func (p *PermissionPolicy) ScopeRateLimit(scope PermissionScope) int {
	if p == nil || scope == "" {
		return 0
	}
	if cfg, ok := p.scopes[scope]; ok {
		return cfg.Limits.MaxActionsPerMinute
	}
	return 0
}

// ScopeConfigs exposes the per-scope configuration for the dashboard.
func (p *PermissionPolicy) ScopeConfigs() map[PermissionScope]ScopeConfig {
	out := map[PermissionScope]ScopeConfig{}
	if p == nil {
		return out
	}
	for _, scope := range allScopes() {
		out[scope] = p.scopes[scope]
	}
	return out
}

// scopeRequiresExplicitConsent marks the scopes that must be enabled by name.
// shell and system can wipe a machine or run arbitrary programs, so a policy
// that merely lists the action (or sets default_allow) does not enable them.
func scopeRequiresExplicitConsent(scope PermissionScope) bool {
	switch scope {
	case ScopeShell, ScopeSystem, ScopeExtensions:
		return true
	}
	return false
}

func knownScope(scope PermissionScope) bool {
	switch scope {
	case ScopeInput, ScopeGame, ScopeShell, ScopeFilesystem, ScopeProcess, ScopeNetwork, ScopeSystem, ScopeVision, ScopeExtensions:
		return true
	}
	return false
}

// scopeForAction returns the permission scope an action belongs to. MCP tools
// are not in the static table (their names come from the server at runtime), so
// they map by prefix to the extensions scope. Every scope lookup goes through
// here so that a dynamic name can never fall through to default_allow.
func scopeForAction(action string) (PermissionScope, bool) {
	if scope, ok := actionScope[action]; ok {
		return scope, true
	}
	if strings.HasPrefix(action, mcpActionPrefix) {
		return ScopeExtensions, true
	}
	return "", false
}

func (p *PermissionPolicy) IsAllowed(action string) bool {
	// The hard deny list outranks everything, including an explicit allow.
	if p.hardDenied(action) {
		return false
	}

	// Deny list always wins.
	if _, denied := p.denied[action]; denied {
		return false
	}

	if alwaysAllowed[action] {
		return true
	}

	// The dangerous scopes are checked before the explicit allow list: a bare
	// allow-list entry (or default_allow) must not hand out a shell or system
	// control. The operator has to turn those scopes on deliberately.
	if scope, ok := scopeForAction(action); ok && scopeRequiresExplicitConsent(scope) {
		return p.scopeOn(scope)
	}

	// Explicit allow list wins next.
	if _, allowed := p.allowed[action]; allowed {
		return true
	}

	// Scope gates: Vedal toggles capability categories in the management UI.
	if scope, ok := scopeForAction(action); ok {
		if _, configured := p.scopes[scope]; configured {
			return p.scopeOn(scope)
		}
	}

	return p.DefaultAllow
}

// scopeOn reports whether a configured scope is on: the operator's switch, or an
// approval of a request that is still in force while the scope is requestable.
func (p *PermissionPolicy) scopeOn(scope PermissionScope) bool {
	if p == nil {
		return false
	}
	cfg, ok := p.scopes[scope]
	if !ok {
		return false
	}
	return cfg.Allowed || p.grantedFor(scope)
}

// ScopeRequestable reports whether Neuro may ask the operator for the scope.
func (p *PermissionPolicy) ScopeRequestable(scope PermissionScope) bool {
	if p == nil {
		return false
	}
	cfg, ok := p.scopes[scope]
	return ok && cfg.Requestable
}

// grantedFor reports whether an approved request currently lifts the scope.
func (p *PermissionPolicy) grantedFor(scope PermissionScope) bool {
	if !p.ScopeRequestable(scope) {
		return false
	}
	return p.grants.activeFor(scope)
}
