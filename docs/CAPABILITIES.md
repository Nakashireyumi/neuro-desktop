# Neuro Desktop — Current Capabilities

Honest snapshot of what works today (post bridge/executor split). This is not a
roadmap; see [VISION.md](../VISION.md) and [PRODUCTION_TODO.md](PRODUCTION_TODO.md)
for direction and backlog.

Last updated: 2026-09-10

## Architecture (what you actually run)

| Piece | Binary / path | Role |
|-------|---------------|------|
| **Bridge (server)** | `neuro-integration` | Neuro WebSocket client, permissions, TCP executor hub `:9876`, admin HTTP `:8300` |
| **Executor (client)** | `neuro-desktop` + Python | Mouse/keyboard/scripts on the controlled machine |
| **Local Neuro mock** | `desktop/tools/ollama-neuro` | Randy-like tester using Ollama + `heredos/rwkv7:2.9b` |
| **Operator UI** | `desktop/frontend` | Permissions editor + stub extensions UI (browser / localStorage) |
| **Process supervisor** | `process-handler` | Optional; incomplete messaging, not required for day-to-day use |

**Split machines:** bridge on the Neuro/operator PC; executor on the desktop Neuro should control (`--executor --server host:9876`).

**Co-located:** `./neuro-desktop` can still spawn the Go bridge beside itself (file IPC fallback if no TCP client).

## Platforms

| Capability | Windows | Linux | macOS |
|------------|---------|-------|-------|
| Low-level mouse / keyboard (`pyautogui`) | Yes | Yes* | Yes* |
| High-level desktop intents (start menu, snap, lock, …) | Yes (Win shortcuts) | Best-effort (DE-dependent) | Best-effort (⌘ mappings) |
| Window enumeration | Yes | Weak / optional | Weak / optional |
| Screenshots (`mss`) | Yes | Yes* | Yes* |
| Headless CI / no display | Unit tests only (`NEURO_HEADLESS=1`) | Same | Same |

\* Needs a real graphical session. Do **not** run with `sudo` (breaks Wayland/X auth and file ownership). On Omarchy/Hyprland, run from your logged-in desktop session.

## Neuro API integration

Working:

- Connect / reconnect to Neuro (or Ollama mock / Randy) via WebSocket
- `startup`, `actions/register`, `action` → `action/result`, `context`
- Validate quickly, send `action/result`, then run desktop work (API ~20s result window)
- Permission policy: scopes + allow/deny lists (`permissions.json`)
- Periodic desktop context snapshots (Markdown)

Not production-hardened:

- Live Neuro vs Evil character UX beyond startup ack fields
- Voice chat side-channel
- Signed / distributed permission policies

## Actions Neuro can use

### Low-level (default registered on startup)

- `move_mouse_to`, `mouse_click`, `key_press`, `type_text`
- `run_script` (action-script language)
- `execute_queue`, `clear_action_queue`
- `enable_low_level_controls` / `disable_low_level_controls` (mode switch)

### High-level desktop intents (when HL mode / script intents)

OS-mapped shortcuts, including:

- Start / launcher, show desktop, minimize, close foreground app
- Task manager / Force Quit, file manager, run/search dialogs
- Snap left/right, settings, notifications, clipboard history (where supported)
- Lock workstation, app switcher, screen snip

Unsupported intents on an OS fail with a clear error (e.g. macOS clipboard history).

### Catalog / extensions / vision (partial)

- Catalog list/search/get — metadata from local catalog config
- Extension install/enable — **partial** (git-clone path exists; not a real plugin SDK)
- Desktop context + optional HTTP vision summarize (`NEURO_VISION_SERVER_URL`)
- Frontend “extensions” tab is mostly **UI/localStorage**, not a live installer

## Operator / Vedal controls

Working:

- `permissions.json` enforced in the Go bridge before actions run
- Scopes: `input`, `filesystem`, `process`, `network`, `system`, `vision`
- Safe defaults: system + filesystem restricted; dangerous actions denied in the example policy
- Admin HTTP: `GET/PUT /api/permissions`, `GET /api/status` on `:8300`
- Frontend Permissions page can **export** a Go-compatible policy file

Not done:

- Native host bridge (`ndHost`) — UI still falls back to browser storage
- Live sync of UI edits into the running bridge without exporting the file
- Rich path/process/host allowlists beyond the JSON schema placeholders

## Local testing without live Neuro

`desktop/tools/ollama-neuro`:

- Mimics Neuro API on `ws://127.0.0.1:8000`
- Modes: `manual` (Randy-like), `random`, `ollama` (RWKV7)
- Control UI on `:1337`
- Tuned for slow CPUs (long timeouts, `--warm`, `keep_alive`)

## Explicitly not ready

- Full plugin marketplace / signed catalogs
- Production native tray shell
- Process-handler as the primary supervisor (stubs / TODOs remain)
- Guaranteed Linux DE shortcut parity
- Running under `sudo` or pure SSH without display forwarding

## Quick verify

```bash
cd desktop

# Fix leftover root-owned build dirs if a past sudo broke things:
sudo chown -R "$USER:$USER" frontend/dist dist apps/neuro-desktop/target backend/python/.venv

# Bundle as your user (never sudo):
./scripts/bundle/dev.sh

# Or split:
# terminal A — bridge
./apps/neuro-desktop/target/release/neuro-integration --ws-url ws://127.0.0.1:8000
# terminal B — executor (graphical session)
./apps/neuro-desktop/target/release/neuro-desktop --executor --server 127.0.0.1:9876
```

For a fake Neuro backend: see [desktop/tools/ollama-neuro/README.md](../desktop/tools/ollama-neuro/README.md).
