# Game profiles

A **game profile** is how Neuro Desktop turns "play this game" into concrete
input on the controlled machine. Neuro gets the same neutral action vocabulary
for every game; the profile supplies the keybinds.

```
catalog/games/*.json          →  bundled profiles (shipped with Neuro Desktop)
$NEURO_GAME_PROFILES_DIR      →  your own directory of *.json profiles
$NEURO_GAME_PROFILES_FILE     →  a single file (object, array, or {"profiles": [...]})
```

Profiles from the environment are loaded in addition to the bundled ones.
Profile ids are lower-case and unique; the first definition of an id wins.

## Control modes — how Neuro Desktop coexists with game integrations

| `control.mode` | Who drives the game | What Neuro Desktop does |
| --- | --- | --- |
| `nd` | Neuro Desktop | `game_move`, `game_look`, `game_action`, `game_press` all work |
| `external` | an existing integration | Input actions are refused with a message naming the owner; `game_observe` / `game_status` still work |
| `hybrid` | both | Neuro Desktop may only inject the actions listed in `control.nd_actions` |
| `auto` | decided at session start | `external` when an integration named in `control.external_integration` is connected through the relay, otherwise `nd` |

`auto` is what makes "run alongside existing integrations" work without editing
files: install the game's own integration and connect it through Neuro Relay,
and Neuro Desktop steps aside automatically. Remove it, and Neuro Desktop takes
the controls again.

## Field reference

```jsonc
{
  "id": "minecraft",                    // required, unique, lower-case
  "name": "Minecraft",
  "description": "shown to Neuro as context",
  "tags": ["sandbox"],

  "match": {
    "processes": ["javaw", "minecraft"],  // matched against running process names
    "window_titles": ["Minecraft"],       // substring match on the active window
    "default": false                      // true = fallback profile when nothing matches
  },

  "control": {
    "mode": "nd",                         // nd | external | hybrid | auto
    "external_integration": "minecraft",  // integration that owns input (external/hybrid/auto)
    "movement": "both",                   // keys | mouse | both (informational)
    "mouse_look": {
      "enabled": true,
      "sensitivity": 1.0,                 // multiplies dx/dy
      "invert_y": false,
      "max_step": 600                     // hard clamp per game_look call
    },
    "move_hold_seconds": 0.75,            // default hold for game_move
    "max_actions_per_minute": 120,        // runaway-loop guard
    "nd_actions": ["game_observe"],       // hybrid only: what Neuro Desktop may inject
    "allow_raw_keys": true                // false = game_action only, no game_press
  },

  "keys": {                               // neutral name → key on the keyboard
    "forward": "w", "jump": "space", "attack": "left"   // "left"/"right"/"middle" = mouse buttons
  },

  "vision": { "recommended": true, "prompt": "what to ask the vision server" },

  "launch": {                             // needs the system permission scope
    "commands": { "windows": "steam://rungameid/12345", "linux": "steam://rungameid/12345" }
  },

  "notes": "extra instructions Neuro receives when the session starts"
}
```

## Adding a game

1. Copy `generic-keyboard-mouse.json` to `<game-id>.json`.
2. Set `match.processes` / `match.window_titles` to something recognisable.
3. Set the keybinds to whatever the game's own controls screen shows.
4. Call `game_detect` (or open the dashboard's Games tab) to check the match.

Deterministic input beats guessing: give Neuro a `vision.prompt` that mentions
the HUD elements it should read, and keep `move_hold_seconds` short enough that
a walk is a few calls rather than one long blind dash.
