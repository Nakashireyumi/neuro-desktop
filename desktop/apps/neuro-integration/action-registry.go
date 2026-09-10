package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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
	CmdGetStatus           CommandType = "get_status"
	CmdRunScript           CommandType = "run_script"
	CmdExecuteQueue        CommandType = "execute_queue"
	CmdClearActionQueue    CommandType = "clear_action_queue"
	CmdShutdownGracefully  CommandType = "shutdown_gracefully"
	CmdShutdownImmediately CommandType = "shutdown_immediately"
)

var (
	RegisterHLActionsOnStartup bool = false
	RegisterLLActionsOnStartup bool = true

	currentActionList   = map[string]neuro.ActionHandler{}
	currentActionListMu sync.Mutex
	actionModeMu        sync.Mutex
)

type actionSpec struct {
	Name        CommandType
	Description string
	Schema      *neuro.ActionSchema
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
	{
		Name:        CmdListCatalogItems,
		Description: "List available catalog integrations/apps configured for Neuro Desktop",
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
		Name:        CmdGetDesktopContext,
		Description: "Collect current desktop context snapshot (window/process/action history + optional screenshot path)",
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
		Description: "Collect desktop context and send it to Neuro as a context message",
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
	{
		Name:        CmdListInstalledExts,
		Description: "List installed/managed Neuro Desktop extensions",
		Schema:      nil,
	},
	{
		Name:        CmdInstallExtension,
		Description: "Install an extension from the catalog (supports metadata-only or git-clone mode)",
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

var LLActionSpecs = []actionSpec{
	{
		Name:        DisableLLControls,
		Description: "Disable low-level controls and re-enable high-level controls",
		Schema:      nil,
	},
	{
		Name:        CmdMouseMove,
		Description: "Move mouse cursor to specific coordinates",
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
		Description: "Click mouse button at current cursor position",
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
		Description: "Type text using keyboard",
		Schema: neuro.WrapSchema(map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"maxLength":   1000,
				"description": "Text to type",
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
		Description: "Press a specific keyboard key",
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
		Description: "Clear queued actions",
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
	if a.integration.permissions != nil && !a.integration.permissions.IsAllowed(a.GetName()) {
		return nil, neuro.NewFailureResult(fmt.Sprintf("Action denied by policy: %s", a.GetName()))
	}

	params := map[string]interface{}{}
	if err := neuro.ParseActionData(data, &params); err != nil {
		return nil, neuro.NewFailureResult("Invalid action parameters")
	}

	executeNow := getBoolParam(params, "execute_now", true)
	clearAfter := getBoolParam(params, "clear_after", true)

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
	case CmdGetDesktopContext:
		capture := getBoolParam(params, "capture_screenshot", false)
		contextMessage, err := a.integration.getDesktopContext(capture)
		if err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, neuro.NewSuccessResult(contextMessage)
	case CmdSendDesktopContext:
		capture := getBoolParam(params, "capture_screenshot", false)
		silent := getBoolParam(params, "silent", true)
		return nil, a.integration.sendDesktopContext(capture, silent)
	case CmdListInstalledExts:
		return nil, a.integration.listInstalledExtensions()
	case CmdInstallExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		return nil, a.integration.installExtension(itemID)
	case CmdUninstallExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		return nil, a.integration.uninstallExtension(itemID)
	case CmdEnableExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		return nil, a.integration.setExtensionEnabled(itemID, true)
	case CmdDisableExtension:
		itemID, _ := params["item_id"].(string)
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return nil, neuro.NewFailureResult("item_id is required")
		}
		return nil, a.integration.setExtensionEnabled(itemID, false)
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

	return pendingWork{cmd: &cmd}, neuro.NewSuccessResult("accepted")
}

type pendingWork struct {
	cmd          *IPCCommand
	scriptIntent string
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

	var result neuro.ExecutionResult
	if work.scriptIntent != "" {
		result = a.integration.executeScriptIntent(work.scriptIntent)
	} else if work.cmd != nil {
		resp, err := a.integration.sendToRust(*work.cmd)
		if err != nil {
			result = neuro.NewFailureResult(fmt.Sprintf("executor error: %v", err))
		} else if !resp.Success {
			message := resp.Error
			if message == "" {
				message = "Command failed"
			}
			result = neuro.NewFailureResult(message)
		} else {
			return
		}
	} else {
		return
	}

	// Execution finished after action/result was already sent. Tell Neuro via context.
	if !result.Successful {
		_ = a.integration.client.SendContext(
			fmt.Sprintf("## Action execution failed\n\n- action: `%s`\n- error: %s", a.GetName(), result.Message),
			true,
		)
	}
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
			return IPCCommand{}, fmt.Errorf("x and y are required numbers")
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
			return IPCCommand{}, fmt.Errorf("text is required")
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
			return IPCCommand{}, fmt.Errorf("key is required")
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
			return IPCCommand{}, fmt.Errorf("script is required")
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

	case CmdClearActionQueue:
		return IPCCommand{
			Type:       CmdClearActionQueue,
			ExecuteNow: executeNow,
			ClearAfter: clearAfter,
		}, nil
	}

	return IPCCommand{}, fmt.Errorf("unknown action: %s", action)
}
