# Neurcro catalog

This branch maintains the integration marketplace index and signed built-in catalog
for [Neurcro](https://github.com/Nakashireyumi/neuro-desktop).
Application source is developed separately; this branch contains metadata and validators.

- `marketplace/index.json`: reviewed upstream integration sources, pinned commits,
  prerequisites, licenses and game associations. A listing does not execute code.
- `marketplace/index.schema.json`, `validate.py`, `tests/`: index format and checks.
- `desktop/catalog/`: signed runtime entries, publisher public keys and game profiles.
  Private signing keys, installed state, approvals and connection credentials do not belong here.

Open a pull request targeting `catalog` for listing or metadata changes. Run:

```sh
python3 marketplace/validate.py
python3 -m unittest discover -s marketplace/tests
```

Application releases carry a reviewed index snapshot. Runtime code verifies managed
extension signatures before fetching or starting code; listing in the marketplace alone
does not grant those privileges. Keep publisher key changes under explicit review.

SDK: https://github.com/cassitly/neuro-desktop-sdk
Vencord adapter: https://github.com/cassitly/neuro-desktop-vencord
