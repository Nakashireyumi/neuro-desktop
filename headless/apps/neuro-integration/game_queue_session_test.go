package main

import (
	"strings"
	"testing"
)

func TestQueuedGameInputCannotInheritReplacementSession(t *testing.T) {
	for _, name := range []CommandType{CmdGameMove, CmdGameLook, CmdGameAction, CmdGamePress} {
		for _, replacement := range []string{"same profile", "other profile", "ended"} {
			t.Run(string(name)+"/"+replacement, func(t *testing.T) {
				profile := GameProfile{ID: "first", Keys: map[string]string{"forward": "w", "attack": "left"}, Control: GameControl{Mode: ControlModeND, MouseLook: GameMouseLook{Enabled: true}}}
				profile.applyGameProfileDefaults()
				n := &NDIntegration{games: newGameRuntime(&GameRegistry{profiles: []GameProfile{profile}}), stop: newStopSwitch(), done: make(chan struct{})}
				original := n.games.start(profile, "")
				action := &IPCProxyAction{integration: n, spec: actionSpec{Name: name}}
				params := map[string]interface{}{"direction": "forward", "dx": float64(1), "action": "attack", "key": "space"}
				var state interface{}
				switch name {
				case CmdGameMove:
					state, _ = n.buildGameMove(action, params)
				case CmdGameLook:
					state, _ = n.buildGameLook(action, params)
				case CmdGameAction:
					state, _ = n.buildGameAction(action, params)
				case CmdGamePress:
					state, _ = n.buildGamePress(action, params)
				}
				work, ok := state.(pendingWork)
				if !ok || work.gameSession == nil || work.gameSession.generation != original.generation {
					t.Fatal("input was not bound to its original session")
				}
				if replacement == "ended" {
					n.games.end()
				} else {
					if replacement == "other profile" {
						profile.ID = "second"
					}
					n.games.start(profile, "")
					// Windows wall-clock ticks can be identical across starts.
					// Reproduce that deterministically on every test platform.
					n.games.mu.Lock()
					n.games.session.StartedAt = original.StartedAt
					n.games.mu.Unlock()
				}
				// No executor is configured. A stale batch must be rejected before
				// any input, release, or IPC attempt on the replacement desktop.
				result := action.executeGameCommands(work)
				if result.Successful || !strings.Contains(result.Message, "session changed") {
					t.Fatalf("stale input was not discarded: %+v", result)
				}
			})
		}
	}
}
