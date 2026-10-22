# Neuro Desktop marketplace index

Choose dedicated integrations for your games and applications here. The index
records upstream sources, exact commits, prerequisites, connection settings and
capabilities to review. It contains all eight projects requested for the initial
marketplace. Neuro Desktop's ordinary desktop controls remain useful when no
dedicated integration is available.

These are **source listings**, with upstream installation instructions. They are
not bundled game files, preinstalled extensions, verified working builds, or
automatic installation recipes. Every entry says which setup and tests remain.
An empty platform list means that upstream has not documented a verified matrix;
it does not mean that all operating systems work.

The intended canonical index is the `catalog` branch of
[`Nakashireyumi/neuro-desktop`](https://github.com/Nakashireyumi/neuro-desktop).
The `branch` value in `index.json` describes that intended publishing location;
creating or publishing a branch is a separate Git operation. This directory is
also kept with application source so a release can carry a reviewed snapshot.

Validate changes without fetching or running upstream code:

```sh
python3 marketplace/validate.py
python3 -m unittest discover -s marketplace/tests -v
```

`index.schema.json` documents the version 1 wire shape. `validate.py` adds
checks for duplicate identifiers, credential-free HTTPS links, commit-pinned
instructions and license readiness. It has no third-party Python dependencies.
If `jsonschema` is installed, the tests also check schema compatibility.

Each listing contains:

| Field | Meaning |
| --- | --- |
| `source_ref`, `source_commit` | Human-readable upstream branch and the exact reviewed revision. Fetch by commit, not by a mutable branch name. |
| `license` | Actual repository license status. A README badge alone is insufficient. |
| `platforms.documented`, `.tested` | Upstream documented platforms and platforms actually exercised by Neuro Desktop maintainers, respectively. |
| `game_ids` | Game profile associations; these do not claim an installed game or a connected integration. |
| `neuro_api` | Connection protocol; upstream integrations keep using the official Neuro API. |
| `install` | Manual setup readiness, pinned instructions and prerequisites. Version 1 contains no executable commands. |
| `permissions` | Capabilities inferred during source review. External integrations enforce their own controls; this metadata is not an OS sandbox. |
| `settings` | Upstream configuration names to map when writing an adapter. No credential values belong here. |
| `verification` | Review date, direct source links and remaining verification work. |

`neuratro` has an MIT-derived license with an additional modification condition;
`neurocompanion` has no detected repository license. Both require license review
before redistributing or adapting their source. The other listings' MIT labels
do not cover game assets, mod loaders or their dependencies.

See [the marketplace workflow](../docs/MARKETPLACE.md) for publishing, SDK
adapters, game handoff, permissions and viewer visibility.
