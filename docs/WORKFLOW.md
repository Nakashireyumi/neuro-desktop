# Development, integration, and release workflow

Neuro Desktop gives Neuro and Evil general control of applications and operating systems. Dedicated game and application integrations are optional: they provide more precise state and actions when available. Keep the desktop controls useful without requiring a dedicated integration for every app.

The native manager is a separate GTK application with its own startup splash. The server owns Neuro API connections, permissions, the executor hub, the marketplace, and local management APIs. The packaged Python client runs on the computer being controlled. The browser dashboard remains an optional management surface. See [architecture](ARCHITECTURE.md) and [deployment](DEPLOYMENT.md) for split-machine layouts.

## Repositories and branches

| Purpose | Repository or branch |
| --- | --- |
| Application and official releases | [Nakashireyumi/neuro-desktop](https://github.com/Nakashireyumi/neuro-desktop), protected `master` |
| Working fork | [cassitly/neuro-desktop](https://github.com/cassitly/neuro-desktop) |
| Current development PR | [Fork #6](https://github.com/cassitly/neuro-desktop/pull/6), feature branch into fork `dev` |
| Existing upstream development PR | [Upstream #28](https://github.com/Nakashireyumi/neuro-desktop/pull/28), fork `dev` into upstream `dev/latest` |
| Canonical marketplace directory | Upstream application's separate `catalog` branch |
| Standalone integration SDK | [cassitly/neuro-desktop-sdk](https://github.com/cassitly/neuro-desktop-sdk) |
| Standalone Discord viewer adapter/plugin | [cassitly/neuro-desktop-vencord](https://github.com/cassitly/neuro-desktop-vencord) |

Use short-lived `codex/` feature branches for new application work. Reuse the existing development PRs while consolidating this work; their descriptions must describe the final code and verification. The final release PR must explicitly target **`Nakashireyumi/neuro-desktop:master`**. A PR to the fork's `master` does not update upstream.

Upstream's development branch is currently named `dev/latest`; the fork's is `dev`. Check the actual refs before choosing a base. The [branch review](BRANCH_REVIEW.md) records the earlier master revert, the existing PRs, duplicate branches, and unique work that needs preserving. It is a dated snapshot, not a reason to reset a newer branch.

This workflow uses the development/release separation described in [NeuroPilot's contributor workflow](https://github.com/VSC-NeuroPilot/neuropilot/blob/0b0abd373a440d13614080b4767a284cca17e538/.github/README.md). Neuro Desktop builds its own native programs and promotes the exact tested archives into a draft release.

## Make and verify a change

1. Inspect the working tree, branch, and remotes. Preserve unrelated local files and uncommitted work. In particular, local build output, private configuration, and separate SDK/catalog repositories do not belong in an application commit.
2. Bring in reviewed work already present on the feature's remote before publishing. The branch audit found Go-client commit `13327aa` ahead of the original checkout; retain that history and its tests. The initial Go client is an incremental port, while the Python client still supplies the shipping desktop input and capture implementation.
3. Make the smallest complete change. Add features when they help real desktop or integration workflows. Keep background work bounded; hidden pages should not repeatedly poll or capture the screen.
4. Run the relevant checks below, then the repository's complete CI. Exercise the native UI when changing a view, and test permission denial and recovery alongside the successful path.
5. Update behavior, setup, and troubleshooting documentation with the code. Record platform results precisely. A cross-compile or headless test does not establish that real input and screen capture work on another OS.

From the repository root:

```sh
NEURO_HEADLESS=1 ./desktop/scripts/build-all.sh
python3 -m unittest discover -s desktop/tools/ci -p 'test_*.py'
python3 desktop/tools/ci/repo_checks.py
```

For a backend change:

```sh
cd desktop/apps/neuro-integration
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...       # a platform with cgo support
go test -tags neurodev -count=1 ./...
```

For the native manager, pure client tests need no GTK. GUI tests need GTK 4/PyGObject and a display, or Xvfb on Linux:

```sh
PYTHONPATH=desktop/apps/neuro-manager python3 -m unittest discover -s desktop/apps/neuro-manager/tests
PYTHONPATH=desktop/apps/neuro-manager NEURO_MANAGER_GUI_TESTS=1 /usr/bin/python3 -m unittest discover -s desktop/apps/neuro-manager/tests
```

Select the GTK-compatible Python described in the [manager README](../desktop/apps/neuro-manager/README.md); `/usr/bin/python3` above is a Linux example. Windows uses the PowerShell build scripts and MSYS2's matching GTK/Python for the manager. The controller's CPython environment stays separate from the manager's GTK environment.

CI's **Release gate (every required job succeeded)** is the required aggregate result. It fails if a required job fails or is skipped, including platform builds, native manager controls, real executor transport, SDK/Vencord checks, catalog trust, relay, vision, and bundles. Requiring this gate on maintained branches makes partial green results easier to identify.

## Add an integration or marketplace listing

Keep the [official Neuro API](https://github.com/VedalAI/neuro-sdk/blob/main/API/SPECIFICATION.md) as the action/context protocol. The Neuro Desktop SDK adds a manifest, scoped presence and health, Games associations, and an optional curated activity feed. It does not require publishing a private game or Discord backend.

For an existing Minecraft, osu!, or private integration, add a local SDK manifest and adapter around the existing connection. Preserve its Neuro API game display name and register the matching game IDs. Verify the dedicated integration is healthy before handing input ownership to it. Test that disconnecting returns ownership safely and that Neuro Desktop and the dedicated integration do not issue duplicate input.

For a marketplace update, work on a feature branch based on the canonical `catalog` branch and submit a PR back to `catalog`. Review the exact upstream commit, its license and prerequisites, then update the source entry and commit-pinned instructions. Run the directory's validator and tests in that branch. Keep platform results explicitly unverified until tested. See the [marketplace guide](MARKETPLACE.md).

Application releases carry the reviewed snapshot at `desktop/catalog/marketplace.json`. Updating that snapshot is a separate reviewed application change; record the canonical catalog commit in its PR. A mutable upstream branch or repository URL is not a source pin. Fetch SDK, Vencord, and catalog validation sources at their reviewed full commit IDs in CI. The application working tree ignores local `ecosystem/` and `marketplace/` checkouts so those remain separate repositories/branch content.

A source listing does not install game assets, run an installer, certify a build, or grant permissions. Managed source staging fetches only an approved pinned commit and executes no setup script. Runtime entries in the signed catalog, publishers, and permission policies remain separate from the source directory. Re-sign a managed entry after changing signed fields; display-only compatibility wording does not modify the original signed Memory entry.

## Delegate management and viewer sharing

You approve global scopes and each item's runtime capabilities. Delegate management separately: Neuro or Evil can then install, stage, enable, disable, and remove that approved item through model actions or a delegated manager session. They cannot grant themselves new capabilities. An install starts disabled.

Use the delegated UI session for autonomous management. Grant changes, integration credential creation, and other operator decisions require fresh operator-token entry. The server authenticates credentials; it does not establish the physical identity of someone clicking a signed-in window. Keep operator credentials out of prompts, source manifests, settings, viewer feeds, and adapter environments. See [delegated manager sessions](DELEGATED_MANAGER.md) and [extension permissions](EXTENSION_PERMISSIONS.md).

Discord viewer sharing is a separate opt-in projection. Default to hidden, expire stale activity, and test immediate hiding and revocation. The Vencord adapter does not implement or replace the private Discord backend. Test it with a local fixture feed first, then connect eligible activity from the private backend through the SDK when ready. Keep message text, credentials, channel/member IDs, and raw screenshots outside that projection.

## Build release candidates

The [release workflow](../.github/workflows/release.yml) runs only through `workflow_dispatch`. It never starts a release on a push, tag, schedule, or pull request. The workflow must first exist on the repository's default branch to be dispatchable. Select the exact branch commit whose latest normal CI run and aggregate release gate have succeeded.

To build candidate artifacts in the fork:

```sh
gh workflow run release.yml --repo cassitly/neuro-desktop --ref dev \
  -f version=v0.2.0-rc.1 -f publish=false
```

Forks and non-master refs produce Actions artifacts only, even if `publish=true` is requested. The workflow checks the selected commit's latest CI before spending time on platform builds.

The matrix builds Linux x64, Windows x64, macOS ARM64, and macOS Intel packages on native runners. Each package includes the native GTK manager, Go server/dashboard programs, the frozen controller, configuration examples, catalog snapshots, and documentation. The scripts' source-only fallback is not acceptable for these releases: the workflow fails if a required frozen program is missing.

The core programs include their Python/GTK runtime. Feature-specific prerequisites, such as Node.js for the Memory MCP server or a game's mod loader, remain the prerequisites recorded by that feature and its marketplace listing.

Each platform runs backend and controller checks, native GTK control tests, and packaged-program smoke tests. Those smoke tests verify the signed catalog, authentication, the dashboard proxy, and a real connection from the frozen executor to the frozen server, without injecting desktop input. A real Neuro backend and private integrations are not required for these checks.

The tested bundle is archived once. Linux/macOS use tar to preserve executable modes and application symlinks; Windows uses ZIP. Actions uploads the archives with overwrite disabled. The draft job downloads these exact artifacts and does not rebuild them. For a fresh complete build, dispatch a new workflow run rather than overwriting a successful run's artifacts.

Each target supplies:

- Its portable archive.
- A `.build.json` manifest identifying the source SHA, workflow run, prior CI run, target architecture, tool versions, resolved agent packages, GTK package versions, and hashes of dependency/catalog inputs.
- A `.sha256` file for the archive and build manifest. The archive also includes `BUILD.json` and the application license.

Node uses `npm ci` with the committed package lock. Go uses module sums and read-only dependency resolution. Direct controller dependencies and PyInstaller are pinned. Python transitive dependencies and OS GTK packages are currently captured in the build manifest rather than fully hash-locked, so rebuilding the same source is not guaranteed to produce identical bytes. The release promotes the existing tested bytes. Platform signing/notarization is not configured by this workflow.

## Prepare an official draft release

After the upstream master PR has merged and its CI has passed:

1. Update the server `Version` and manager `__version__` to the intended release version without the leading `v`, plus relevant changelog and setup notes. Publishable builds verify those values against the requested version. Complete the same review and CI process for that version change.
2. Confirm the version is unused and dispatch **upstream `master`**, explicitly requesting publication:

   ```sh
   gh workflow run release.yml --repo Nakashireyumi/neuro-desktop --ref master \
     -f version=v0.2.0-rc.1 -f publish=true
   ```

3. The draft job requires the literal upstream repository, `refs/heads/master`, protected master, successful platform builds, the successful CI gate for the exact source SHA, matching manifests/checksums, and an unused version. It refuses an existing tag or release, including a draft.
4. GitHub provenance is generated for the tested assets and verified against the upstream repository, release workflow, master ref, and source SHA. The job then creates a **draft** and uploads the archives, manifests, checksums, and attestation bundle. It never publishes a public release automatically.
5. Review the draft's assets and notes. Check real desktop input/capture on the intended environments, platform installation behavior, and any signing/notarization requirements before making it public. Publish the existing reviewed draft assets; do not replace them with local rebuilds.

An upload failure can leave an incomplete draft. Inspect it explicitly before recovering; the workflow refuses to overwrite it or silently move its tag. All release authentication uses the job token through `GH_TOKEN`, with write permissions limited to the gated draft job. Tokens never appear in repository URLs.

After downloading an official archive, verify its checksum and provenance. For example, use `sha256sum -c` for the matching `.sha256` file on Linux, then:

```sh
gh attestation verify neuro-desktop-v0.2.0-rc.1-linux-x64.tar.gz \
  --repo Nakashireyumi/neuro-desktop \
  --signer-workflow Nakashireyumi/neuro-desktop/.github/workflows/release.yml \
  --source-digest THE_REVIEWED_SOURCE_SHA --source-ref refs/heads/master
```

Download the matching build manifest too when checking a `.sha256` file that covers both assets. GitHub documents [artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations) and the [CLI verification flags](https://cli.github.com/manual/gh_attestation_verify). Provenance identifies the source and workflow; it does not replace code or runtime review.

## Synchronize and clean branches

Refresh the branch inventory after the upstream merge. Update the fork's maintained `master` and `dev` with normal merges or fast-forwards from the reviewed upstream result. Check commit ancestry before updating an older feature branch. If a branch has unique work, preserve it in an archival ref and review what remains before deleting or superseding it.

Close or supersede obsolete PRs before deleting their heads. Keep the canonical `catalog` branch and its review history independent of application branch cleanup. Retain protected upstream master and its ruleset; use a new PR to resolve divergence. Avoid force pushes, broad resets, and cleanup commands that would erase private configuration or separate repository checkouts.
