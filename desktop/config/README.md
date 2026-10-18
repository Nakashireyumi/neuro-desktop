# Configuration

The server has no configuration file. It reads its settings from environment
variables and command-line flags. The full list is in [`../README.md`](../README.md#configuration).

## Files

| File | Purpose |
|------|---------|
| `permissions.example.json` | Safe-by-default operator policy for Vedal. The bundle copies it to `permissions.json` |

## The policy

The policy is a JSON file (`NEURO_PERMISSIONS_FILE`) with:

- `default_allow`: whether an action with no scope is allowed (`false` by default);
- `allowed_actions` and `denied_actions`: explicit lists by action name;
- `scopes`: one entry per capability category (`input`, `game`, `vision`, `process`,
  `network`, `filesystem`, `system`, `shell`, `extensions`), each with
  - `allowed`: whether the scope is on now;
  - `requestable`: whether Neuro may ask Vedal for it with `request_permission`;
  - `limits.max_actions_per_minute`: an optional per-scope rate limit.

The example turns on input, process, network, vision, and game. It makes filesystem and
extensions requestable, and it leaves shell and system off and not requestable. Changes
made on the dashboard are written to that file (`NEURO_PERMISSIONS_FILE`) and take effect at
once. Keep a copy of the file before you edit it on the dashboard.

See `../docs/SAFETY.md` for what each scope means.
