# Runtime configuration

Copy `integration-config.yml` next to the `neuro-desktop` binary (or keep it under
`config/` in a release bundle).

## Files

| File | Purpose |
|------|---------|
| `integration-config.yml` | Neuro WebSocket URL and package metadata |
| `permissions.example.json` | Safe-by-default operator policy for Vedal |

## Permissions

`neuro-integration` loads `NEURO_PERMISSIONS_FILE` (default: `permissions.json`
beside the binary). Export from the frontend Permissions page, or copy the
example:

```bash
cp config/permissions.example.json ./permissions.json
```

Schema (shared with the UI export):

- `default_allow` — fallback when an action has no scope mapping
- `scopes` — capability categories (`input`, `filesystem`, `process`, `network`, `system`, `vision`)
- `allowed_actions` / `denied_actions` — per-action overrides (`denied` wins)

See [VISION.md](../../VISION.md) for the bridge vs executor split.
