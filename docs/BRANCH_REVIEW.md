# Branch review

## Consolidation completed on 2026-10-18

The original inventory below is retained as the audit trail. The reviewed
application tree at `6efb7de` passed the complete [push CI and release gate](https://github.com/cassitly/neuro-desktop/actions/runs/39118669921), including native GTK controls,
standalone manager startup/packaging and the installed SDK/server contract on
Linux, Windows and macOS. Real desktop input/capture acceptance on Windows/macOS
remains separate from those tests.

[Fork #6](https://github.com/cassitly/neuro-desktop/pull/6) merged as `572daaf`;
[upstream #28](https://github.com/Nakashireyumi/neuro-desktop/pull/28) merged as
`ddae583`. Both merge trees match the tested application tree. Fork `master`,
`dev`, `arena/456c497d-neuro-desktop`, `codex/production-readiness-batch` and
`temp-upload` were fast-forwarded to `ddae583` after fresh ancestry checks proved
they had no unique commits. The independent `catalog` branch remains at
`7d4504c9ba063ffd41df3fe8bca7928bfb353e48` in both repositories.

[Release PR #29](https://github.com/Nakashireyumi/neuro-desktop/pull/29) targets
**Nakashireyumi/neuro-desktop:master** from upstream `dev/latest`. It remains a
draft while subsequent requested work is completed. Protected upstream master
is still `aaf860d`; its ruleset is unchanged. Master history was reconciled with
ordinary merge `2d67dc1` and carried into development by `19ea8fe`, preserving
the original GPL license and attribution.

Obsolete PRs #19 and #24 were closed. The seven obsolete upstream heads in the
table below were preserved as tags named `archive/2026-10-18/<original-branch>`,
then removed. Deletion checked both the current head and exact archive tag and
used atomic expected-head leases, so a concurrently changed branch would stop
the cleanup. Upstream now retains `master`, `dev/latest` and `catalog`.

To recover an archived branch, fetch its tag and create a new topic branch from
it; for example:

```sh
git fetch https://github.com/Nakashireyumi/neuro-desktop.git \
  refs/tags/archive/2026-10-18/implementations/neuro-desktop-ui
git switch -c codex/review-historical-ui FETCH_HEAD
```

New development can advance maintained branches beyond this dated snapshot.
Refresh remote refs before further synchronization or cleanup.

## Original inventory

Reviewed on 2026-10-15 using local Git history and authenticated, read-only GitHub REST requests. This review changed no Git refs, pull requests, repository settings, or remote branches. The remote inventory below is a snapshot; refresh it before publishing or deleting branches.

## Repositories and working history

The upstream repository is [`Nakashireyumi/neuro-desktop`](https://github.com/Nakashireyumi/neuro-desktop). Its default branch is `master`. The working checkout's sole configured remote, `origin`, points to [`cassitly/neuro-desktop`](https://github.com/cassitly/neuro-desktop), a fork whose default branch is also `master`. The authenticated account has administrative access to both repositories.

The local working branch was `arena/456c497d-neuro-desktop` at `25f0a8557dfaa389ddb0095b12c6128d4193ad7e`. Its remote branch was one commit ahead at `13327aadc7ed83a3fe2853b2a824efbc02954b85`. The uncommitted work in this checkout was excluded from commit comparisons.

`13327aa` adds the first Go executor client in `desktop/apps/neuro-client-go`, with executor authentication, reconnects, status, shell policy, shutdown, tests, and cross-platform build checks. The commit explicitly keeps the Python client as the shipping desktop executor: mouse, keyboard, screenshots, window inspection, scripts, and queueing are not implemented in this Go slice. Preserve this commit when updating the working branch. Review and resolve overlapping changes to build scripts, CI, and documentation before pushing.

## Existing pull requests

| Pull request | Head | Base | State at review |
| --- | --- | --- | --- |
| [Fork #6](https://github.com/cassitly/neuro-desktop/pull/6) | `cassitly:arena/456c497d-neuro-desktop` | `cassitly:dev` | Open draft; mergeable; head `13327aa` |
| [Upstream #28](https://github.com/Nakashireyumi/neuro-desktop/pull/28) | `cassitly:dev` | `Nakashireyumi:dev/latest` | Open; mergeable; head `145157f` |
| [Upstream #24](https://github.com/Nakashireyumi/neuro-desktop/pull/24) | `Nakashireyumi:coderabbitai/utg/b1ea7f9` | `Nakashireyumi:master` | Open; generated tests for the previous architecture |
| [Upstream #19](https://github.com/Nakashireyumi/neuro-desktop/pull/19) | `Nakashireyumi:coderabbitai/chat/b1ea7f9` | `Nakashireyumi:dev/latest` | Open; changes to the previous setup and integration client |

The 28 reported check runs on `13327aa` had succeeded: two workflow runs, each including its release gate, Linux/Windows/macOS build-and-test jobs, the bundle check, Python client build, executor end-to-end checks, and catalog/MCP/vision/relay checks. Those results verify that commit, not later uncommitted changes or a native manager added afterward.

Both #6 and #28 have obsolete descriptions. In particular, #6 records an earlier decision to decline a native tray and says the dashboard was not visually tested. #28 describes the removed Rust/C++ architecture. Rewrite their titles and descriptions around the final implementation and its actual validation. There were no submitted reviews returned for these two PRs at review time.

## Fork branches

All five remote branch heads matched the locally cached `origin/*` refs. Counts below compare each branch with local `25f0a85`.

| Branch | Head | Relationship |
| --- | --- | --- |
| `arena/456c497d-neuro-desktop` | `13327aadc7ed83a3fe2853b2a824efbc02954b85` | One additional Go-client commit |
| `dev` | `145157ffcf4e7813d66ac9275c4f853a0d0e6e2e` | Included in working history; 22 commits behind |
| `master` | `98e8683a80730d2c5e899c035737988631d0b8d2` | Included in working history; 155 commits behind |
| `codex/production-readiness-batch` | `51a34cef3f443c62767c872de3f72e3ca275ff01` | Included; 39 commits behind; fork PR #5 merged |
| `temp-upload` | `8bc6c68e53b02a08db459dedbd95841c47f7171d` | Included; 98 commits behind; fork PR #2 merged |

The two merged topic branches have no unique commits to recover. The development and default branches can be updated through normal merge or fast-forward operations after the reviewed changes pass CI. Updating branches in unrelated forks or rewriting their histories is outside this inventory.

## Upstream branches

“Unique commits” means commits reachable from the branch but not from local `25f0a85`; it does not necessarily imply a distinct final file tree.

| Branch | Head | Unique commits and review |
| --- | --- | --- |
| `master` | `aaf860da621975cfd83b98fea6a5e6fe9729b84d` | Three unique merge/revert commits; see below |
| `dev/latest` | `37f9efc8ae655bbc702c7b48c9e5a0246d366511` | One unique merge commit, #23; no unique file changes from its merge base |
| `issues/termination-upon-launch` | `60108ec9858faa614fddbdb57609b6517f41ea2d` | Already included in working history |
| `revert-26-temp-upload` | `3a4489fbb9560aa76d23ca8239a0b7e1a18acba3` | Two unique commits, already merged into upstream `master` through #27 |
| `fix/ui-token-synchronization-issues` | `4cc052ae55395473b18cacecc615f516aa326c77` | Same head and 19 unique commits as the UI implementation branch |
| `implementations/neuro-desktop-ui` | `4cc052ae55395473b18cacecc615f516aa326c77` | Previous Flask dashboard, plugin management, control settings, and tests |
| `implementations/bundled-setup-file` | `048645440b29ef425fc697634b485c880d59cbf0` | Two unique commits: Windows API submodule update and Node setup package |
| `coderabbitai/chat/b1ea7f9` | `d87757762c630036e4ae5bfa2da22c4e73b1aa3a` | One unique commit; open PR #19 |
| `coderabbitai/utg/b1ea7f9` | `da22a53a20a768d10958edca2264223cc506fab1` | One unique generated-test commit; open PR #24 |

### Upstream master and protection

Upstream `master` and the local working history diverge from `d12eec5e75cc7bd65ed505dd6bc5db936de370a0`. The working branch has 99 unique commits; upstream has these three:

- `0d51b07f348551360718f530c564c396e3d9fa51`: merge PR #26, temporarily transferring fork files.
- `3a4489fbb9560aa76d23ca8239a0b7e1a18acba3`: revert that transfer.
- `aaf860da621975cfd83b98fea6a5e6fe9729b84d`: merge the revert through PR #27.

This explains why upstream's current tree still uses the earlier Python layout. A PR to upstream `master` must reconcile that history and the replacement architecture explicitly.

The active repository ruleset **9661234, “Protecty”**, covers the default branch and `refs/heads/dev`. It contains deletion, non-fast-forward, creation, and update restrictions; code-quality rules; and Copilot review on pushes and draft PRs. Organization administrators have an always-available bypass. The classic branch-protection endpoint returns 404 because protection is provided by this ruleset. Preserve the ruleset and use the intended PR/review path.

### Work worth carrying forward

The two branches at `4cc052a` introduce a plugin manifest with identity, version, entry point, dependencies, declared permissions, enable state, and a Neuro-integration flag. They also introduce plugin management routes and operator control settings. These are useful product requirements for the marketplace and SDK.

Their implementation needs replacement rather than a direct merge: declared permissions are not enforced, plugins default to enabled, entry points are imported into the manager process, and paths and runtime privileges are not isolated. The old tests target Flask routes and removed module paths; several skip when routes are absent. Translate useful scenarios into tests for the current API.

The generated-test branch covers coordinate boundaries, screenshot input, action schemas, session errors, context publishing, and optional dependency failures. These scenarios remain useful, but the tests refer to the removed architecture and rely heavily on mocks. Port only tests that exercise current behavior and meaningful failure modes.

The chat branch suppresses the missing-Python setup failure and comments out regionalization initialization while later code still refers to it. It should be superseded by actionable dependency reporting and current startup tests.

The bundled-setup branch predates the current bundles and native manager. Its goal of a convenient platform installer is relevant; its old Node setup and Windows-specific submodule assumptions should not be restored into the current runtime.

## Licensing and publication route

Upstream publishes a root GNU GPL version 3 `LICENSE`. The reviewed fork checkout had no root `LICENSE`, and GitHub detected no license on the fork. Restore the upstream license and preserve relevant attribution when consolidating its history and publishing derivative components. Review separately licensed integrations before redistributing their code or binary assets.

Recommended publication sequence:

1. Preserve remote `13327aa` and reconcile its build, CI, and documentation changes with the current work.
2. Run the required checks on the final combined tree, including platform jobs for native artifacts.
3. Update the existing fork PR #6, merge the reviewed feature into fork `dev`, and refresh upstream PR #28 rather than creating duplicate development PRs.
4. Create a separate PR whose base repository is **`Nakashireyumi/neuro-desktop`** and base branch is **`master`**. Resolve the revert history deliberately and include the final workflow and deployment guide.
5. Update the fork's maintained branches through normal merges or fast-forwards after verification. Archive useful unique history before deleting superseded branches. Resolve or supersede PRs #19 and #24 before removing their heads.
6. Refresh this inventory immediately before cleanup. Keep the marketplace index branch separate from application development and retain its publication/signature history.

This sequence is a recommendation. At the time of this review, no new PR had been created, no branch had been merged or deleted, and no GitHub repository settings had been changed.
