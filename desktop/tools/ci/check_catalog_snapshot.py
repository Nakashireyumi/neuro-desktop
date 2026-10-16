#!/usr/bin/env python3
"""Verify shipped catalog data against its separately maintained pinned branch."""

import json
from pathlib import Path
import subprocess
import sys


def main():
    root = Path(__file__).resolve().parents[3]
    source = Path(sys.argv[1]).resolve()
    snapshot = json.loads((root / "desktop/catalog/snapshot.json").read_text())
    revision = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip()
    if revision != snapshot["commit"]:
        raise SystemExit("Canonical catalog checkout does not match the pinned snapshot commit")
    for local_name, canonical_name in snapshot["files"].items():
        local = root / "desktop/catalog" / local_name
        canonical = source / canonical_name
        if local.read_bytes() != canonical.read_bytes():
            raise SystemExit(f"Catalog snapshot differs: {local_name}; update the canonical branch and pin together")
    print(f"Verified {len(snapshot['files'])} catalog files at {revision}")


if __name__ == "__main__":
    main()
