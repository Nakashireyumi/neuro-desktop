package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The game layer is what lets Neuro play a game that has no dedicated
// integration. A profile describes how to recognise a game on the controlled
// machine, which keys its controls map to, and whether Neuro Desktop is
// allowed to drive the input at all.
//
// Control modes exist because Neuro Desktop must be able to run *alongside*
// existing game integrations:
//
//	nd       — Neuro Desktop owns input; the generic interface below is used.
//	external — an existing integration owns input; ND only observes.
//	hybrid   — ND owns input for the actions listed in control.nd_actions.
type GameControlMode string

const (
	ControlModeND       GameControlMode = "nd"
	ControlModeExternal GameControlMode = "external"
	ControlModeHybrid   GameControlMode = "hybrid"
	// ControlModeAuto resolves at session start: external when the game's own
	// integration is connected through the relay, otherwise Neuro Desktop drives.
	ControlModeAuto GameControlMode = "auto"
)

type GameMatch struct {
	// Processes matched against running process names (case-insensitive).
	Processes []string `json:"processes,omitempty"`
	// WindowTitles matched against the active window title (case-insensitive).
	WindowTitles []string `json:"window_titles,omitempty"`
	// Default marks the fallback profile used when nothing else matches.
	Default bool `json:"default,omitempty"`
}

type GameMouseLook struct {
	Enabled     bool    `json:"enabled"`
	Sensitivity float64 `json:"sensitivity,omitempty"`
	InvertY     bool    `json:"invert_y,omitempty"`
	// MaxStep clamps a single look command, so a hallucinated 5000px flick
	// cannot spin the camera into the floor.
	MaxStep int `json:"max_step,omitempty"`
}

type GameControl struct {
	Mode             GameControlMode `json:"mode,omitempty"`
	ExternalName     string          `json:"external_integration,omitempty"`
	Movement         string          `json:"movement,omitempty"` // "keys", "mouse", "both"
	MouseLook        GameMouseLook   `json:"mouse_look,omitempty"`
	MoveHoldSeconds  float64         `json:"move_hold_seconds,omitempty"`
	NDActions        []string        `json:"nd_actions,omitempty"` // hybrid mode: which actions ND may inject
	AllowRawKeys     *bool           `json:"allow_raw_keys,omitempty"`
	MaxActionsPerMin int             `json:"max_actions_per_minute,omitempty"`
}

type GameVision struct {
	Recommended bool   `json:"recommended,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type GameLaunch struct {
	// Commands are OS-native launch strings (steam://..., executable path).
	// Launching requires the system scope and is denied by default.
	Commands map[string]string `json:"commands,omitempty"`
}

// GameProfile is one game Neuro Desktop knows how to interface with.
type GameProfile struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Match       GameMatch         `json:"match"`
	Control     GameControl       `json:"control"`
	Keys        map[string]string `json:"keys,omitempty"`
	Notes       string            `json:"notes,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Vision      GameVision        `json:"vision,omitempty"`
	Launch      GameLaunch        `json:"launch,omitempty"`
}

// GameDetected is the outcome of matching the desktop against the registry.
type GameDetected struct {
	Profile     *GameProfile `json:"profile,omitempty"`
	MatchedOn   string       `json:"matched_on,omitempty"`
	ActiveTitle string       `json:"active_window,omitempty"`
	Processes   []string     `json:"process_candidates,omitempty"`
}

// GameSession is live state for "Neuro is playing this game right now".
type GameSession struct {
	// generation identifies a session independently of wall-clock resolution.
	generation    uint64
	ProfileID     string          `json:"profile_id"`
	ProfileName   string          `json:"profile_name"`
	ControlMode   GameControlMode `json:"control_mode"`
	ExternalName  string          `json:"external_integration,omitempty"`
	StartedAt     time.Time       `json:"started_at"`
	ActionsIssued int             `json:"actions_issued"`
	LastAction    string          `json:"last_action,omitempty"`
	LastActionAt  time.Time       `json:"last_action_at,omitempty"`
}

// GameRegistry holds the loaded profiles.
type GameRegistry struct {
	profiles []GameProfile
	source   string
}

func defaultGameProfilePaths() (string, string) {
	dir := strings.TrimSpace(os.Getenv("NEURO_GAME_PROFILES_DIR"))
	if dir == "" {
		dir = filepath.Join("catalog", "games")
	}
	file := strings.TrimSpace(os.Getenv("NEURO_GAME_PROFILES_FILE"))
	return dir, file
}

// loadGameRegistry reads profiles from an explicit file, a directory of
// *.json files, or the bundled catalog. Missing sources are not fatal: a
// desktop with no game profiles still works as a desktop integration.
func loadGameRegistry() *GameRegistry {
	dir, file := defaultGameProfilePaths()

	registry := &GameRegistry{}

	if file != "" {
		profiles, err := loadGameProfilesFile(file)
		if err != nil {
			log.Printf("Game profiles file %s: %v", file, err)
		} else {
			registry.profiles = append(registry.profiles, profiles...)
			registry.source = file
		}
	}

	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err == nil {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
					continue
				}
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			for _, name := range names {
				profiles, err := loadGameProfilesFile(filepath.Join(dir, name))
				if err != nil {
					log.Printf("Game profile %s skipped: %v", name, err)
					continue
				}
				registry.profiles = append(registry.profiles, profiles...)
			}
			if registry.source == "" {
				registry.source = dir
			}
		}
	}

	registry.profiles = dedupeGameProfiles(registry.profiles)
	for i := range registry.profiles {
		registry.profiles[i].applyGameProfileDefaults()
	}

	if len(registry.profiles) > 0 {
		log.Printf("Loaded %d game profile(s) from %s", len(registry.profiles), registry.source)
	}
	return registry
}

func dedupeGameProfiles(profiles []GameProfile) []GameProfile {
	seen := map[string]bool{}
	out := make([]GameProfile, 0, len(profiles))
	for _, profile := range profiles {
		id := strings.ToLower(strings.TrimSpace(profile.ID))
		if id == "" {
			log.Printf("Game profile without an id ignored")
			continue
		}
		if seen[id] {
			log.Printf("Game profile %s defined more than once; keeping the first", id)
			continue
		}
		seen[id] = true
		profile.ID = id
		out = append(out, profile)
	}
	return out
}

// loadGameProfilesFile accepts either a single profile object, an array of
// profiles, or {"profiles": [...]}.
func loadGameProfilesFile(path string) ([]GameProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("empty profile file")
	}

	switch trimmed[0] {
	case '[':
		var profiles []GameProfile
		if err := json.Unmarshal(data, &profiles); err != nil {
			return nil, fmt.Errorf("invalid profile array: %w", err)
		}
		return profiles, nil
	case '{':
		var wrapper struct {
			Profiles []GameProfile `json:"profiles"`
		}
		if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Profiles) > 0 {
			return wrapper.Profiles, nil
		}
		var single GameProfile
		if err := json.Unmarshal(data, &single); err != nil {
			return nil, fmt.Errorf("invalid profile object: %w", err)
		}
		return []GameProfile{single}, nil
	default:
		return nil, fmt.Errorf("unrecognised profile format")
	}
}

func (p *GameProfile) applyGameProfileDefaults() {
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.Control.Mode == "" {
		p.Control.Mode = ControlModeND
	}
	switch p.Control.Mode {
	case ControlModeND, ControlModeExternal, ControlModeHybrid, ControlModeAuto:
	default:
		log.Printf("Profile %s has unknown control mode %q; falling back to %s",
			p.ID, p.Control.Mode, ControlModeND)
		p.Control.Mode = ControlModeND
	}
	if p.Control.Movement == "" {
		p.Control.Movement = "keys"
	}
	if p.Control.MouseLook.Sensitivity <= 0 {
		p.Control.MouseLook.Sensitivity = 1.0
	}
	if p.Control.MouseLook.MaxStep <= 0 {
		p.Control.MouseLook.MaxStep = 600
	}
	if p.Control.MoveHoldSeconds <= 0 {
		p.Control.MoveHoldSeconds = 1.0
	}
	if p.Control.MaxActionsPerMin <= 0 {
		p.Control.MaxActionsPerMin = 120
	}
	if p.Control.Mode == "" {
		p.Control.Mode = ControlModeND
	}
	if p.Keys == nil {
		p.Keys = map[string]string{}
	}
	if p.Control.AllowRawKeys == nil {
		allow := true
		p.Control.AllowRawKeys = &allow
	}
}

// ProfileByID returns a copy of a profile by id.
func (r *GameRegistry) ProfileByID(id string) (*GameProfile, bool) {
	target := strings.ToLower(strings.TrimSpace(id))
	for i := range r.profiles {
		if r.profiles[i].ID == target {
			profile := r.profiles[i]
			return &profile, true
		}
	}
	return nil, false
}

// Profiles returns a stable, sorted copy of the registry.
func (r *GameRegistry) Profiles() []GameProfile {
	out := make([]GameProfile, len(r.profiles))
	copy(out, r.profiles)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *GameRegistry) Source() string {
	if r == nil {
		return ""
	}
	return r.source
}

func matchProcess(names []string, pattern string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	for _, name := range names {
		candidate := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
		if candidate == pattern {
			return true
		}
		// Allow "minecraft" to match "minecraft.exe" / "minecraft-launcher".
		if strings.TrimSuffix(candidate, filepath.Ext(candidate)) == strings.TrimSuffix(pattern, filepath.Ext(pattern)) {
			return true
		}
		if strings.Contains(candidate, pattern) {
			return true
		}
	}
	return false
}

func matchTitle(title string, patterns []string) (string, bool) {
	lowered := strings.ToLower(title)
	if strings.TrimSpace(lowered) == "" {
		return "", false
	}
	for _, pattern := range patterns {
		needle := strings.ToLower(strings.TrimSpace(pattern))
		if needle == "" {
			continue
		}
		if strings.Contains(lowered, needle) {
			return pattern, true
		}
	}
	return "", false
}

// Detect matches a desktop snapshot (active window + processes) to a profile.
// Scoring prefers a process-name hit over a window-title hit because titles are
// noisy (browser tabs, terminals, launcher windows).
func (r *GameRegistry) Detect(activeWindow string, processes []string) GameDetected {
	result := GameDetected{ActiveTitle: activeWindow}

	if r == nil {
		return result
	}

	bestScore := 0
	var best *GameProfile
	bestReason := ""
	var fallback *GameProfile

	for i := range r.profiles {
		profile := &r.profiles[i]

		if profile.Match.Default {
			if fallback == nil {
				fallback = profile
			}
			continue
		}

		score := 0
		reason := ""

		for _, pattern := range profile.Match.Processes {
			if matchProcess(processes, pattern) {
				if score < 3 {
					score = 3
					reason = fmt.Sprintf("process %q", pattern)
				}
				break
			}
		}

		if matched, ok := matchTitle(activeWindow, profile.Match.WindowTitles); ok && score < 2 {
			score = 2
			reason = fmt.Sprintf("window title %q", matched)
		}

		if score > bestScore {
			bestScore = score
			best = profile
			bestReason = reason
		}
	}

	if best != nil {
		result.Profile = best
		result.MatchedOn = bestReason
		return result
	}

	if fallback != nil {
		result.Profile = fallback
		result.MatchedOn = "fallback profile"
	}
	return result
}

// GameRuntime is the live session state owned by the integration. It holds the
// loaded profile registry as well, so one object answers "what can Neuro play"
// and "what is Neuro playing right now".
type GameRuntime struct {
	mu                sync.Mutex
	registry          *GameRegistry
	session           *GameSession
	sessionGeneration uint64
	// lastDetection caches the desktop snapshot so `game_detect` cannot be used
	// to hammer the executor with get_status calls.
	lastDetection       GameDetected
	lastDetectionAt     time.Time
	lastDetectionPermit *observationPermit
	actionTimestamps    []time.Time
}

const gameDetectionTTL = 5 * time.Second

func newGameRuntime(registry *GameRegistry) *GameRuntime {
	if registry == nil {
		registry = &GameRegistry{}
	}
	return &GameRuntime{registry: registry}
}

// Profiles returns every loaded profile.
func (g *GameRuntime) Profiles() []GameProfile {
	if g == nil || g.registry == nil {
		return nil
	}
	return g.registry.Profiles()
}

// ProfileByID looks a profile up for the active session.
func (g *GameRuntime) ProfileByID(id string) (*GameProfile, bool) {
	if g == nil || g.registry == nil {
		return nil, false
	}
	return g.registry.ProfileByID(id)
}

// Detect matches a desktop snapshot against the registry.
func (g *GameRuntime) Detect(activeWindow string, processes []string) GameDetected {
	if g == nil || g.registry == nil {
		return GameDetected{ActiveTitle: activeWindow}
	}
	return g.registry.Detect(activeWindow, processes)
}

// RegistrySource reports where the profiles came from (for the dashboard).
func (g *GameRuntime) RegistrySource() string {
	if g == nil || g.registry == nil {
		return ""
	}
	return g.registry.Source()
}

func (g *GameRuntime) Session() *GameSession {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session == nil {
		return nil
	}
	copied := *g.session
	return &copied
}

func (g *GameRuntime) start(profile GameProfile, mode GameControlMode) GameSession {
	if g == nil {
		return GameSession{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if mode == "" {
		mode = profile.Control.Mode
	}

	g.sessionGeneration++
	session := &GameSession{
		generation:   g.sessionGeneration,
		ProfileID:    profile.ID,
		ProfileName:  profile.Name,
		ControlMode:  mode,
		ExternalName: profile.Control.ExternalName,
		StartedAt:    time.Now().UTC(),
	}
	g.session = session
	g.actionTimestamps = nil

	copied := *session
	return copied
}

func (g *GameRuntime) end() *GameSession {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session == nil {
		return nil
	}
	previous := g.session
	g.session = nil
	copied := *previous
	return &copied
}

func (g *GameRuntime) recordAction(name string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session == nil {
		return
	}
	g.session.ActionsIssued++
	g.session.LastAction = name
	g.session.LastActionAt = time.Now().UTC()
	g.actionTimestamps = append(g.actionTimestamps, time.Now())
	if len(g.actionTimestamps) > 256 {
		g.actionTimestamps = g.actionTimestamps[len(g.actionTimestamps)-256:]
	}
}

// rateLimited reports whether the session has exceeded its action budget. The
// budget stops a runaway loop (Neuro retrying an action it cannot perceive)
// from pinning the controlled machine's input at 100%.
func (g *GameRuntime) rateLimited(limitPerMinute int) bool {
	if g == nil || limitPerMinute <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Minute)
	kept := g.actionTimestamps[:0]
	for _, ts := range g.actionTimestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	g.actionTimestamps = kept
	return len(kept) >= limitPerMinute
}

func (g *GameRuntime) setDetection(detected GameDetected, permit *observationPermit) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastDetection = detected
	g.lastDetectionAt = time.Now()
	g.lastDetectionPermit = permit
}

// Cached metadata is private observation data too. Its original approval must
// still be current; a replacement grant/policy starts a new observation.
func (g *GameRuntime) cachedDetection(permit *observationPermit) (GameDetected, bool) {
	if g == nil {
		return GameDetected{}, false
	}
	g.mu.Lock()
	detected, at, origin := g.lastDetection, g.lastDetectionAt, g.lastDetectionPermit
	g.mu.Unlock()
	if detected.Profile == nil || time.Since(at) > gameDetectionTTL || origin == nil || permit == nil ||
		origin.check() != nil || permit.check() != nil {
		return GameDetected{}, false
	}
	return detected, true
}

// inputAllowed decides whether Neuro Desktop may inject input for a game
// action, given the profile's control mode and (in hybrid mode) the action list.
func inputAllowed(profile *GameProfile, session *GameSession, actionKey string, relay *RelayState) error {
	if profile == nil {
		return fmt.Errorf("no game profile is active; call game_start_session first")
	}

	mode := profile.Control.Mode
	if session != nil && session.ControlMode != "" {
		mode = session.ControlMode
	}

	switch mode {
	case ControlModeND, ControlModeAuto:
		return nil
	case ControlModeExternal:
		return fmt.Errorf("this game is controlled by the %q integration, so Neuro Desktop will not inject input (%s). %s",
			ownerFor(profile), actionKey, externalDelegationHint(relay, ownerFor(profile)))
	case ControlModeHybrid:
		for _, allowed := range profile.Control.NDActions {
			if strings.EqualFold(allowed, actionKey) {
				return nil
			}
		}
		return fmt.Errorf(
			"%s is outside Neuro Desktop's hybrid allow-list for %s (the %q integration owns input). %s",
			actionKey, profile.Name, ownerFor(profile), externalDelegationHint(relay, ownerFor(profile)))
	default:
		return fmt.Errorf("unknown control mode %q for profile %s", mode, profile.ID)
	}
}

// GameControlSummary renders the profile for Neuro as Markdown context.
func (p *GameProfile) GameControlSummary() string {
	var lines []string
	lines = append(lines, fmt.Sprintf("## Game interface: %s", p.Name))

	switch p.Control.Mode {
	case ControlModeExternal:
		owner := nonEmptyOr(p.Control.ExternalName, p.ID)
		lines = append(lines, fmt.Sprintf(
			"- Control: an existing integration (%s) handles input. Use its actions; `game_observe` still works here.", owner))
	case ControlModeHybrid:
		owner := nonEmptyOr(p.Control.ExternalName, p.ID)
		lines = append(lines, fmt.Sprintf(
			"- Control: hybrid — the %s integration owns most input; Neuro Desktop may only use: %s",
			owner, strings.Join(p.Control.NDActions, ", ")))
	default:
		lines = append(lines, "- Control: Neuro Desktop drives this game through the actions below.")
	}

	move := p.Control.MoveHoldSeconds
	if move <= 0 {
		move = 1
	}
	lines = append(lines, fmt.Sprintf(
		"- Movement: `game_move {direction: forward|back|left|right, seconds: %.1f}` (default hold %.1fs)",
		move, move))

	if p.Control.MouseLook.Enabled {
		lines = append(lines, "- Camera: `game_look {dx: <pixels>, dy: <pixels>}` (relative mouse-look)")
	}

	if len(p.Keys) > 0 {
		names := make([]string, 0, len(p.Keys))
		for name := range p.Keys {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%s=%s", name, p.Keys[name]))
		}
		lines = append(lines, fmt.Sprintf("- Key actions (`game_action`, or `game_press` for raw keys): %s", strings.Join(parts, ", ")))
	}

	if p.Description != "" {
		lines = append(lines, fmt.Sprintf("- About: %s", p.Description))
	}
	if p.Notes != "" {
		lines = append(lines, fmt.Sprintf("- Play notes: %s", p.Notes))
	}
	if p.Vision.Recommended {
		lines = append(lines, "- Visual feedback strongly recommended: call `game_observe` before deciding what to do next.")
	}

	return strings.Join(lines, "\n")
}

// ownerFor names the integration that owns a game, falling back to the profile id.
func ownerFor(profile *GameProfile) string {
	if profile.Control.ExternalName != "" {
		return profile.Control.ExternalName
	}
	return profile.ID
}

// externalDelegationHint tells Neuro what to do instead when another
// integration owns the game. When the Neuro Relay link has seen that
// integration's action list, the hint names real actions to call; otherwise it
// says plainly that the operator has to describe them.
func externalDelegationHint(relay *RelayState, owner string) string {
	base := "Call that integration's own actions instead (Neuro sees them directly when the relay is running), " +
		"or use game_observe to look at the screen."
	if relay == nil {
		return base
	}
	if hint := relay.peerActionHint(owner); hint != "" {
		return hint + " " + base
	}
	if hint := relay.peerActionHint(strings.ToLower(owner)); hint != "" {
		return hint + " " + base
	}
	return base
}
