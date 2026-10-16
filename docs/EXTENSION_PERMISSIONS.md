# Extension permissions and management delegation

You control which integrations can run and which ones Neuro and Evil can manage. Installing an item records or stages it; installation leaves it disabled and grants no runtime capabilities.

Three independent decisions apply:

1. The global `extensions` permission allows model actions that manage or call extensions.
2. Each item's `manage_allowed` decision lets Neuro and Evil install, stage, enable, disable, and remove that item. It grants no authority over another item or its permissions.
3. Each item's `run_allowed` decision and capability grants determine whether it can start. Every declared required capability needs an explicit grant and its corresponding global permission, where one exists.

For example, delegating management of a game integration lets Neuro prepare its approved source. Starting an integration that requires `input` also needs its per-item input grant and the global input permission. A later version that requests an additional required capability remains blocked until you approve that capability.

## Policy storage and API

The policy file is `extension-permissions.json` beside the extension state file. Set `NEURO_EXTENSION_PERMISSIONS_FILE` to choose another location. It is separate from install state so reinstalling or removing an item does not erase your decisions. A missing item policy denies management and runtime; a malformed policy file fails closed.

```json
{
  "version": 1,
  "extensions": {
    "example-game": {
      "manage_allowed": true,
      "run_allowed": true,
      "grants": {"input": true, "vision": false},
      "settings": {"show_activity": true}
    }
  }
}
```

`GET /api/extensions/{id}/permissions` returns the item policy, its permission declarations, settings schema, and any legacy migration status. The item can come from the signed catalog, integration registry, or marketplace.

`PUT /api/extensions/{id}/permissions` accepts the inner policy object shown above. It requires the normal operator session credential and a separately entered operator token in `X-ND-Operator-Token`. The manager must prompt for this value when changing approvals; it must never automatically copy its stored session credential into the approval header. The API does not accept policy changes from a delegated manager session or model action.

Ordinary install, enable, disable, and uninstall API calls require the same item delegation as model actions. You can perform a direct management action without delegating it by supplying the separately entered operator token. Enabling still checks runtime grants. Changing `run_allowed` or a required grant takes effect on future calls immediately, and the MCP manager withdraws and stops a no-longer-approved server.

Temporary scope approvals use one deadline timer per scope. Expiry stops affected managed MCP processes even while the server is idle; renewal replaces the previous timer, and shutdown cancels outstanding timers. Permission changes and expiry clear viewer visibility and buffered activity. Approving a capability again does not restore an earlier sharing session: explicitly enable visibility again when ready.

The server checks deferred source installs again when they execute. Revoking delegation while a job waits prevents that job from starting. A Git operation that already began may finish staging source; staging never executes an installer.

## Manifest declarations and settings

Catalog entries and SDK manifests use the same declaration array:

```json
{
  "permissions": [
    {"id": "input", "description": "Control this game's window", "required": true},
    {"id": "vision", "description": "Observe this game's window", "required": false}
  ],
  "settings_schema": {
    "type": "object",
    "properties": {"show_activity": {"type": "boolean"}},
    "additionalProperties": false
  }
}
```

Supported capability IDs are `input`, `game`, `vision`, `shell`, `filesystem`, `process`, `network`, `system`, `extensions`, and `discord.viewer`. Unknown or duplicate capability IDs are rejected. Optional capabilities must remain unavailable unless granted; an SDK adapter must also honor the applicable global permission when exposing an optional feature.

Settings are bounded JSON data. The server validates types, enum choices, required fields, properties, additional properties, array items, numeric bounds, and maximum string length. Unsupported schema keywords are descriptive metadata and do not create additional enforcement. Keep schemas within that supported subset when validation matters.

Store secrets in the integration's environment or its own credential store. Settings must not contain passwords, OAuth tokens, API keys, operator credentials, or authorization headers. A secret-related setting may identify an environment variable with an `_env` suffix, such as `discord_token_env: "DISCORD_TOKEN"`; the policy stores only the variable name. `NEURO_*` references are reserved for the server and refused. Settings are not interpolated into shell commands or automatically exported to child processes.

## Existing Memory installations

The previously shipped signed Memory extension has a narrow migration: an already installed, enabled, verified `memory` item with no permission declarations may retain runtime approval if it has no explicit policy. It gains no management delegation. An explicit policy, including a revocation, always takes precedence. New installations stay disabled and require approval before enabling.

The manager and model-facing catalog details correct that entry's earlier wording about who can enable it. This is a presentation transform of the known legacy sentences. The stored signed item stays unchanged, and signature verification uses its original bytes. A display API response is therefore not a replacement for the original signed catalog document.

## Trust boundary

An operator session and a delegated manager session have different credentials. Give Neuro or Evil a delegated session for UI management; keep the operator token out of their prompts, settings, adapter environments, and screenshots. The server authenticates credentials and cannot establish which physical person typed or clicked in a signed-in window. Fresh token entry prevents an ordinary stored UI session from silently granting new privileges; it does not prove human identity.

These controls govern Neuro Desktop's managed starts, API calls, and feature contracts. They do not sandbox an external integration process at the operating-system level. An integration running with your account can retain its own independent backend and permissions. Use the host OS's isolation and account boundaries when process-level restrictions are required.
