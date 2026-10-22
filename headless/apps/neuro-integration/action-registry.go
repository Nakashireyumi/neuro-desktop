package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

const (
	CmdMouseMove  CommandType = "move_mouse_to"
	CmdMouseClick CommandType = "mouse_click"
	CmdKeyPress   CommandType = "key_press"
	CmdTypeText   CommandType = "type_text"

	EnableLLControls  CommandType = "enable_low_level_controls"
	DisableLLControls CommandType = "disable_low_level_controls"

	CmdOpenWindowsMenu     CommandType = "open_windows_menu"
	CmdShowDesktop         CommandType = "show_desktop"
	CmdMinimizeAll         CommandType = "minimize_all_windows"
	CmdCloseForeground     CommandType = "close_foreground_app"
	CmdOpenTaskManager     CommandType = "open_task_manager"
	CmdCloseAllApps        CommandType = "close_all_apps"
	CmdOpenExplorer        CommandType = "open_file_explorer"
	CmdOpenRunDialog       CommandType = "open_run_dialog"
	CmdOpenSearch          CommandType = "open_windows_search"
	CmdSnapWindowLeft      CommandType = "snap_window_left"
	CmdSnapWindowRight     CommandType = "snap_window_right"
	CmdOpenSettings        CommandType = "open_windows_settings"
	CmdOpenNotification    CommandType = "open_notification_center"
	CmdOpenClipboard       CommandType = "open_clipboard_history"
	CmdLockWorkstation     CommandType = "lock_workstation"
	CmdSwitchAppNext       CommandType = "switch_app_next"
	CmdSwitchAppPrevious   CommandType = "switch_app_previous"
	CmdOpenPowerUserMenu   CommandType = "open_power_user_menu"
	CmdTakeScreenSnip      CommandType = "take_screen_snip"
	CmdListCatalogItems    CommandType = "list_catalog_items"
	CmdFindCatalogItems    CommandType = "find_catalog_items"
	CmdGetCatalogItem      CommandType = "get_catalog_item"
	CmdGetDesktopContext   CommandType = "get_desktop_context"
	CmdSendDesktopContext  CommandType = "send_desktop_context"
	CmdListInstalledExts   CommandType = "list_installed_extensions"
	CmdInstallExtension    CommandType = "install_extension"
	CmdUninstallExtension  CommandType = "uninstall_extension"
	CmdEnableExtension     CommandType = "enable_extension"
	CmdDisableExtension    CommandType = "disable_extension"
	CmdMarketplaceSearch   CommandType = "marketplace_search"
	CmdStageIntegration    CommandType = "stage_integration_source"
	CmdViewerVisibility    CommandType = "set_viewer_visibility"
	CmdGetStatus           CommandType = "get_status"
	CmdRunScript           CommandType = "run_script"
	CmdExecuteQueue        CommandType = "execute_queue"
	CmdClearActionQueue    CommandType = "clear_action_queue"
	CmdShutdownGracefully  CommandType = "shutdown_gracefully"
	CmdShutdownImmediately CommandType = "shutdown_immediately"

	// Raw input primitives the game layer needs (relative look, held keys,
	// key combinations, held mouse buttons, and a safety release-everything).
	// Self-documentation for small models.
	CmdDesktopGuide  CommandType = "desktop_guide"
	CmdResetControls CommandType = "reset_controls"

	// Shell capability (headless-friendly: this is what Neuro can do on a
	// command-line-only machine). Guarded by the `shell` scope and the shell
	// firewall, and never enabled by the shipped example policy.
	CmdShellCommand CommandType = "shell_command"

	CmdMoveMouseRelative CommandType = "move_mouse_relative"
	CmdKeyHoldFor        CommandType = "key_hold_for"
	CmdKeyReleaseAll     CommandType = "key_release_all"
	CmdKeyCombo          CommandType = "key_combo"
	CmdMouseHoldFor      CommandType = "mouse_hold_for"

	// High-level game interface: Neuro plays a game that has no dedicated
	// integration, or observes one that does.
	CmdGameListProfiles CommandType = "game_list_profiles"
	CmdGameDetect       CommandType = "game_detect"
	CmdGameStartSession CommandType = "game_start_session"
	CmdGameEndSession   CommandType = "game_end_session"
	CmdGameStatus       CommandType = "game_status"
	CmdGameMove         CommandType = "game_move"
	CmdGameLook         CommandType = "game_look"
	CmdGameAction       CommandType = "game_action"
	CmdGamePress        CommandType = "game_press"
	CmdGameReleaseAll   CommandType = "game_release_all"
	CmdGameObserve      CommandType = "game_observe"
	CmdGameLaunch       CommandType = "game_launch"
)

// actionKindGame marks the high-level game interface actions so they can be
// registered (or withheld) as a group.
const actionKindGame = "game"

var (
	RegisterHLActionsOnStartup   bool = false
	RegisterLLActionsOnStartup   bool = true
	RegisterGameActionsOnStartup bool = getEnvBool("NEURO_GAME_ACTIONS", true)

	currentActionList   = map[string]neuro.ActionHandler{}
	currentActionListMu sync.Mutex
	actionModeMu        sync.Mutex
)

type actionSpec struct {
	Name        CommandType
	Description string
	Schema      *neuro.ActionSchema
	// Kind groups actions: "" for desktop actions, actionKindGame for the game
	// interface. The registration list is filtered by kind.
	Kind string
}

var HLActionSpecs = []actionSpec{
	{
		Name:        EnableLLControls,
		Description: "Enable low-level mouse and keyboard controls, and disable high-level controls",
		Schema:      nil,
	},
	{
		Name:        CmdOpenWindowsMenu,
		Description: "Open the OS start menu / launcher (Windows Start, macOS Spotlight, Linux Activities)",
		Schema:      nil,
	},
	{
		Name:        CmdShowDesktop,
		Description: "Show the desktop by minimizing or hiding windows",
		Schema:      nil,
	},
	{
		Name:        CmdMinimizeAll,
		Description: "Minimize or hide all windows",
		Schema:      nil,
	},
	{
		Name:        CmdCloseForeground,
		Description: "Close the foreground application",
		Schema:      nil,
	},
	{
		Name:        CmdOpenTaskManager,
		Description: "Open the system task/process manager (or Force Quit on macOS)",
		Schema:      nil,
	},
	{
		Name:        CmdCloseAllApps,
		Description: "Non-destructive fallback that shows the desktop",
		Schema:      nil,
	},
	{
		Name:        CmdOpenExplorer,
		Description: "Open the file manager (Explorer / Finder / Files)",
		Schema:      nil,
	},
	{
		Name:        CmdOpenRunDialog,
		Description: "Open the run/command launcher dialog",
		Schema:      nil,
	},
	{
		Name:        CmdOpenSearch,
		Description: "Open system search / spotlight",
		Schema:      nil,
	},
	{
		Name:        CmdSnapWindowLeft,
		Description: "Snap or tile the active window to the left half of the screen",
		Schema:      nil,
	},
	{
		Name:        CmdSnapWindowRight,
		Description: "Snap or tile the active window to the right half of the screen",
		Schema:      nil,
	},
	{
		Name:        CmdOpenSettings,
		Description: "Open system or app settings",
		Schema:      nil,
	},
	{
		Name:        CmdOpenNotification,
		Description: "Open the notification / quick settings panel",
		Schema:      nil,
	},
	{
		Name:        CmdOpenClipboard,
		Description: "Open clipboard history if supported on this OS",
		Schema:      nil,
	},
	{
		Name:        CmdLockWorkstation,
		Description: "Lock the current workstation / session",
		Schema:      nil,
	},
	{
		Name:        CmdSwitchAppNext,
		Description: "Switch to the next application",
		Schema:      nil,
	},
	{
		Name:        CmdSwitchAppPrevious,
		Description: "Switch to the previous application",
		Schema:      nil,
	},
	{
		Name:        CmdOpenPowerUserMenu,
		Description: "Open the power-user / quick system menu when available",
		Schema:      nil,
	},
	{
		Name:        CmdTakeScreenSnip,
		Description: "Open the screenshot / snipping overlay",
		Schema:      nil,
	},
}

// Desktop context is available in every input mode.
var DesktopContextActionSpecs = []actionSpec{
	{
		Name:        CmdGetDesktopContext,
		Description: "Request a desktop context snapshot (platform, input availability, screen, active window, samples, optional vision summary). The snapshot follows as a message.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"capture_screenshot": map[string]interface{}{
				"type":        "boolean",
				"default":     false,
				"description": "Capture a screenshot before generating context",
			},
		}, nil),
	},
	{
		Name:        CmdSendDesktopContext,
		Description: "Request desktop context and send it to Neuro as a context message, where it remains available for later decisions.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"capture_screenshot": map[string]interface{}{
				"type":        "boolean",
				"default":     false,
				"description": "Capture a screenshot before generating context",
			},
			"silent": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Send context silently",
			},
		}, nil),
	},
}

// ShellActionSpecs is registered independently of the high/low level switch:
// a headless machine has no high-level intents, but it does have a shell.
var ShellActionSpecs = []actionSpec{
	{
		Name: CmdShellCommand,
		Description: `Run one command line on the controlled machine and return its exit code and output. ` +
			`Use this on headless/command-line-only machines, or for terminal work. ` +
			`The program must be on the shell allowlist. Example: {"command": "ls -la"}`,
		Schema: neuro.WrapSchema(map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The full command line to run, e.g. \"ls -la\" or \"python3 --version\"",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory; defaults to NEURO_SHELL_CWD or the executor's directory",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "How long to wait, in seconds (default 20, maximum 120)",
			},
		}, []string{"command"}),
	},
}

// Management actions remain available in both input modes.
var ManagementActionSpecs = []actionSpec{
	{
		Name:        CmdMarketplaceSearch,
		Description: "Search the integration marketplace. Listings explain compatibility, installation steps, and permissions; a listing does not imply an integration is installed or running.",
		Schema:      neuro.WrapSchema(map[string]interface{}{"query": map[string]interface{}{"type": "string", "description": "Optional name, game, or tag"}}, nil),
	},
	{
		Name:        CmdStageIntegration,
		Description: "Stage an approved marketplace integration's pinned source for local setup. Needs extensions permission and per-item management delegation. This does not execute an installer or start the integration; the result arrives as a message.",
		Schema:      neuro.WrapSchema(map[string]interface{}{"item_id": map[string]interface{}{"type": "string", "description": "Marketplace integration id"}}, []string{"item_id"}),
	},
	{
		Name:        CmdViewerVisibility,
		Description: "Choose whether viewers can see your curated integration activity. Sharing expires automatically; private messages, credentials, and raw desktop capture are excluded.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"integration_id": map[string]interface{}{"type": "string"},
			"actor":          map[string]interface{}{"type": "string", "enum": []string{"neuro", "evil"}},
			"visible":        map[string]interface{}{"type": "boolean"},
			"minutes":        map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 60, "default": 15},
		}, []string{"integration_id", "actor", "visible"}),
	},
	{
		Name:        CmdListCatalogItems,
		Description: "List available catalog integrations/apps configured for Neurcro",
		Schema:      nil,
	},
	{
		Name:        CmdFindCatalogItems,
		Description: "Search catalog items by name, id, description, or tags",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"default":     5,
				"description": "Maximum number of results (1-20)",
			},
		}, []string{"query"}),
	},
	{
		Name:        CmdGetCatalogItem,
		Description: "Get detailed metadata for a single catalog item by id",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"item_id": map[string]interface{}{
				"type":        "string",
				"description": "Catalog item id",
			},
		}, []string{"item_id"}),
	},
	{
		Name:        CmdListInstalledExts,
		Description: "List installed/managed Neurcro extensions",
		Schema:      nil,
	},
	{
		Name: CmdInstallExtension,
		Description: "Install a catalog extension by id. Needs the extensions permission (you can ask for it with request_permission). " +
			"Only signed catalog items can fetch code. The outcome arrives later as a message.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"item_id": map[string]interface{}{
				"type":        "string",
				"description": "Catalog extension id",
			},
		}, []string{"item_id"}),
	},
	{
		Name:        CmdUninstallExtension,
		Description: "Uninstall an extension by id",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"item_id": map[string]interface{}{
				"type":        "string",
				"description": "Catalog extension id",
			},
		}, []string{"item_id"}),
	},
	{
		Name:        CmdEnableExtension,
		Description: "Enable an installed extension by id",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"item_id": map[string]interface{}{
				"type":        "string",
				"description": "Catalog extension id",
			},
		}, []string{"item_id"}),
	},
	{
		Name:        CmdDisableExtension,
		Description: "Disable an installed extension by id",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"item_id": map[string]interface{}{
				"type":        "string",
				"description": "Catalog extension id",
			},
		}, []string{"item_id"}),
	},
}

// alwaysRegisteredSpecs are registered whatever the high- or low-level mode.
// The shell is the main capability on a headless machine, the guide is the
// cheapest way to make a small model behave, and request_permission is how
// Neuro asks the operator for a permission it does not have.
func alwaysRegisteredSpecs() []actionSpec {
	specs := append([]actionSpec{}, DesktopContextActionSpecs...)
	specs = append(specs, ShellActionSpecs...)
	specs = append(specs, ManagementActionSpecs...)
	specs = append(specs, guideActionSpecs()...)
	specs = append(specs, escapeHatchSpecs()...)
	return append(specs, permissionRequestSpecs()...)
}

var LLActionSpecs = []actionSpec{
	{
		Name:        DisableLLControls,
		Description: "Disable low-level controls and re-enable high-level controls",
		Schema:      nil,
	},
	{
		Name:        CmdMouseMove,
		Description: "Move mouse cursor to specific coordinates in screen pixels. Example: {\"x\": 640, \"y\": 360}",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"x": map[string]interface{}{
				"type":        "integer",
				"description": "X coordinate",
			},
			"y": map[string]interface{}{
				"type":        "integer",
				"description": "Y coordinate",
			},
			"execute_now": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Execute immediately or keep in queue",
			},
			"clear_after": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Clear queue after execution",
			},
		}, []string{"x", "y"}),
	},
	{
		Name:        CmdMouseClick,
		Description: "Click mouse button at current cursor position. Example: {\"button\": \"left\"}",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"button": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"left", "right", "middle"},
				"default":     "left",
				"description": "Mouse button",
			},
			"execute_now": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Execute immediately or keep in queue",
			},
			"clear_after": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Clear queue after execution",
			},
		}, nil),
	},
	{
		Name:        CmdTypeText,
		Description: "Type printable ASCII using the keyboard. Example: {\"text\": \"hello\"}. Non-ASCII text is refused; use shell_command.",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"maxLength":   1000,
				"description": "Printable ASCII text to type",
			},
			"execute_now": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Execute immediately or keep in queue",
			},
			"clear_after": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Clear queue after execution",
			},
		}, []string{"text"}),
	},
	{
		Name:        CmdKeyPress,
		Description: "Press a specific keyboard key. Example: {\"key\": \"enter\"}",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Key to press",
			},
			"execute_now": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Execute immediately or keep in queue",
			},
			"clear_after": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Clear queue after execution",
			},
		}, []string{"key"}),
	},
	{
		Name:        CmdRunScript,
		Description: "Execute action script language (see integration docs)",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"script": map[string]interface{}{
				"type":        "string",
				"maxLength":   65536,
				"description": "Action script body",
			},
			"execute_now": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Execute immediately or keep in queue",
			},
			"clear_after": map[string]interface{}{
				"type":        "boolean",
				"default":     true,
				"description": "Clear queue after execution",
			},
		}, []string{"script"}),
	},
	{
		Name:        CmdExecuteQueue,
		Description: "Execute queued actions",
		Schema:      nil,
	},
	{
		Name:        CmdClearActionQueue,
		Description: "Clear queued actions that have not run yet. Example: {}",
		Schema:      nil,
	},
}

type IPCProxyAction struct {
	integration *NDIntegration
	spec        actionSpec
}

func (a *IPCProxyAction) GetName() string {
	return string(a.spec.Name)
}

func (a *IPCProxyAction) GetDescription() string {
	return a.spec.Description
}

func (a *IPCProxyAction) GetSchema() *neuro.ActionSchema {
	return a.spec.Schema
}

func (a *IPCProxyAction) Validate(data json.RawMessage) (interface{}, neuro.ExecutionResult) {
	name := a.GetName()
	a.integration.stats.noteAction(name)

	policy := a.integration.policy()

	// Pause flag and kill switch come first: they must hold even if the policy
	// was just widened from the dashboard.
	if reason := a.integration.stop.blockReason(name); reason != "" {
		a.integration.stats.noteDenied(name)
		a.integration.audit.record("action", map[string]interface{}{
			"action": name, "decision": "refused", "reason": "stopped",
		})
		return nil, neuro.NewFailureResult(reason)
	}

	if policy != nil && !policy.IsAllowed(name) {
		a.integration.stats.noteDenied(name)
		a.integration.audit.record("action", map[string]interface{}{
			"action": name, "decision": "refused", "reason": "policy",
		})
		return nil, neuro.NewFailureResult(policyDenialMessage(name, policy))
	}

	// Per-scope rate limit, before any work is queued.
	if policy != nil {
		scope, _ := scopeForAction(name)
		if limit := policy.ScopeRateLimit(scope); limit > 0 {
			if allowed, retryAfter := a.integration.rate.allow(scope, limit, time.Now()); !allowed {
				a.integration.stats.noteDenied(name)
				return nil, neuro.NewFailureResult(rateLimitDenial(scope, limit, retryAfter))
			}
		}
	}

	params := map[string]interface{}{}
	if err := neuro.ParseActionData(data, &params); err != nil {
		return nil, neuro.NewFailureResult(fmt.Sprintf(
			"Invalid parameters for %s: the data must be a JSON object like %s. (Underlying error: %v)",
			name, a.expectedParamsHint(), err))
	}
	if reason := a.missingParamsReason(params); reason != "" {
		return nil, neuro.NewFailureResult(reason)
	}
	if reason := validateActionParameters(a.spec, params); reason != "" {
		return nil, neuro.NewFailureResult(reason)
	}

	executeNow := getBoolParam(params, "execute_now", true)
	clearAfter := getBoolParam(params, "clear_after", true)

	if a.spec.Kind == actionKindGame {
		return a.handleGameAction(params)
	}

	if a.spec.Name == CmdShellCommand {
		return a.handleShellCommand(params)
	}

	if a.spec.Name == CmdDesktopGuide {
		topic, _ := params["topic"].(string)
		return nil, a.integration.desktopGuide(topic)
	}

	if a.spec.Name == CmdResetControls {
		// Like every input action: accept now, do the work in Execute, and
		// report the outcome as context (see Execute).
		a.integration.audit.record("action", map[string]interface{}{
			"action": name, "decision": "accepted",
		})
		return pendingWork{reset: true}, neuro.NewSuccessResult("accepted")
	}

	// Fast in-process actions: answer Neuro immediately (API best practice).
	switch a.spec.Name {
	case EnableLLControls:
		if err := a.integration.switchActionMode(false, true); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, neuro.NewSuccessResult("Enabled low-level controls")

	case DisableLLControls:
		if err := a.integration.switchActionMode(true, false); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, neuro.NewSuccessResult("Enabled high-level controls")

	case CmdListCatalogItems:
		return nil, a.integration.listCatalogItems()
	case CmdFindCatalogItems:
		query, _ := params["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return nil, neuro.NewFailureResult("query is required")
		}
		limit := 5
		if rawLimit, ok := params["limit"].(float64); ok {
			limit = int(rawLimit)
		}
		return nil, a.integration.findCatalogItems(query, limit)
	case CmdGetCatalogItem:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		return nil, a.integration.getCatalogItem(itemID)
	case CmdGetDesktopContext, CmdSendDesktopContext:
		capture := getBoolParam(params, "capture_screenshot", false)
		permit, err := a.integration.observationPermit(a.spec.Name)
		if err == nil && capture {
			err = permit.requireVisionService()
		}
		if err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return pendingWork{desktopContext: &desktopContextRequest{
			capture: capture, silent: getBoolParam(params, "silent", true), permit: permit,
		}}, neuro.NewSuccessResult("accepted; the desktop context follows when ready")
	case CmdListInstalledExts:
		return nil, a.integration.listInstalledExtensions()
	case CmdMarketplaceSearch:
		query, _ := params["query"].(string)
		return nil, a.integration.marketplaceSearch(query)
	case CmdStageIntegration:
		id, _ := params["item_id"].(string)
		id, err := normalizeExtensionID(id)
		if err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		if err := a.integration.checkExtensionManagement(id); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return pendingWork{extension: &extensionJob{op: "stage", id: id}}, neuro.NewSuccessResult("accepted: staging " + id + "; the outcome arrives as a message")
	case CmdViewerVisibility:
		id, _ := params["integration_id"].(string)
		actor, _ := params["actor"].(string)
		visible, visibleOK := params["visible"].(bool)
		minutes := 15
		if value, exists := params["minutes"]; exists {
			number, ok := value.(float64)
			if !ok || number < 1 || number > 60 || number != float64(int(number)) {
				return nil, neuro.NewFailureResult("minutes must be an integer from 1 through 60")
			}
			minutes = int(number)
		}
		if _, err := normalizeExtensionID(id); err != nil || (actor != "neuro" && actor != "evil") || !visibleOK {
			return nil, neuro.NewFailureResult("integration_id, actor (neuro or evil), and visible (boolean) are required")
		}
		if err := a.integration.setViewerVisibility(id, actor, visible, minutes); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, neuro.NewSuccessResult(fmt.Sprintf("Viewer visibility for %s (%s) is %t", id, actor, visible))
	case CmdRequestPermission:
		// Filing a request never changes the policy; the decision comes later
		// as a message from the operator.
		scope, _ := params["scope"].(string)
		reason, _ := params["reason"].(string)
		minutes := int(numericParam(params, "minutes"))
		return nil, a.integration.requestPermission(scope, reason, minutes, "neuro")

	case CmdInstallExtension:
		// An install can fetch code and take longer than the action window, so
		// it runs in Execute, after the acknowledgement. Only the id is checked here.
		itemID, _ := params["item_id"].(string)
		itemID = strings.ToLower(strings.TrimSpace(itemID))
		if _, err := normalizeExtensionID(itemID); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		if err := a.integration.checkExtensionManagement(itemID); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		a.integration.audit.record("action", map[string]interface{}{
			"action": name, "decision": "accepted", "item": itemID,
		})
		return pendingWork{extension: &extensionJob{op: "install", id: itemID}},
			neuro.NewSuccessResult("accepted: installing " + itemID + ". The outcome arrives as a message when it finishes.")
	case CmdUninstallExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		if err := a.integration.checkExtensionManagement(itemID); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, a.integration.uninstallExtension(itemID)
	case CmdEnableExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		if err := a.integration.checkExtensionManagement(itemID); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, a.integration.setExtensionEnabled(itemID, true)
	case CmdDisableExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		if err := a.integration.checkExtensionManagement(itemID); err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, a.integration.setExtensionEnabled(itemID, false)
	}

	// A script can contain multiple capabilities. Validate every line before
	// acknowledging it, with the same token grammar as the Python executor.
	if a.spec.Name == CmdRunScript {
		script, _ := params["script"].(string)
		requirements, err := validateScript(script)
		if err == nil {
			err = requirements.check(a.integration.policy())
		}
		if err != nil {
			a.integration.stats.noteDenied(name)
			return nil, neuro.NewFailureResult(err.Error())
		}
	}

	// Desktop intents → validated now, executed after action/result (best practice).
	scriptIntent := scriptIntentFor(a.spec.Name)
	if scriptIntent != "" {
		return pendingWork{scriptIntent: scriptIntent}, neuro.NewSuccessResult("accepted")
	}

	cmd, err := buildIPCCommand(a.spec.Name, params, executeNow, clearAfter)
	if err != nil {
		return nil, neuro.NewFailureResult(err.Error())
	}

	a.integration.audit.record("action", map[string]interface{}{
		"action": name, "decision": "accepted",
	})

	return pendingWork{cmd: &cmd}, neuro.NewSuccessResult("accepted")
}

type desktopContextRequest struct {
	capture bool
	silent  bool
	permit  *observationPermit
}

type pendingWork struct {
	desktopContext *desktopContextRequest
	game           *gameActionRequest
	cmd            *IPCCommand
	scriptIntent   string
	// extension is a slow extension operation (an install) run after the ack.
	extension *extensionJob
	// gameCommands is a batch of input primitives produced by one game action
	// (e.g. game_move with steps=3 holds a key three times, in order).
	gameCommands []IPCCommand
	// gameActionName is the Neuro action that produced this work.
	gameActionName string
	// reset is the escape hatch (reset_controls): clear queued inputs, then
	// release every held key and button.
	reset bool
}

func scriptIntentFor(name CommandType) string {
	switch name {
	case CmdOpenWindowsMenu:
		return "OPEN_WINDOWS_MENU"
	case CmdShowDesktop:
		return "SHOW_DESKTOP"
	case CmdMinimizeAll:
		return "MINIMIZE_ALL_WINDOWS"
	case CmdCloseForeground:
		return "CLOSE_FOREGROUND_APP"
	case CmdOpenTaskManager:
		return "OPEN_TASK_MANAGER"
	case CmdCloseAllApps:
		return "CLOSE_ALL_APPS"
	case CmdOpenExplorer:
		return "OPEN_FILE_EXPLORER"
	case CmdOpenRunDialog:
		return "OPEN_RUN_DIALOG"
	case CmdOpenSearch:
		return "OPEN_SEARCH"
	case CmdSnapWindowLeft:
		return "SNAP_WINDOW_LEFT"
	case CmdSnapWindowRight:
		return "SNAP_WINDOW_RIGHT"
	case CmdOpenSettings:
		return "OPEN_WINDOWS_SETTINGS"
	case CmdOpenNotification:
		return "OPEN_NOTIFICATION_CENTER"
	case CmdOpenClipboard:
		return "OPEN_CLIPBOARD_HISTORY"
	case CmdLockWorkstation:
		return "LOCK_WORKSTATION"
	case CmdSwitchAppNext:
		return "SWITCH_APP_NEXT"
	case CmdSwitchAppPrevious:
		return "SWITCH_APP_PREVIOUS"
	case CmdOpenPowerUserMenu:
		return "OPEN_POWER_USER_MENU"
	case CmdTakeScreenSnip:
		return "TAKE_SCREEN_SNIP"
	default:
		return ""
	}
}

func (a *IPCProxyAction) Execute(state interface{}) {
	work, ok := state.(pendingWork)
	if !ok {
		return
	}

	if err := a.executionAllowed(); err != nil {
		a.reportOutcome(neuro.NewFailureResult(err.Error()))
		return
	}
	var result neuro.ExecutionResult
	switch {
	case work.desktopContext != nil:
		request := work.desktopContext
		message, err := a.integration.getDesktopContextGuarded(request.capture, request.permit)
		if err == nil {
			err = request.permit.check()
		}
		if err != nil {
			a.reportOutcome(neuro.NewFailureResult(err.Error()))
			return
		}
		if a.integration.client != nil {
			_ = a.integration.client.SendContext(boundedContextMessage(message), request.silent)
		}
		return
	case work.game != nil:
		result = a.executeGameAction(work.game)
		a.reportOutcome(result)
		return
	case work.extension != nil:
		a.integration.runExtensionJob(work.extension)
		return
	case len(work.gameCommands) > 0:
		result = a.executeGameCommands(work)
	case work.scriptIntent != "":
		result = a.integration.executeScriptIntent(work.scriptIntent)
	case work.reset:
		result = a.integration.resetControls()
	case work.cmd != nil:
		resp, err := a.integration.sendToExecutorForAction(*work.cmd, a.GetName())
		if err != nil {
			result = neuro.NewFailureResult(fmt.Sprintf("executor error: %v", err))
		} else if !resp.Success {
			message := resp.Error
			if message == "" {
				message = "Command failed"
			}
			result = neuro.NewFailureResult(message)
		} else if output, ok := resp.Data["output"].(string); ok && strings.TrimSpace(output) != "" {
			if err := a.executionAllowed(); err != nil {
				result = neuro.NewFailureResult(err.Error())
			} else {
				result = neuro.NewSuccessResult("Output:\n```text\n" + boundedText(strings.TrimSpace(output), 1600) + "\n```")
			}
		} else {
			result = neuro.NewSuccessResult("Command completed")
		}

	default:
		return
	}

	// The action result acknowledged admission, so deliver the actual outcome.
	a.reportOutcome(result)
}

// Recheck current authority after waiting in the serial execution queue.
func (a *IPCProxyAction) executionAllowed() error {
	select {
	case <-a.integration.done:
		return fmt.Errorf("Neurcro is shutting down")
	default:
	}
	if reason := a.integration.stop.blockReason(a.GetName()); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if policy := a.integration.policy(); policy != nil && !policy.IsAllowed(a.GetName()) {
		return fmt.Errorf("%s", policyDenialMessage(a.GetName(), policy))
	}
	return nil
}

func (a *IPCProxyAction) reportOutcome(result neuro.ExecutionResult) {
	if !result.Successful {
		a.integration.stats.noteFailure(a.GetName())
	}
	if a.integration.client == nil {
		return
	}
	status := "completed"
	if !result.Successful {
		status = "failed"
	}
	_ = a.integration.client.SendContext(boundedContextMessage(fmt.Sprintf(
		"## %s %s\n\n%s", a.GetName(), status, result.Message)), true)
}

// executeGameCommands runs the input primitives of one game action in order and
// releases everything if any of them fails, so a half-applied input cannot
// leave a key stuck down.
func (a *IPCProxyAction) executeGameCommands(work pendingWork) neuro.ExecutionResult {
	session := a.integration.games.Session()
	guard := func() error {
		if err := a.executionAllowed(); err != nil {
			return err
		}
		current := a.integration.games.Session()
		if session == nil || current == nil || session.ProfileID != current.ProfileID || !session.StartedAt.Equal(current.StartedAt) {
			return fmt.Errorf("game session changed; request a new game action")
		}
		return inputAllowed(a.integration.currentProfile(), current, work.gameActionName, a.integration.relay)
	}
	for _, cmd := range work.gameCommands {
		resp, err := a.integration.sendToExecutorForAction(cmd, work.gameActionName, guard)
		if err != nil {
			a.integration.releaseAllInput()
			return neuro.NewFailureResult(fmt.Sprintf("executor error: %v", err))
		}
		if !resp.Success {
			a.integration.releaseAllInput()
			return neuro.NewFailureResult(nonEmptyOr(resp.Error, "Command failed"))
		}
	}

	if work.gameActionName != "" {
		a.integration.games.recordAction(work.gameActionName)
	}
	return neuro.NewSuccessResult("ok")
}

func getBoolParam(params map[string]interface{}, key string, defaultValue bool) bool {
	val, ok := params[key]
	if !ok {
		return defaultValue
	}
	typed, ok := val.(bool)
	if !ok {
		return defaultValue
	}
	return typed
}

func buildIPCCommand(
	action CommandType,
	params map[string]interface{},
	executeNow bool,
	clearAfter bool,
) (IPCCommand, error) {
	switch action {
	case CmdMouseMove:
		x, xOK := params["x"].(float64)
		y, yOK := params["y"].(float64)
		if !xOK || !yOK {
			return IPCCommand{}, fmt.Errorf("move_mouse_to needs numeric x and y in screen pixels, for example {\"x\": 640, \"y\": 360}")
		}
		return IPCCommand{
			Type: CmdMouseMove,
			Params: map[string]interface{}{
				"x": int(x),
				"y": int(y),
			},
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdMouseClick:
		button := "left"
		if rawButton, ok := params["button"].(string); ok && rawButton != "" {
			button = rawButton
		}
		return IPCCommand{
			Type: CmdMouseClick,
			Params: map[string]interface{}{
				"button": button,
			},
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdTypeText:
		text, ok := params["text"].(string)
		if !ok || text == "" {
			return IPCCommand{}, fmt.Errorf("type_text needs a non-empty \"text\" string, for example {\"text\": \"hello\"}")
		}
		return IPCCommand{
			Type: CmdTypeText,
			Params: map[string]interface{}{
				"text": text,
			},
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdKeyPress:
		key, ok := params["key"].(string)
		if !ok || key == "" {
			return IPCCommand{}, fmt.Errorf("key_press needs a \"key\" name, for example {\"key\": \"enter\"}. Call desktop_guide for the other key names")
		}
		return IPCCommand{
			Type: CmdKeyPress,
			Params: map[string]interface{}{
				"key": key,
			},
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdRunScript:
		script, ok := params["script"].(string)
		if !ok || script == "" {
			return IPCCommand{}, fmt.Errorf("run_script needs a non-empty \"script\" string. Call desktop_guide if you are unsure what to put in it")
		}
		return IPCCommand{
			Type: CmdRunScript,
			Params: map[string]interface{}{
				"script": script,
			},
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdExecuteQueue:
		return IPCCommand{
			Type:       CmdExecuteQueue,
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil

	case CmdShellCommand:
		command, ok := params["command"].(string)
		if !ok || strings.TrimSpace(command) == "" {
			return IPCCommand{}, fmt.Errorf("shell_command needs a non-empty \"command\" string, for example {\"command\": \"echo hello\"}. Call desktop_guide with topic shell for the rules")
		}
		cmdParams := map[string]interface{}{
			"command": strings.TrimSpace(command),
			"timeout": shellTimeoutSeconds(numericParam(params, "timeout")),
		}
		if cwd, _ := params["cwd"].(string); strings.TrimSpace(cwd) != "" {
			cmdParams["cwd"] = strings.TrimSpace(cwd)
		} else if cwd, err := shellCWD(); err == nil && cwd != "" {
			cmdParams["cwd"] = cwd
		}
		// The server owns the shell policy. The agent receives the effective
		// lists with every command, so it never needs its own copy of the
		// environment to agree with this one.
		allowlist, _, _, _ := loadShellRules()
		cmdParams["allowlist"] = append([]string{}, allowlist...)
		cmdParams["denylist"] = splitListEnv("NEURO_SHELL_DENYLIST")
		return IPCCommand{
			Type:   CmdShellCommand,
			Params: cmdParams,
		}, nil

	case CmdClearActionQueue:
		return IPCCommand{
			Type:       CmdClearActionQueue,
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil
	}

	return IPCCommand{}, fmt.Errorf("unknown action %q. Call desktop_guide to see the actions that are available now", action)
}

// numericParam reads a JSON number (or numeric string) parameter.
func numericParam(params map[string]interface{}, key string) float64 {
	switch value := params[key].(type) {
	case float64:
		return value
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

// handleShellCommand applies the shell firewall, then queues the command.
//
// The Python executor checks the same rules again: a watcher or a hand-written
// script must not be able to skip this gate.
func (a *IPCProxyAction) handleShellCommand(params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	command, _ := params["command"].(string)
	if err := checkShellCommand(command); err != nil {
		return nil, neuro.NewFailureResult(err.Error())
	}

	cmd, err := buildIPCCommand(CmdShellCommand, params, true, true)
	if err != nil {
		return nil, neuro.NewFailureResult(err.Error())
	}

	// The command line itself is the interesting part of a shell audit entry.
	a.integration.audit.record("action", map[string]interface{}{
		"action": string(CmdShellCommand), "decision": "accepted", "command": command,
	})

	return pendingWork{cmd: &cmd}, neuro.NewSuccessResult("accepted")
}

// expectedParamsHint renders the action's schema properties so a weak model that
// sent the wrong shape is told the right one instead of just "invalid".
func (a *IPCProxyAction) expectedParamsHint() string {
	if a.spec.Schema != nil && len(a.spec.Schema.Properties) > 0 {
		keys := make([]string, 0, len(a.spec.Schema.Properties))
		for key := range a.spec.Schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		hint := "{" + strings.Join(keys, ", ") + "}"
		if len(a.spec.Schema.Required) > 0 {
			hint += " (required: " + strings.Join(a.spec.Schema.Required, ", ") + ")"
		}
		return hint
	}
	return "{} (this action takes no parameters)"
}

// missingParamsReason reports empty required parameters with their names, which
// is the single most common way a small model fails a call.
func (a *IPCProxyAction) missingParamsReason(params map[string]interface{}) string {
	if a.spec.Schema == nil {
		return ""
	}
	for _, key := range a.spec.Schema.Required {
		value, present := params[key]
		if !present || value == nil {
			return fmt.Sprintf("%s needs the parameter %q. Call it again with %s.",
				a.spec.Name, key, a.expectedParamsHint())
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			return fmt.Sprintf("%s received an empty %q. Call it again with a value, e.g. %s.",
				a.spec.Name, key, a.expectedParamsHint())
		}
	}
	return ""
}
