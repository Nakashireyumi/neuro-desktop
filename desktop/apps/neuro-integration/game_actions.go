package main

import (
	"fmt"
	"log"
	"sort"
	"strings"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// gameActionNames is the fixed vocabulary of high-level game actions exposed to
// Neuro. Directions and action names are neutral; each game profile maps them
// onto its own keybinds so Neuro does not need per-game knowledge.
var gameMoveDirections = []string{"forward", "back", "left", "right", "up", "down"}

var gameActionVocabulary = []string{
	"jump", "attack", "use", "reload", "crouch", "sprint", "interact",
	"inventory", "map", "pause", "confirm", "cancel", "swap", "chat", "drop", "special",
}

func gameActionSpecs() []actionSpec {
	moveEnum := make([]interface{}, 0, len(gameMoveDirections))
	for _, direction := range gameMoveDirections {
		moveEnum = append(moveEnum, direction)
	}
	actionEnum := make([]interface{}, 0, len(gameActionVocabulary))
	for _, name := range gameActionVocabulary {
		actionEnum = append(actionEnum, name)
	}

	return []actionSpec{
		{
			Name:        CmdGameListProfiles,
			Kind:        actionKindGame,
			Description: "List the game profiles Neuro Desktop can interface with, and how each one is controlled",
			Schema:      nil,
		},
		{
			Name:        CmdGameDetect,
			Kind:        actionKindGame,
			Description: "Look at what is running on the controlled machine and report which game profile matches (does not start a session)",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"refresh": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "Ask the executor for a fresh window/process snapshot instead of using the cache",
				},
			}, nil),
		},
		{
			Name:        CmdGameStartSession,
			Kind:        actionKindGame,
			Description: "Start playing a game: detect it (or use profile_id), send yourself the control map, and allow game input",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"profile_id": map[string]interface{}{
					"type":        "string",
					"description": "Game profile id, e.g. minecraft. Omit to auto-detect the running game",
				},
				"launch": map[string]interface{}{
					"type":        "boolean",
					"default":     false,
					"description": "Also run the profile's launch command (requires the system permission scope)",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"auto", "nd", "external", "hybrid"},
					"description": "Override the profile's control mode for this session. auto hands the game to the dedicated integration when it is connected, otherwise Neuro Desktop drives it",
				},
			}, nil),
		},
		{
			Name:        CmdGameEndSession,
			Kind:        actionKindGame,
			Description: "Stop the current game session and release any keys/buttons Neuro Desktop is holding",
			Schema:      nil,
		},
		{
			Name:        CmdGameStatus,
			Kind:        actionKindGame,
			Description: "Report the active game session, control mode, and which input actions are currently allowed",
			Schema:      nil,
		},
		{
			Name:        CmdGameMove,
			Kind:        actionKindGame,
			Description: "Move in the game by holding a movement direction (uses the profile's keybinds)",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        moveEnum,
					"description": "Movement direction",
				},
				"seconds": map[string]interface{}{
					"type":        "number",
					"description": "How long to hold the direction (defaults to the profile's move_hold_seconds)",
				},
				"steps": map[string]interface{}{
					"type":        "integer",
					"description": "Repeat the hold this many times (1-10)",
				},
			}, []string{"direction"}),
		},
		{
			Name:        CmdGameLook,
			Kind:        actionKindGame,
			Description: "Turn the in-game camera with a relative mouse movement (mouse-look games)",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"dx": map[string]interface{}{
					"type":        "integer",
					"description": "Horizontal pixels: positive turns right, negative turns left",
				},
				"dy": map[string]interface{}{
					"type":        "integer",
					"description": "Vertical pixels: positive looks down, negative looks up",
				},
				"duration": map[string]interface{}{
					"type":        "number",
					"description": "Seconds to spread the movement over (default 0.05)",
				},
			}, []string{"dx", "dy"}),
		},
		{
			Name:        CmdGameAction,
			Kind:        actionKindGame,
			Description: "Trigger a named game action (jump, attack, use, inventory, ...) using the profile's keybinds",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        actionEnum,
					"description": "Which action to perform",
				},
				"seconds": map[string]interface{}{
					"type":        "number",
					"description": "Hold the action for this long instead of tapping it (e.g. sustained fire)",
				},
			}, []string{"action"}),
		},
		{
			Name:        CmdGamePress,
			Kind:        actionKindGame,
			Description: "Press a raw key or key combination in the game (escape hatch when no named action fits)",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Single key, e.g. \"e\"",
				},
				"keys": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Combination, e.g. [\"ctrl\", \"1\"]",
				},
				"seconds": map[string]interface{}{
					"type":        "number",
					"description": "Hold for this long instead of tapping",
				},
			}, nil),
		},
		{
			Name:        CmdGameReleaseAll,
			Kind:        actionKindGame,
			Description: "Safety: release every key and mouse button Neuro Desktop is holding",
			Schema:      nil,
		},
		{
			Name:        CmdGameObserve,
			Kind:        actionKindGame,
			Description: "Take a screenshot of the game and (if a vision server is configured) summarise what is on screen",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"vision": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "Ask the configured vision server to describe the frame",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Optional question for the vision server",
				},
			}, nil),
		},
		{
			Name:        CmdGameLaunch,
			Kind:        actionKindGame,
			Description: "Launch the game via the profile's launch command (requires the system permission scope)",
			Schema: neuro.WrapSchema(map[string]interface{}{
				"profile_id": map[string]interface{}{
					"type":        "string",
					"description": "Profile whose launch command should run",
				},
			}, []string{"profile_id"}),
		},
	}
}

// handleGameAction implements every game_* action. Read-only actions answer
// immediately; anything that touches the machine returns pendingWork so the
// action is acknowledged before the desktop work happens.
func (a *IPCProxyAction) handleGameAction(params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	integration := a.integration

	switch a.spec.Name {
	case CmdGameListProfiles:
		return nil, integration.listGameProfiles()

	case CmdGameDetect:
		refresh := getBoolParam(params, "refresh", true)
		detected, err := integration.detectGame(refresh)
		if err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		return nil, neuro.NewSuccessResult(describeDetection(detected))

	case CmdGameStatus:
		return nil, neuro.NewSuccessResult(integration.describeGameStatus())

	case CmdGameStartSession:
		profileID := strings.TrimSpace(stringParam(params, "profile_id"))
		launch := getBoolParam(params, "launch", false)
		modeOverride := strings.ToLower(strings.TrimSpace(stringParam(params, "mode")))
		return nil, integration.startGameSession(profileID, launch, modeOverride)

	case CmdGameEndSession:
		return nil, integration.endGameSession("requested by Neuro")

	case CmdGameReleaseAll:
		return nil, integration.releaseAllInput()

	case CmdGameObserve:
		request := &gameObserveRequest{
			vision:            getBoolParam(params, "vision", true),
			prompt:            strings.TrimSpace(stringParam(params, "prompt")),
			recordActionAs:    string(a.spec.Name),
			recordActionNamed: string(a.spec.Name),
		}
		return pendingWork{gameObserve: request}, neuro.NewSuccessResult("accepted")

	case CmdGameMove:
		return integration.buildGameMove(a, params)

	case CmdGameLook:
		return integration.buildGameLook(a, params)

	case CmdGameAction:
		return integration.buildGameAction(a, params)

	case CmdGamePress:
		return integration.buildGamePress(a, params)

	case CmdGameLaunch:
		return integration.buildGameLaunch(a, params)
	}

	return nil, neuro.NewFailureResult(fmt.Sprintf("Unhandled game action: %s", a.spec.Name))
}

func stringParam(params map[string]interface{}, key string) string {
	value, _ := params[key].(string)
	return value
}

func numberParam(params map[string]interface{}, key string) (float64, bool) {
	value, ok := params[key].(float64)
	return value, ok
}

func intParam(params map[string]interface{}, key string) (int, bool) {
	value, ok := params[key].(float64)
	if !ok {
		return 0, false
	}
	return int(value), true
}

func stringSliceParam(params map[string]interface{}, key string) []string {
	raw, ok := params[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func (n *NDIntegration) listGameProfiles() neuro.ExecutionResult {
	if n.games == nil || len(n.games.Profiles()) == 0 {
		return neuro.NewSuccessResult(
			"No game profiles are configured. Add JSON profiles to the game profile directory (NEURO_GAME_PROFILES_DIR, default catalog/games).")
	}

	lines := make([]string, 0)
	for _, profile := range n.games.Profiles() {
		owner := ""
		if profile.Control.Mode != ControlModeND {
			owner = fmt.Sprintf(", owner=%s", nonEmptyOr(profile.Control.ExternalName, profile.ID))
		}
		lines = append(lines, fmt.Sprintf(
			"%s (%s) — control=%s%s — keys=%d",
			profile.ID, profile.Name, profile.Control.Mode, owner, len(profile.Keys)))
	}
	return neuro.NewSuccessResult("Game profiles -> " + strings.Join(lines, " | "))
}

func describeDetection(detected GameDetected) string {
	parts := []string{}
	if detected.ActiveTitle != "" {
		parts = append(parts, fmt.Sprintf("active window: %s", detected.ActiveTitle))
	}
	if detected.Profile == nil {
		parts = append(parts, "no game profile matched (use game_list_profiles, or game_start_session with an explicit profile_id)")
		return "game_detect -> " + strings.Join(parts, " ; ")
	}
	parts = append(parts, fmt.Sprintf("matched profile: %s (%s)", detected.Profile.ID, detected.Profile.Name))
	if detected.MatchedOn != "" {
		parts = append(parts, "matched on "+detected.MatchedOn)
	}
	parts = append(parts, fmt.Sprintf("control mode: %s", detected.Profile.Control.Mode))
	return "game_detect -> " + strings.Join(parts, " ; ")
}

func (n *NDIntegration) describeGameStatus() string {
	session := n.games.Session()
	if session == nil {
		detected, err := n.detectGame(false)
		if err != nil {
			return "No game session is active."
		}
		if detected.Profile != nil {
			return fmt.Sprintf(
				"No game session is active. Detected %s (%s) via %s — call game_start_session to begin.",
				detected.Profile.Name, detected.Profile.ID, nonEmptyOr(detected.MatchedOn, "auto-detect"))
		}
		return "No game session is active, and no game profile matches the current desktop."
	}

	profile, _ := n.games.ProfileByID(session.ProfileID)
	allowed := "input allowed (Neuro Desktop drives this game)"
	if profile != nil {
		switch session.ControlMode {
		case ControlModeExternal:
			allowed = fmt.Sprintf("input delegated to the %s integration", nonEmptyOr(session.ExternalName, session.ProfileID))
		case ControlModeHybrid:
			allowed = fmt.Sprintf("hybrid: Neuro Desktop may only use %s", strings.Join(profile.Control.NDActions, ", "))
		}
	}

	return fmt.Sprintf(
		"Active session: %s (%s) since %s, control=%s, %s, actions issued=%d, last=%s",
		session.ProfileName, session.ProfileID,
		session.StartedAt.Format("15:04:05Z"),
		session.ControlMode, allowed,
		session.ActionsIssued, nonEmptyOr(session.LastAction, "none"))
}

// detectGame snapshots the controlled machine and matches it to a profile.
func (n *NDIntegration) detectGame(refresh bool) (GameDetected, error) {
	permit, err := n.observationPermit(CmdGameDetect)
	if err != nil {
		return GameDetected{}, err
	}
	return n.detectGameGuarded(refresh, permit)
}

func (n *NDIntegration) detectGameGuarded(refresh bool, permit *observationPermit) (GameDetected, error) {
	if err := permit.check(); err != nil {
		return GameDetected{}, err
	}
	if !refresh {
		if cached, ok := n.games.cachedDetection(permit); ok {
			return cached, nil
		}
	}

	if n.games == nil {
		return GameDetected{}, fmt.Errorf("no game profile registry loaded")
	}

	status, err := n.fetchDesktopStatusGuarded(false, permit.check)
	if err != nil {
		return GameDetected{}, fmt.Errorf("could not inspect the controlled machine: %w", err)
	}
	if err := permit.check(); err != nil {
		return GameDetected{}, err
	}

	activeWindow, _ := status["active_window"].(string)
	processes := toStringSlice(status["running_processes"])

	detected := n.games.Detect(activeWindow, processes)
	n.games.setDetection(detected, permit)
	if err := permit.check(); err != nil {
		return GameDetected{}, err
	}
	return detected, nil
}

// resolveAutoMode decides who drives a game when the profile says "auto".
func (n *NDIntegration) resolveAutoMode(profile *GameProfile) GameControlMode {
	if n.adapterOwnsGame(profile) {
		return ControlModeExternal
	}
	owner := strings.ToLower(strings.TrimSpace(profile.Control.ExternalName))
	if owner == "" || n.relay == nil {
		return ControlModeND
	}

	peers := n.relay.status().Peers
	for name := range peers {
		candidate := strings.ToLower(strings.TrimSpace(name))
		if candidate == owner || strings.Contains(candidate, owner) || strings.Contains(owner, candidate) {
			log.Printf("Game %s: %q is connected through the relay, so input stays with it", profile.ID, name)
			return ControlModeExternal
		}
	}
	return ControlModeND
}

func (n *NDIntegration) startGameSession(profileID string, launch bool, modeOverride string) neuro.ExecutionResult {
	session := n.games.Session()

	var profile *GameProfile

	if profileID != "" {
		found, ok := n.games.ProfileByID(profileID)
		if !ok {
			return neuro.NewFailureResult(fmt.Sprintf(
				"Unknown game profile %q. Use game_list_profiles to see what is configured.", profileID))
		}
		profile = found
	} else if session != nil {
		found, ok := n.games.ProfileByID(session.ProfileID)
		if ok {
			profile = found
		}
	}

	if profile == nil {
		detected, err := n.detectGame(true)
		if err != nil {
			return neuro.NewFailureResult(err.Error())
		}
		if detected.Profile == nil {
			return neuro.NewFailureResult(
				"No game profile matches the current desktop. Use game_list_profiles, or pass profile_id explicitly.")
		}
		profile = detected.Profile
	}

	if launch {
		if err := n.launchGame(profile); err != nil {
			return neuro.NewFailureResult(err.Error())
		}
	}

	mode := profile.Control.Mode
	if mode == ControlModeAuto {
		mode = n.resolveAutoMode(profile)
	}
	if modeOverride != "" {
		switch GameControlMode(modeOverride) {
		case ControlModeND, ControlModeExternal, ControlModeHybrid:
			mode = GameControlMode(modeOverride)
		case ControlModeAuto:
			// "auto" is a valid operator choice: hand the game to the dedicated
			// integration when it is connected, otherwise drive it here.
			mode = n.resolveAutoMode(profile)
		default:
			return neuro.NewFailureResult(fmt.Sprintf(
				"Unknown control mode %q (use auto, nd, external or hybrid)", modeOverride))
		}
	}

	started := n.games.start(*profile, mode)

	// Tell Neuro how to play, and keep the message in context so later actions
	// are still grounded in the control map.
	context := profile.GameControlSummary()
	context += fmt.Sprintf("\n\n_Session started %s. Neuro Desktop will report input failures as context._",
		started.StartedAt.Format("15:04:05Z"))
	if err := n.client.SendContext(context, true); err != nil {
		log.Printf("Failed to send game control map context: %v", err)
	}

	log.Printf("Game session started: %s (control=%s)", profile.ID, started.ControlMode)

	summary := fmt.Sprintf("Playing %s. Control mode: %s.", profile.Name, started.ControlMode)
	if started.ControlMode == ControlModeExternal {
		summary = fmt.Sprintf(
			"Observing %s. Input belongs to the %s integration — game_move/game_action/game_press will be refused.",
			profile.Name, nonEmptyOr(profile.Control.ExternalName, profile.ID))
	}
	return neuro.NewSuccessResult(summary)
}

func (n *NDIntegration) endGameSession(reason string) neuro.ExecutionResult {
	previous := n.games.end()
	if previous == nil {
		return neuro.NewSuccessResult("No game session was active.")
	}

	// Never leave keys held after a session ends.
	if _, err := n.sendToExecutor(IPCCommand{
		Type:       CmdKeyReleaseAll,
		ExecuteNow: true,
		ClearAfter: false,
	}); err != nil {
		log.Printf("Warning: could not release held input on session end: %v", err)
	}

	_ = n.client.SendContext(fmt.Sprintf(
		"## Game session ended\n\n- Game: %s\n- Reason: %s\n- Actions issued: %d",
		previous.ProfileName, nonEmptyOr(reason, "unspecified"), previous.ActionsIssued), true)

	log.Printf("Game session ended: %s (%s)", previous.ProfileID, reason)
	return neuro.NewSuccessResult(fmt.Sprintf("Stopped playing %s.", previous.ProfileName))
}

func (n *NDIntegration) releaseAllInput() neuro.ExecutionResult {
	resp, err := n.sendToExecutor(IPCCommand{
		Type:       CmdKeyReleaseAll,
		ExecuteNow: true,
		ClearAfter: true,
	})
	if err != nil {
		return neuro.NewFailureResult(fmt.Sprintf("Could not release input: %v", err))
	}
	if !resp.Success {
		return neuro.NewFailureResult(nonEmptyOr(resp.Error, "Could not release input"))
	}
	return neuro.NewSuccessResult("Released all held keys and mouse buttons.")
}

// activeGameContext is appended to periodic desktop context so Neuro keeps
// track of which game it is playing.
func (n *NDIntegration) activeGameContext() string {
	if n.games == nil {
		return ""
	}
	session := n.games.Session()
	if session == nil {
		return ""
	}
	return fmt.Sprintf("## Active game session\n\n- Game: %s (%s)\n- Control mode: %s\n- Actions issued: %d\n- Last action: %s",
		session.ProfileName, session.ProfileID, session.ControlMode, session.ActionsIssued,
		nonEmptyOr(session.LastAction, "none"))
}

// runningGameSummary lists any profile matching the current desktop, so a
// session can be started without an extra detection round trip.
func (n *NDIntegration) runningGameSummary(status map[string]interface{}) string {
	if n.games == nil || len(n.games.Profiles()) == 0 {
		return ""
	}
	activeWindow, _ := status["active_window"].(string)
	detected := n.games.Detect(activeWindow, toStringSlice(status["running_processes"]))
	if detected.Profile == nil {
		return ""
	}
	return fmt.Sprintf("%s (%s)", detected.Profile.Name, detected.Profile.ID)
}

func (n *NDIntegration) currentProfile() *GameProfile {
	session := n.games.Session()
	if session == nil {
		return nil
	}
	profile, _ := n.games.ProfileByID(session.ProfileID)
	return profile
}

// guardGameInput enforces control-mode arbitration plus the per-session rate
// limit. It returns the active profile when input may proceed.
func (n *NDIntegration) guardGameInput(actionKey string) (*GameProfile, *GameSession, neuro.ExecutionResult, bool) {
	profile := n.currentProfile()
	session := n.games.Session()
	if err := inputAllowed(profile, session, actionKey, n.relay); err != nil {
		return nil, session, neuro.NewFailureResult(err.Error()), false
	}
	if n.games.rateLimited(profile.Control.MaxActionsPerMin) {
		return nil, session, neuro.NewFailureResult(fmt.Sprintf(
			"Rate limit reached (%d game actions per minute for %s). Wait a moment or end the session.",
			profile.Control.MaxActionsPerMin, profile.Name)), false
	}
	return profile, session, neuro.ExecutionResult{}, true
}

func (n *NDIntegration) buildGameMove(a *IPCProxyAction, params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	profile, _, failure, ok := n.guardGameInput("game_move")
	if !ok {
		return nil, failure
	}

	direction := strings.ToLower(strings.TrimSpace(stringParam(params, "direction")))
	if direction == "" {
		return nil, neuro.NewFailureResult("direction is required")
	}

	key, keyed := profile.Keys[direction]
	if !keyed {
		key, keyed = profile.Keys["move_"+direction]
	}
	if !keyed {
		return nil, neuro.NewFailureResult(fmt.Sprintf(
			"%s has no keybind for direction %q. Available: %s",
			profile.Name, direction, strings.Join(sortedKeys(profile.Keys), ", ")))
	}

	seconds := profile.Control.MoveHoldSeconds
	if provided, ok := numberParam(params, "seconds"); ok && provided > 0 {
		seconds = provided
	}
	if seconds > 15 {
		seconds = 15
	}

	steps := 1
	if provided, ok := intParam(params, "steps"); ok && provided > 1 {
		steps = provided
	}
	if steps > 10 {
		steps = 10
	}

	commands := make([]IPCCommand, 0, steps)
	for i := 0; i < steps; i++ {
		commands = append(commands, IPCCommand{
			Type: CmdKeyHoldFor,
			Params: map[string]interface{}{
				"key":     key,
				"seconds": seconds,
			},
			ExecuteNow: true,
			ClearAfter: false,
		})
	}

	n.games.recordAction("move:" + direction)
	return pendingWork{gameCommands: commands, gameActionName: string(a.spec.Name)}, neuro.NewSuccessResult("accepted")
}

func (n *NDIntegration) buildGameLook(a *IPCProxyAction, params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	profile, _, failure, ok := n.guardGameInput("game_look")
	if !ok {
		return nil, failure
	}

	if !profile.Control.MouseLook.Enabled {
		return nil, neuro.NewFailureResult(fmt.Sprintf(
			"%s does not use mouse-look (profile control.mouse_look.enabled is false).", profile.Name))
	}

	dx, dxOK := intParam(params, "dx")
	dy, dyOK := intParam(params, "dy")
	if !dxOK && !dyOK {
		return nil, neuro.NewFailureResult("dx and/or dy are required")
	}

	sensitivity := profile.Control.MouseLook.Sensitivity
	if sensitivity <= 0 {
		sensitivity = 1.0
	}
	maxStep := profile.Control.MouseLook.MaxStep
	if maxStep <= 0 {
		maxStep = 600
	}

	dx = clampInt(int(float64(dx)*sensitivity), -maxStep, maxStep)
	dy = clampInt(int(float64(dy)*sensitivity), -maxStep, maxStep)
	if profile.Control.MouseLook.InvertY {
		dy = -dy
	}

	duration := 0.05
	if provided, ok := numberParam(params, "duration"); ok && provided > 0 {
		duration = provided
	}
	if duration > 2 {
		duration = 2
	}

	n.games.recordAction("look")
	return pendingWork{
		gameCommands: []IPCCommand{{
			Type: CmdMoveMouseRelative,
			Params: map[string]interface{}{
				"dx":       dx,
				"dy":       dy,
				"duration": duration,
			},
			ExecuteNow: true,
			ClearAfter: false,
		}},
		gameActionName: string(a.spec.Name),
	}, neuro.NewSuccessResult("accepted")
}

func (n *NDIntegration) buildGameAction(a *IPCProxyAction, params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	profile, _, failure, ok := n.guardGameInput("game_action")
	if !ok {
		return nil, failure
	}

	actionName := strings.ToLower(strings.TrimSpace(stringParam(params, "action")))
	if actionName == "" {
		return nil, neuro.NewFailureResult("action is required")
	}

	key, keyed := profile.Keys[actionName]
	if !keyed {
		return nil, neuro.NewFailureResult(fmt.Sprintf(
			"%s has no keybind for action %q. Available: %s",
			profile.Name, actionName, strings.Join(sortedKeys(profile.Keys), ", ")))
	}

	seconds, held := numberParam(params, "seconds")
	if held && seconds > 30 {
		seconds = 30
	}

	command, err := gameInputCommand(key, seconds, held)
	if err != nil {
		return nil, neuro.NewFailureResult(err.Error())
	}

	n.games.recordAction("action:" + actionName)
	return pendingWork{
		gameCommands:   []IPCCommand{command},
		gameActionName: string(a.spec.Name),
	}, neuro.NewSuccessResult("accepted")
}

func (n *NDIntegration) buildGamePress(a *IPCProxyAction, params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	profile, _, failure, ok := n.guardGameInput("game_press")
	if !ok {
		return nil, failure
	}

	if profile.Control.AllowRawKeys != nil && !*profile.Control.AllowRawKeys {
		return nil, neuro.NewFailureResult(fmt.Sprintf(
			"Raw key presses are disabled for %s. Use game_action instead.", profile.Name))
	}

	keys := stringSliceParam(params, "keys")
	singleKey := strings.TrimSpace(stringParam(params, "key"))
	if singleKey != "" {
		keys = append([]string{singleKey}, keys...)
	}
	if len(keys) == 0 {
		return nil, neuro.NewFailureResult("Provide key (single) or keys (combination)")
	}

	seconds, held := numberParam(params, "seconds")
	if held && seconds > 30 {
		seconds = 30
	}

	var command IPCCommand
	if len(keys) == 1 {
		built, err := gameInputCommand(keys[0], seconds, held)
		if err != nil {
			return nil, neuro.NewFailureResult(err.Error())
		}
		command = built
	} else {
		command = IPCCommand{
			Type:       CmdKeyCombo,
			Params:     map[string]interface{}{"keys": keys},
			ExecuteNow: true,
			ClearAfter: false,
		}
	}

	n.games.recordAction("press:" + strings.Join(keys, "+"))
	return pendingWork{
		gameCommands:   []IPCCommand{command},
		gameActionName: string(a.spec.Name),
	}, neuro.NewSuccessResult("accepted")
}

// gameInputCommand turns a keybind (which may be a mouse button) into the right
// executor command.
func gameInputCommand(key string, seconds float64, held bool) (IPCCommand, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return IPCCommand{}, fmt.Errorf("empty keybind")
	}

	switch strings.ToLower(trimmed) {
	case "left", "right", "middle", "mouseleft", "mouseright", "mousemiddle":
		button := normalizeMouseButton(trimmed)
		if held && seconds > 0 {
			return IPCCommand{
				Type:       CmdMouseHoldFor,
				Params:     map[string]interface{}{"button": button, "seconds": seconds},
				ExecuteNow: true,
				ClearAfter: false,
			}, nil
		}
		return IPCCommand{
			Type:       CmdMouseClick,
			Params:     map[string]interface{}{"button": button},
			ExecuteNow: true,
			ClearAfter: false,
		}, nil
	}

	if strings.ContainsAny(trimmed, "+") {
		parts := splitKeys(trimmed)
		if len(parts) > 1 {
			return IPCCommand{
				Type:       CmdKeyCombo,
				Params:     map[string]interface{}{"keys": parts},
				ExecuteNow: true,
				ClearAfter: false,
			}, nil
		}
	}

	if held && seconds > 0 {
		return IPCCommand{
			Type:       CmdKeyHoldFor,
			Params:     map[string]interface{}{"key": trimmed, "seconds": seconds},
			ExecuteNow: true,
			ClearAfter: false,
		}, nil
	}

	return IPCCommand{
		Type:       CmdKeyPress,
		Params:     map[string]interface{}{"key": trimmed},
		ExecuteNow: true,
		ClearAfter: false,
	}, nil
}

func normalizeMouseButton(button string) string {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "left", "mouseleft":
		return "left"
	case "right", "mouseright":
		return "right"
	case "middle", "mousemiddle":
		return "middle"
	}
	return "left"
}

func splitKeys(combo string) []string {
	raw := strings.FieldsFunc(combo, func(r rune) bool {
		return r == '+' || r == ' ' || r == ','
	})
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (n *NDIntegration) buildGameLaunch(a *IPCProxyAction, params map[string]interface{}) (interface{}, neuro.ExecutionResult) {
	profileID := strings.TrimSpace(stringParam(params, "profile_id"))
	profile, ok := n.games.ProfileByID(profileID)
	if !ok {
		return nil, neuro.NewFailureResult(fmt.Sprintf("Unknown game profile %q", profileID))
	}
	if err := n.launchGame(profile); err != nil {
		return nil, neuro.NewFailureResult(err.Error())
	}
	return nil, neuro.NewSuccessResult(fmt.Sprintf("Launching %s.", profile.Name))
}

func (n *NDIntegration) launchGame(profile *GameProfile) error {
	if len(profile.Launch.Commands) == 0 {
		return fmt.Errorf("%s has no launch command configured in its profile", profile.Name)
	}

	key := launchPlatformKey()
	command, ok := profile.Launch.Commands[key]
	if !ok || strings.TrimSpace(command) == "" {
		return fmt.Errorf("%s has no launch command for %s (configured: %s)",
			profile.Name, key, strings.Join(sortedKeys(profile.Launch.Commands), ", "))
	}

	resp, err := n.sendToExecutorForAction(IPCCommand{
		Type:       CmdRunScript,
		Params:     map[string]interface{}{"script": fmt.Sprintf("LAUNCH %s", command)},
		ExecuteNow: true,
		ClearAfter: true,
	}, string(CmdGameLaunch))
	if err != nil {
		return fmt.Errorf("launch failed: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("launch failed: %s", nonEmptyOr(resp.Error, "executor refused the command"))
	}

	log.Printf("Launched %s via %s", profile.ID, command)
	return nil
}

func launchPlatformKey() string {
	return platformKey()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// gameObserveRequest is the slow part of game_observe: screenshot + vision.
type gameObserveRequest struct {
	vision            bool
	prompt            string
	recordActionAs    string
	recordActionNamed string
}

// runGameObserve captures the screen, optionally asks the vision server, and
// delivers the result to Neuro through context (the action already returned).
func (n *NDIntegration) runGameObserve(request *gameObserveRequest) {
	if request == nil {
		return
	}

	permit, err := n.observationPermit(CmdGameObserve)
	if err != nil {
		n.reportGameFailure(request.recordActionAs, err.Error())
		return
	}
	message, err := n.buildGameObservationGuarded(request.vision, request.prompt, permit)
	if err != nil {
		n.reportGameFailure(request.recordActionAs, err.Error())
		return
	}

	if err := permit.check(); err != nil {
		n.reportGameFailure(request.recordActionAs, err.Error())
		return
	}
	if err := n.client.SendContext(message, true); err != nil {
		log.Printf("Failed to send game observation context: %v", err)
	}
	n.games.recordAction("observe")
}

// buildGameObservation produces the text Neuro receives from game_observe. It is
// also what the dashboard shows for "what would Neuro see right now".
func (n *NDIntegration) buildGameObservation(vision bool, prompt string) (string, error) {
	permit, err := n.observationPermit(CmdGameObserve)
	if err != nil {
		return "", err
	}
	return n.buildGameObservationGuarded(vision, prompt, permit)
}

func (n *NDIntegration) buildGameObservationGuarded(vision bool, prompt string, permit *observationPermit) (string, error) {
	if vision {
		if err := permit.requireVisionService(); err != nil {
			return "", err
		}
	}
	if err := permit.check(); err != nil {
		return "", err
	}
	status, err := n.fetchDesktopStatusGuarded(vision && visionServerURL() != "", permit.check)
	if err != nil {
		return "", err
	}
	if err := permit.check(); err != nil {
		return "", err
	}

	shot, hasShot, shotErr := screenshotPNG(status)
	if shotErr != nil {
		log.Printf("Screenshot skipped: %v", shotErr)
	}
	summary := ""

	visionURL := visionServerURL()
	if vision && visionURL != "" && hasShot {
		if prompt == "" {
			prompt = "Describe what is happening in this game screenshot for an AI that is playing it. Mention the player's situation, the HUD, and anything urgent in one or two sentences."
		}
		if err := permit.check(); err != nil {
			return "", err
		}
		captured, visionErr := summarizeWithVisionServerContext(permit.vision.ctx, visionURL, shot, prompt)
		if visionErr != nil {
			log.Printf("Vision summarise failed: %v", visionErr)
		} else {
			summary = captured
		}
	}

	session := n.games.Session()
	lines := []string{"## Game observation"}
	if session != nil {
		lines = append(lines, fmt.Sprintf("- Playing: %s (%s)", session.ProfileName, session.ProfileID))
	}
	if activeWindow, ok := status["active_window"].(string); ok && strings.TrimSpace(activeWindow) != "" {
		lines = append(lines, fmt.Sprintf("- Active window: %s", activeWindow))
	}
	if hasShot {
		lines = append(lines, fmt.Sprintf("- Screenshot: captured (PNG, %d KB)", len(shot)/1024))
	} else if shotErr != nil {
		lines = append(lines, fmt.Sprintf("- Screenshot: unusable (%v)", shotErr))
	} else if reason, _ := status["screenshot_error"].(string); reason != "" {
		lines = append(lines, "- Screenshot: unavailable ("+reason+")")
	}
	if summary != "" {
		lines = append(lines, fmt.Sprintf("- Vision summary: %s", summary))
	} else if vision {
		// Say which case it is: a missing URL and a server that failed need
		// different fixes, and the hint must not send the operator the wrong way.
		if visionServerURL() == "" {
			lines = append(lines, "- Vision summary: unavailable (set NEURO_VISION_URL to enable)")
		} else {
			lines = append(lines, "- Vision summary: unavailable (the vision server did not answer; its state is on the dashboard)")
		}
	}

	if err := permit.check(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func (n *NDIntegration) reportGameFailure(action, message string) {
	_ = n.client.SendContext(fmt.Sprintf(
		"## Game action failed\n\n- action: `%s`\n- error: %s", action, message), true)
}
