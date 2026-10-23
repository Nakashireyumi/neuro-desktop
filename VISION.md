# Vision

Neuro Desktop gives Neuro and Evil a reliable **desktop control surface** on
Windows, Linux, and macOS, interoperable with dedicated game integrations and
Neuro Relay.

## Product Direction

1. Safe Control
   - High-level intents first; low-level input behind clear safeguards.
   - Vedal (or an operator) manages capabilities via a permission policy
     (`permissions.json`: scopes + allow/deny lists) and the admin HTTP API.

2. Server / Client Split (supported)
   - **Bridge / server** (`neuro-integration`): speaks the
     [Neuro API](https://github.com/VedalAI/neuro-sdk) WebSocket protocol,
     enforces permissions, hosts the executor TCP hub (`:9876`) and operator
     admin API (`:8300`). Can run on the Neuro machine.
   - **Executor / client** (`neuro-desktop` + Python): connects to the bridge
     (`--executor --server host:9876`) and runs mouse/keyboard/scripts on the
     machine being controlled — possibly a different PC.
   - Co-located mode still works (Rust spawns the Go bridge + file IPC fallback).

3. Neuro API best practices
   - Validate quickly and send `action/result` before long desktop work.
   - Context in Markdown (`##` headings), not spammy streams.
   - Stable action set; clear failure messages.

4. Integration Hub
   - Bundle Neuro Relay and support multiple concurrent integration sessions.

5. Observable Automation
   - Every action should be traceable with structured logs and replayable context.

6. Production Reliability
   - Headless-safe CI (no real mouse/display), deterministic builds, clear docs.

## Platform Notes

- Core input (pyautogui / pynput / mss) is cross-platform.
- High-level desktop intents map to OS-specific shortcuts
  (`controller/platform_intents.py`).
- Never run the bundle/executor with `sudo` — it breaks display auth and file ownership.
- Plugin marketplace / signed catalogs remain deferred (UI stubs only).

## Non-Goals (Current)

1. Unrestricted autonomous system-level execution without permission gates.
2. Perfect parity of every OS chrome shortcut (DEs and window managers differ).
3. Marketplace monetization before trust and signing foundations are complete.
