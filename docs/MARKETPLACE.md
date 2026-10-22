# Marketplace and dedicated integration workflow

Neurcro gives Neuro and Evil general desktop controls first. Dedicated
integrations add precise application state and higher-level actions when you
choose to connect them. Existing Minecraft, osu! or private stream integrations
can continue using your current setup; you do not need to publish their source
or replace Vedal's backend to associate them with the Games tab.

In the manager, speak to you directly: “Enable this extension” and “Allow Neuro
to manage this extension.” Action descriptions and results sent to Neuro should
address her separately. “You” in manager copy means the human operating it.

## Index and release branches

Keep the upstream source directory on the main repository's `catalog` branch.
Use pull requests to that branch for new listings and reviewed version updates.
Application changes go through `dev`, then a release PR targets `master`, matching
the branch process documented by
[NeuroPilot](https://github.com/VSC-NeuroPilot/neuropilot/blob/0b0abd373a440d13614080b4767a284cca17e538/.github/README.md).

The index is separate from the runtime's signed `desktop/catalog/index.json`.
The source directory records useful upstream projects even when an installer
does not exist. The signed runtime catalog authorizes code that Neurcro
may fetch or run. A source listing must not acquire execution privileges merely
because its repository is in the directory.

For each index update:

1. Read the upstream license, README, runtime requirements and relevant changes.
2. Pin the complete source commit. Link setup instructions to that same commit.
3. Record platform evidence, game associations, connection settings and remaining
   tests. Keep undocumented platforms and runtime results explicitly unverified.
4. Run `python3 marketplace/validate.py` and the marketplace test suite. Review
   changes in a pull request before updating the canonical branch.
5. Copy the reviewed source index into an application release as a snapshot.
   Record the index commit in release notes. Prepare and sign managed catalog
   entries separately after their adapters and installers pass review.

A future automatic updater should retrieve a commit-pinned snapshot, validate
its schema and trust, then replace it atomically. Keep the last valid index if
the update fails. Do not run remote install scripts from a README or automatically
replace the trusted publisher keys when refreshing metadata.

## Initial source listings

All eight requested repositories are present in
the shipped [`desktop/catalog/marketplace.json`](../desktop/catalog/marketplace.json)
snapshot. Its source and validator live on the independent
[`catalog` branch](https://github.com/Nakashireyumi/neuro-desktop/tree/catalog).
This review inspected
public source metadata and documentation on 2026-10-15; it did not run the games.

| Listing | Required setup and limits |
| --- | --- |
| [Neuratro](https://github.com/VX1D/neuro-sdk-neuratro/blob/12a558e5f9ad5e49ab6442a5fbc519b088620d4a/README.md) | Balatro, Lovely 0.9.0, Steamodded and a Rust bridge. Windows instructions are documented. The actual LICENSE adds a modification condition despite its MIT badge. |
| [Neuro Companion](https://github.com/Pekotaker/NeuroCompanion/blob/645181e7ff9b92ee0066537b5a05a214e0d42649/README.md) | Terraria/tModLoader companion with optional WebSocket control. Configure the mod and summon the companion in a world. No license was detected; do not redistribute it from this directory. |
| [Don't Starve Neuro](https://github.com/EthLeigh/dont-starve-neuro/blob/500892a001a57d3bef017b0075ec80ac8ec2501c/README.md) | Use the single-player `single` branch, mod and Node.js bridge. The default branch is discontinued co-op. Port settings must agree between mod and bridge. |
| [Stardew integration](https://github.com/pandapanda135/neuro-stardew-integration/blob/183fe203119bae2b3ea3236a0b5c9373f3f6bc01/README.md) | SMAPI 4.3.2 and a C# mod. Upstream identifies incomplete testing and unsupported multiplayer. No published release was available at review. |
| [Skyrim integration](https://github.com/vituha230/Neuro-Skyrim-Integration/blob/cff51c45afe9a6e69983bb0f9609208cced1e445/README.md) | Windows Skyrim SE 1.6.1170, matching SKSE, XMake and C++23. Deployment changes game scripts and load-order files. Other game versions/platforms remain unverified. |
| [NeuroMCP](https://github.com/ECHO-HELLO-WORLD424/NeuroMCP/blob/2ef4ab8bf9f05a2beff885e64671d5293808c65f/README.md) | Python 3.10+, a configured Streamable HTTP MCP server and Neuro WebSocket endpoint. Review the selected MCP server's tools and access. This is a separate bridge, not a stdio MCP server. |
| [neurontainer](https://github.com/VSC-NeuroPilot/neurontainer/blob/c7e6b59f12825c5b6189783653ca5f617168973e/README.md) | Docker Desktop extension with Docker daemon access. A headless executable is not released. Upstream latest-tag commands need a digest-pinned recipe before managed installation. |
| [NeuroPilot](https://github.com/VSC-NeuroPilot/neuropilot/blob/0b0abd373a440d13614080b4767a284cca17e538/package.json) | VS Code 1.95 or compatible host, its extension and a Neuro endpoint. Its own editor and terminal permissions stay authoritative. Desktop and web entry points do not certify every OS. |

## SDK and adapter boundary

Use the [official Neuro API](https://github.com/VedalAI/neuro-sdk/blob/main/API/SPECIFICATION.md)
for game actions and context. The separate
[Neurcro SDK](https://github.com/cassitly/neuro-desktop-sdk) adds a versioned
manifest, scoped configuration, lifecycle health and Games tab association.
Existing integrations keep their action protocol and private backend. Follow the
[workflow](WORKFLOW.md#add-an-integration-or-marketplace-listing) to register an
adapter and test its readiness, grants and revocation against the server.

For a managed integration, the adapter needs:

- Stable extension ID, version, upstream source commit, license and supported
  platform/architecture combinations.
- A game profile ID and the integration's stable **display name** from the Neuro
  API. The API display name may contain spaces and is not necessarily the
  marketplace slug. Preserve it when connecting through your relay.
- Exact runtime command/arguments, working directory, bounded health/start/stop
  behavior and a list of environment variables that may be passed through.
- Named settings with types, defaults, validation and secret markers. Store
  credential values locally, outside the shared index and signed manifest.
- Explicit install destinations, approved package artifacts and hashes,
  prerequisites, rollback and uninstall behavior. Do not silently overwrite game
  saves, load-order files or an existing manually managed installation.
- Per-extension capabilities with a reason and enforcement point. A declaration
  alone cannot limit arbitrary native code; do not describe it as a sandbox.

The existing Games profiles support `nd`, `external`, `hybrid` and `auto` control
modes. Associate your integration's relay display name with
`control.external_integration`; `auto` can hand off when that peer is connected.
Use `external` when your existing integration always owns control. In `hybrid`,
list the precise Neurcro actions that may inject input. Do not allow two
independent controllers to issue movement simultaneously by accident.

Minecraft and osu! need no fabricated public repository listing for your private
integrations. You can provide a local manifest/profile and connection adapter
when ready. Keep connection identity, ownership and health visible in Games.

## Permissions and self-management

Neuro can browse, install, enable, disable and remove extensions through the
existing Neuro actions when you have granted the relevant permission. She can
also use the manager UI through desktop input, just as she can use other apps.
The same grants should apply to both entry points. The manager must distinguish
your authenticated operator session from a delegated Neuro session so using the
UI cannot grant her additional powers.

Grant management separately from an extension's runtime abilities. An extension
may need only a local connection and game state; another may edit files, launch
processes, manage Docker or publish viewer activity. Review those independently.
Current grants are keyed by item ID; a newly required capability blocks runtime
until you grant it. SDK manifests do not yet bind approval to a version/digest,
so review manifest replacements explicitly. Signed runtime entries and pinned
source commits provide separate distribution checks. Revoking runtime permission
stops affected managed MCP services and clears adapter readiness and viewer
sharing; separately installed applications remain under their own process and
permission controls. Installation and grant changes are audited without tokens,
secret settings or private message contents.

Index capabilities are **review notes inferred from source**, not claims that
upstream authors adopted an SDK manifest or that Neurcro enforces those
limits on separately installed programs. Author-declared capabilities and actual
runtime grants belong to the managed adapter contract.

## Optional Discord viewer integration

Keep the Vencord viewer plugin and backend adapter in a separate repository.
[Vencord](https://github.com/Vendicated/Vencord) is a GPL-3.0 Discord client mod;
follow its plugin development and licensing requirements for code linked into
it. Do not turn a viewer plugin into a replacement for the private Discord
integration or ship Discord account tokens in a client plugin.

The public contract can carry only an approved activity projection: persona,
public application/game label, short activity text, sharing state and a timestamp
or expiry. The private backend adapter decides what content is eligible. Keep
message text, channel/member IDs, screenshots, raw actions and credentials out
of that projection unless a separate explicit feature approves them.

Give Neuro/Evil an explicit sharing toggle within your granted policy, and give
you an immediate revoke control. Default to hidden, clear the projection when
sharing stops, expire stale activity and never replay private backlog after
reconnection. A viewer build should consume a read-only projection and never
receive the manager token. The private backend remains the final part you wire
in; fixtures can test the public protocol without access to it.

## Validation before managed release

Test adapters with a scripted Neuro API server before a live model is available:
startup, registration, matching action IDs/results, reconnect, duplicate action
delivery, permission denial, pause, release, revocation and shutdown. Use the
official SDK's action timing guidance; actions may arrive before or after a
force request, so state mutation must not depend on force timing.

Run actual installer/start/stop/uninstall tests on Windows, macOS and Linux for
each platform claimed by a package. A Linux desktop-manager pass does not verify
a Windows SKSE plugin, Docker Desktop image or game mod. Publish per-platform
results and unresolved requirements with release artifacts.
