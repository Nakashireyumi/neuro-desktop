# Vision Service controls

Open **Extensions → Built-in services → Vision Service** in the native manager
or browser dashboard. This default built-in service controls image capture and
analysis in Neurcro. It is separate from installable extensions, so it has
no install or uninstall action.

You can switch the service off globally, or allow it separately for Neuro and
Evil. Saving requires a fresh operator-token entry. A delegated manager session
can inspect the state but cannot change these decisions. Your choices survive a
server restart and bundle rebuild.

Disabling prevents new image work and invalidates pending results. It does not
terminate an external vision-server process, uninstall a model, or revoke other
programs' access to that server. A request already sent may have disclosed its
image; cancellation cannot recall bytes. Neurcro discards the outdated
result, including when you disable and re-enable the service while it is running.

## Choose the connection persona

The official Neuro action envelope does not identify which twin sent an action.
Neurcro therefore uses your server startup setting, never an `actor`
parameter supplied by a model:

```sh
NEURO_PERSONA=neuro ./neuro-integration
# Equivalent: ./neuro-integration --persona neuro
```

Choose `neuro`, `evil`, or `shared`. The default is `shared`: image use requires
both actor switches to be allowed. This fails closed when the server cannot
distinguish them. To permit one twin independently, configure separate trusted
connections through your backend and set the corresponding server persona.
Apply the policy to each server instance you operate; editing one server does
not remotely configure another instance. A persona label is your routing
assertion, not identity detection by Neurcro.

The card's effective state describes this service decision. The global `vision`
permission, action policy, pause state, client capture support, and analysis
endpoint are additional requirements. Metadata-only observations, such as
window/process detection or `game_observe` with `vision:false`, still use the
existing global Vision permission and do not require image analysis.

## Run an analysis server

The image path already sends PNG bytes from the controlled PC through the bridge
to the separate HTTP vision service. No shared screenshot directory is required.
Set `NEURO_VISION_URL`, for example `http://127.0.0.1:8610/describe`, on the bridge.
Set the same `NEURO_VISION_TOKEN` on both programs when authentication is enabled.
Use a protected connection when crossing machines; the default local HTTP
connection provides no transport encryption.

The service can report image dimensions/statistics, perform OCR, or call a
vision-enabled Ollama or OpenAI-compatible endpoint. These backends have different
dependencies and capabilities. A statistics response does not recognize what an
application is doing. Follow the [vision server setup and backend guide](../headless/apps/nd-vision-server/README.md)
for exact settings. Model-backed semantic analysis and real Windows/macOS capture
still require platform/backend acceptance testing.

The default analysis deadline is 40 seconds. The bridge waits 45 seconds and the
native manager waits 50 seconds for an observation, so the server stops first. If
the configured model cannot answer in that budget, the request returns 504 instead
of continuing invisibly; concurrent work beyond the configured slots returns 503.
An operator may place fixed, trusted servers behind `--backend cluster`. The
bridge never uses a URL supplied by a model, and the cluster backend sends one
image to one worker with at most one sequential failover, never a broadcast.

## Storage and API

Settings live in `builtin-services.json` beside the extension state file. Set
`NEURO_BUILTIN_SERVICES_FILE` to select another file. A missing file uses enabled
defaults for both actors; the global permission system still applies. Invalid
settings fail closed with a diagnostic. Use the manager while the server is
running; do not edit its state file concurrently.

`GET /api/services/vision` returns configuration, connection persona, effective
service state and any disabled reason. It performs no analysis or active health
probe. `GET /api/extensions` includes the same state under
`built_in_services.vision`, allowing the existing page refresh to display it
without a separate poll loop.

An operator tool can send this body to `PUT /api/services/vision`:

```json
{"enabled":true,"actors":{"neuro":true,"evil":false}}
```

All three booleans are required. Authenticate with your dashboard token and
explicitly re-entered `X-ND-Operator-Token`, as with other permission changes.
Delegated tokens cannot write this route. Endpoint credentials remain in operator
configuration and are never returned by this settings API.
