# Production TODO

Last verified: 2026-03-09

## GitHub Issues Verification

### `Nakashireyumi/neuro-desktop` (14 open)

1. [#22](https://github.com/Nakashireyumi/neuro-desktop/issues/22) `Fix critical bugs and code quality issues from PR #19 review`
   - Status: likely stale for this branch.
   - Verification: references files from older layout (`repository-setup.js`, legacy python client) that are not part of the current Rust/Go/Python rewrite tree.

2. [#21](https://github.com/Nakashireyumi/neuro-desktop/issues/21) `Actions that enables the keyboard fails`
   - Status: partially fixed in this fork.
   - Fixes here: key validation in Python keyboard controller + Rust IPC now propagates execution/clear failures instead of silently discarding them.
   - Remaining: end-to-end test against live desktop interaction.

3. [#20](https://github.com/Nakashireyumi/neuro-desktop/issues/20) `Implement Permissions System for Neuro Desktop`
   - Status: partially fixed in this fork.
   - Fixes here: policy file support (`NEURO_PERMISSIONS_FILE`) with allow/deny/default behavior and action-level enforcement in Go integration.
   - Remaining: richer scopes (filesystem/process/network), UI policy editor, signed policy distribution.

4. [#16](https://github.com/Nakashireyumi/neuro-desktop/issues/16) `UI token synchronization issues`
   - Status: partially fixed in this fork.
   - Fixes here: optional bundled relay runtime (`NEURO_RELAY_ENABLED`) with automatic process supervision and restart.
   - Remaining: UI token synchronization flow and relay-first UX defaults.

5. [#15](https://github.com/Nakashireyumi/neuro-desktop/issues/15) `Neuro Desktop SDK`
   - Status: not fixed yet.
   - Remaining work: plugin SDK + versioned API.

6. [#14](https://github.com/Nakashireyumi/neuro-desktop/issues/14) `Neuro Integrations Manager`
   - Status: not fixed yet.
   - Remaining work: package/index manager and install/update flow.

7. [#13](https://github.com/Nakashireyumi/neuro-desktop/issues/13) `virtualized environment`
   - Status: not fixed in this repository.
   - Remaining work: external environment integration.

8. [#12](https://github.com/Nakashireyumi/neuro-desktop/issues/12) `permissions`
   - Status: partially fixed and overlaps issue #20.
   - Remaining work: same as #20.

9. [#11](https://github.com/Nakashireyumi/neuro-desktop/issues/11) `UI`
   - Status: not fixed yet.
   - Remaining work: production UI and marketplace UX.

10. [#8](https://github.com/Nakashireyumi/neuro-desktop/issues/8) `higher-level action abstractions`
    - Status: partially fixed in this fork.
    - Fixes here: added high-level script commands (`OPEN_WINDOWS_MENU`, `SHOW_DESKTOP`, `MINIMIZE_ALL_WINDOWS`, `CLOSE_FOREGROUND_APP`, `OPEN_TASK_MANAGER`, `CLOSE_ALL_APPS`) plus tests/docs.
    - Remaining: policy gating and richer intent planner.

11. [#5](https://github.com/Nakashireyumi/neuro-desktop/issues/5) `Setup file`
    - Status: likely outdated.
    - Verification: mentions `windows-api` package setup from older architecture; current codebase uses bundled Python runtime and separate modules.

12. [#4](https://github.com/Nakashireyumi/neuro-desktop/issues/4) `Action schema`
    - Status: fixed in this fork.
    - Fixes here: added `integration-docs/action-schema.run_script.json` and aligned script documentation with parser behavior.

13. [#3](https://github.com/Nakashireyumi/neuro-desktop/issues/3) `Move neuro-specific configs over from windows-api`
    - Status: likely outdated for current architecture.

14. [#2](https://github.com/Nakashireyumi/neuro-desktop/issues/2) `Provide built-in neuro integrations`
    - Status: partially fixed in this fork.
    - Fixes here: optional relay bundling/startup path in Rust app and bundle scripts.
    - Remaining: curated integration catalog and installer UX.

### `cassitly/neuro-desktop`

- Open issues: 0

### `Nakashireyumi/neuro-relay` (1 open)

1. [#6](https://github.com/Nakashireyumi/neuro-relay/issues/6) `Nakurity ID Server`
   - Status: not fixed in this repository.
   - Remaining work: belongs to relay backend and identity architecture.

### `recassity/neuro-relay`

- Open issues: 0

### `Ubuntufanboy/neuro-desktop`

- Open issues: 0

## GitHub PR Verification

### `Nakashireyumi/neuro-desktop` (3 open PRs)

1. [#28](https://github.com/Nakashireyumi/neuro-desktop/pull/28) `Completely refractor Neuro Desktop`
   - Status: open, non-draft, mergeable `clean` (as of 2026-03-09).
   - Scope: large refactor (134 commits, 128 changed files).

2. [#24](https://github.com/Nakashireyumi/neuro-desktop/pull/24) `CodeRabbit Generated Unit Tests: Add comprehensive pytest test suite for core modules`
   - Status: open, non-draft, mergeable `blocked` (as of 2026-03-09).
   - Scope: test-only addition against `master`.

3. [#19](https://github.com/Nakashireyumi/neuro-desktop/pull/19) `CodeRabbit Chat: Disable Python not found error and regionalization setup`
   - Status: open, non-draft, mergeable `clean` (as of 2026-03-09).
   - Scope: small compatibility/config patch set.

### `cassitly/neuro-desktop` (1 open PR)

1. [#4](https://github.com/cassitly/neuro-desktop/pull/4) `Implement object detection with direct prompts using YOLOE`
   - Status: open, non-draft, mergeable `clean` (as of 2026-03-09).
   - Scope: vision/object-detection focused.

### `Nakashireyumi/neuro-relay` (2 open PRs)

1. [#10](https://github.com/Nakashireyumi/neuro-relay/pull/10) `Major Refactor: Complete Go Rewrite of NeuroRelay (v0.1.0-alpha)`
   - Status: open, draft, mergeable `unstable` (as of 2026-03-09).
   - Scope: major refactor (83 commits, 41 changed files).

2. [#9](https://github.com/Nakashireyumi/neuro-relay/pull/9) `Convert project to not use submodules and add missing type annotations`
   - Status: open, draft, mergeable `clean` (as of 2026-03-09).
   - Scope: repository and typing refactor.

### `recassity/neuro-relay`

- Open PRs: 0

### `Ubuntufanboy/neuro-desktop`

- Open PRs: 0

## GitHub Releases Verification

### `Nakashireyumi/neuro-desktop`

- `0.0.3a-dev` (prerelease), published 2025-11-02
- `0.0.2-alpha` (prerelease), published 2025-10-22
- `0.0.1-alpha` (prerelease), published 2025-10-18

### `cassitly/neuro-desktop`

- No releases published (as of 2026-03-09).

### `Nakashireyumi/neuro-relay`

- `0.0.1-alpha` (prerelease), published 2025-10-19

### `recassity/neuro-relay`

- No releases published (as of 2026-03-09).

### `Ubuntufanboy/neuro-desktop`

- No releases published (as of 2026-03-09).

## Completed In This Fork

- [x] Fix Rust build compatibility with Python 3.14 (`.cargo/config.toml`).
- [x] Fix C++ process-handler test target linker failures.
- [x] Add baseline Windows CI for Rust/Go/Node/C++.
- [x] Stop masking Rust IPC execution/clear errors.
- [x] Add high-level desktop action commands in Python action parser.
- [x] Expand high-level desktop action coverage for Windows workflow commands (settings, notification center, clipboard, app switching, screen snip, power menu, lock).
- [x] Add action parser unit tests for high-level commands and click syntax.
- [x] Add run_script action payload JSON schema.
- [x] Align action script docs with parser behavior.
- [x] Make integration docs loading non-fatal with path fallbacks.
- [x] Add Go tests for integration documentation loading.
- [x] Add initial permission policy loading/enforcement for integration actions.
- [x] Add optional Neuro Relay process supervision and relay-backed integration routing.
- [x] Migrate Go integration module to `neuro-integration-sdk` (with local compatibility patch for command hooks/action acknowledgments).
- [x] Add catalog metadata and catalog actions (`list_catalog_items`, `find_catalog_items`, `get_catalog_item`) with tests.
- [x] Connect process-handler supervised runtime mode (`neuro-desktop --supervised`) and bundle `process-handler.exe`.
- [x] Implement desktop context pipeline: Rust `get_status` desktop snapshot + Go periodic `context` sender (`NEURO_CONTEXT_POLL_SECONDS`).
- [x] Add optional vision bridge (`NEURO_VISION_SERVER_URL`) for screenshot summarization into Neuro context.
- [x] Add extension management actions (`install/enable/disable/uninstall/list`) with persistent state file and MCP plugin catalog entry.
- [x] Replace placeholder frontend with startup splash + management UI for ND/relay/extensions (React + Vite).

## Remaining Production Work

- [ ] Permission model and runtime enforcement (#12/#20).
- [ ] UI token synchronization and relay-first onboarding (#16).
- [ ] Integration manager and marketplace/app-store flow (#11/#14/#15), including signed plugin distribution.
- [ ] Vision pipeline confidence loop and formalized server contract.
- [ ] Security hardening: signed bundles, config integrity, least-privilege defaults.
- [ ] Native tray/minimize-to-notification-area shell (current UI is browser-launched).
- [ ] Release quality gates: coverage target, smoke E2E, installer validation, changelog discipline.
