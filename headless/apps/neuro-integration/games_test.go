package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeProfiles(t *testing.T, name string, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write profile fixture: %v", err)
	}
	return dir
}

const minecraftProfile = `{
  "id": "minecraft",
  "name": "Minecraft",
  "match": {"processes": ["javaw"], "window_titles": ["Minecraft"]},
  "control": {"mode": "nd", "mouse_look": {"enabled": true}},
  "keys": {"forward": "w", "jump": "space", "attack": "left"}
}`

const fallbackProfile = `{
  "id": "generic",
  "name": "Generic",
  "match": {"default": true},
  "control": {"mode": "nd"},
  "keys": {"forward": "w"}
}`

func TestLoadGameRegistryFromDirectory(t *testing.T) {
	fixture := writeProfiles(t, "profiles.json", `{"profiles": [`+minecraftProfile+`,`+fallbackProfile+`]}`)
	t.Setenv("NEURO_GAME_PROFILES_DIR", fixture)
	t.Setenv("NEURO_GAME_PROFILES_FILE", "")

	registry := loadGameRegistry()
	profiles := registry.Profiles()
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].ID != "generic" || profiles[1].ID != "minecraft" {
		t.Fatalf("profiles should be sorted by id, got %s, %s", profiles[0].ID, profiles[1].ID)
	}

	// Defaults are filled in so downstream code never has to guard for them.
	minecraft, ok := registry.ProfileByID("minecraft")
	if !ok {
		t.Fatal("minecraft profile missing")
	}
	if minecraft.Control.MouseLook.Sensitivity != 1.0 {
		t.Fatalf("expected sensitivity default 1.0, got %v", minecraft.Control.MouseLook.Sensitivity)
	}
	if minecraft.Control.MaxActionsPerMin != 120 {
		t.Fatalf("expected rate limit default 120, got %d", minecraft.Control.MaxActionsPerMin)
	}
}

func TestGameRegistryIgnoresDuplicateAndInvalidEntries(t *testing.T) {
	duplicate := strings.Replace(minecraftProfile, `"mode": "nd"`, `"mode": "external"`, 1)
	fixture := writeProfiles(t, "profiles.json", `[`+minecraftProfile+`,`+duplicate+`]`)
	t.Setenv("NEURO_GAME_PROFILES_DIR", fixture)
	t.Setenv("NEURO_GAME_PROFILES_FILE", "")

	registry := loadGameRegistry()
	if len(registry.Profiles()) != 1 {
		t.Fatalf("expected duplicates to collapse to one profile, got %d", len(registry.Profiles()))
	}
	profile, _ := registry.ProfileByID("minecraft")
	if profile.Control.Mode != ControlModeND {
		t.Fatalf("the first definition should win, got mode %s", profile.Control.Mode)
	}
}

func TestGameRegistryRejectsUnknownControlMode(t *testing.T) {
	fixture := writeProfiles(t, "profiles.json", strings.Replace(
		minecraftProfile, `"mode": "nd"`, `"mode": "chaos"`, 1))
	t.Setenv("NEURO_GAME_PROFILES_DIR", fixture)
	t.Setenv("NEURO_GAME_PROFILES_FILE", "")

	registry := loadGameRegistry()
	profile, ok := registry.ProfileByID("minecraft")
	if !ok {
		t.Fatal("profile missing")
	}
	if profile.Control.Mode != ControlModeND {
		t.Fatalf("unknown mode should fall back to nd, got %s", profile.Control.Mode)
	}
}

func TestDetectPrefersProcessOverWindowTitle(t *testing.T) {
	registry := &GameRegistry{}
	registry.profiles = []GameProfile{
		{ID: "title-match", Match: GameMatch{WindowTitles: []string{"Minecraft"}}},
		{ID: "process-match", Match: GameMatch{Processes: []string{"javaw.exe"}}},
	}
	for i := range registry.profiles {
		registry.profiles[i].applyGameProfileDefaults()
	}

	detected := registry.Detect("Minecraft 1.21 - Singleplayer", []string{"javaw.exe", "explorer.exe"})
	if detected.Profile == nil {
		t.Fatal("expected a match")
	}
	if detected.Profile.ID != "process-match" {
		t.Fatalf("process matches should outrank window titles, got %s", detected.Profile.ID)
	}
	if !strings.Contains(detected.MatchedOn, "javaw.exe") {
		t.Fatalf("expected the matching process to be reported, got %q", detected.MatchedOn)
	}
}

func TestDetectFallsBackToDefaultProfile(t *testing.T) {
	registry := &GameRegistry{}
	registry.profiles = []GameProfile{
		{ID: "generic", Match: GameMatch{Default: true}},
		{ID: "minecraft", Match: GameMatch{Processes: []string{"javaw"}}},
	}
	for i := range registry.profiles {
		registry.profiles[i].applyGameProfileDefaults()
	}

	detected := registry.Detect("Some Unknown Game", []string{"unknown.exe"})
	if detected.Profile == nil || detected.Profile.ID != "generic" {
		t.Fatalf("expected the default profile, got %+v", detected.Profile)
	}
}

func TestDetectWithoutMatch(t *testing.T) {
	registry := &GameRegistry{}
	registry.profiles = []GameProfile{
		{ID: "minecraft", Match: GameMatch{Processes: []string{"javaw"}}},
	}

	detected := registry.Detect("Firefox", []string{"firefox"})
	if detected.Profile != nil {
		t.Fatalf("expected no profile, got %s", detected.Profile.ID)
	}
	if detected.ActiveTitle != "Firefox" {
		t.Fatalf("expected the active window to be reported, got %q", detected.ActiveTitle)
	}
}

func TestInputAllowedArbitration(t *testing.T) {
	ndProfile := &GameProfile{ID: "game", Control: GameControl{Mode: ControlModeND}}
	if err := inputAllowed(ndProfile, nil, "game_move", nil); err != nil {
		t.Fatalf("nd mode should allow input: %v", err)
	}

	external := &GameProfile{
		ID:      "stardew",
		Control: GameControl{Mode: ControlModeExternal, ExternalName: "stardew-integration"},
	}
	err := inputAllowed(external, nil, "game_move", nil)
	if err == nil {
		t.Fatal("external mode must refuse input")
	}
	if !strings.Contains(err.Error(), "stardew-integration") {
		t.Fatalf("the refusal should name the owning integration, got: %v", err)
	}

	hybrid := &GameProfile{
		ID:      "stardew",
		Control: GameControl{Mode: ControlModeHybrid, NDActions: []string{"game_observe"}, ExternalName: "stardew-integration"},
	}
	if err := inputAllowed(hybrid, nil, "game_observe", nil); err != nil {
		t.Fatalf("hybrid mode should allow listed actions: %v", err)
	}
	if err := inputAllowed(hybrid, nil, "game_move", nil); err == nil {
		t.Fatal("hybrid mode must refuse actions outside the allow-list")
	}

	if err := inputAllowed(nil, nil, "game_move", nil); err == nil {
		t.Fatal("input without a profile must be refused")
	}
}

func TestSessionStartUsesResolvedMode(t *testing.T) {
	runtime := newGameRuntime(&GameRegistry{})
	profile := GameProfile{ID: "minecraft", Control: GameControl{Mode: ControlModeND}}
	profile.applyGameProfileDefaults()

	session := runtime.start(profile, "")
	if session.ControlMode != ControlModeND {
		t.Fatalf("expected the profile mode to be used, got %s", session.ControlMode)
	}
	if runtime.Session() == nil {
		t.Fatal("expected an active session")
	}

	ended := runtime.end()
	if ended == nil || ended.ProfileID != "minecraft" {
		t.Fatalf("expected the ended session to be reported, got %+v", ended)
	}
	if runtime.Session() != nil {
		t.Fatal("session should be cleared after end")
	}
}

func TestSessionRateLimit(t *testing.T) {
	runtime := newGameRuntime(&GameRegistry{})
	profile := GameProfile{ID: "game", Control: GameControl{Mode: ControlModeND, MaxActionsPerMin: 3}}
	profile.applyGameProfileDefaults()
	runtime.start(profile, ControlModeND)

	for i := 0; i < 3; i++ {
		if runtime.rateLimited(3) {
			t.Fatalf("unexpected rate limit after %d actions", i)
		}
		runtime.recordAction("game_move")
	}
	if !runtime.rateLimited(3) {
		t.Fatal("expected the budget to be exhausted after 3 actions")
	}
}

func TestDetectionCacheExpiry(t *testing.T) {
	runtime := newGameRuntime(&GameRegistry{})
	n := &NDIntegration{games: runtime, permissions: defaultPermissionPolicy()}
	permit, err := n.observationPermit(CmdGameDetect)
	if err != nil {
		t.Fatal(err)
	}
	runtime.setDetection(GameDetected{Profile: &GameProfile{ID: "minecraft"}, MatchedOn: "process"}, permit)

	if _, ok := runtime.cachedDetection(permit); !ok {
		t.Fatal("a fresh detection should be cached")
	}

	runtime.mu.Lock()
	runtime.lastDetectionAt = time.Now().Add(-2 * gameDetectionTTL)
	runtime.mu.Unlock()

	if _, ok := runtime.cachedDetection(permit); ok {
		t.Fatal("an expired detection must not be reused")
	}
}

func TestControlSummaryExplainsOwnership(t *testing.T) {
	profile := GameProfile{
		ID:      "stardew",
		Name:    "Stardew Valley",
		Control: GameControl{Mode: ControlModeExternal, ExternalName: "stardew-integration"},
		Keys:    map[string]string{"forward": "w"},
	}
	profile.applyGameProfileDefaults()

	summary := profile.GameControlSummary()
	if !strings.Contains(summary, "stardew-integration") {
		t.Fatalf("summary should name the owning integration: %s", summary)
	}
	if !strings.Contains(summary, "forward=w") {
		t.Fatalf("summary should list the key map: %s", summary)
	}
}

func TestSplitKeys(t *testing.T) {
	cases := map[string][]string{
		"ctrl+s":       {"ctrl", "s"},
		"ctrl shift p": {"ctrl", "shift", "p"},
		"e":            {"e"},
	}
	for input, want := range cases {
		got := splitKeys(input)
		if len(got) != len(want) {
			t.Fatalf("splitKeys(%q) = %v, want %v", input, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitKeys(%q) = %v, want %v", input, got, want)
			}
		}
	}
}
