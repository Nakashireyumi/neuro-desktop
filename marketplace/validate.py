#!/usr/bin/env python3
"""Validate the pinned, descriptive marketplace index without network access."""

import argparse
import datetime
import json
import re
from pathlib import Path
from urllib.parse import urlsplit


ID = re.compile(r"[a-z0-9][a-z0-9-]{0,63}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
CAPABILITY = re.compile(r"[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)+\Z")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def text(value, where):
    require(isinstance(value, str) and bool(value.strip()), f"{where}: expected nonempty text")


def fields(value, names, where):
    require(isinstance(value, dict), f"{where}: expected object")
    require(set(value) == set(names.split()), f"{where}: unexpected or missing fields")


def strings(value, where):
    require(isinstance(value, list), f"{where}: expected array")
    for entry in value:
        text(entry, where)
    require(len(value) == len(set(value)), f"{where}: duplicates")


def https(value, where):
    text(value, where)
    try:
        parsed = urlsplit(value)
        valid = (parsed.scheme == "https" and parsed.hostname and parsed.username is None
                 and parsed.password is None and not parsed.query and not parsed.fragment
                 and parsed.port in (None, 443))
    except ValueError:
        valid = False
    require(valid, f"{where}: expected HTTPS URL without credentials, query, or fragment")


def date(value, where):
    text(value, where)
    require(bool(re.fullmatch(r"\d{4}-\d{2}-\d{2}", value)), f"{where}: expected ISO date")
    try:
        datetime.date.fromisoformat(value)
    except ValueError as error:
        raise ValueError(f"{where}: invalid date") from error


def validate(index):
    """Raise ValueError for ambiguous, unpinned, or executable seed metadata.

    Version 1 intentionally has no command, download URL or managed installer.
    Adding that capability requires a versioned runtime/package review first.
    """
    fields(index, "schema_version revision updated_at repository branch items", "index")
    require(type(index["schema_version"]) is int and index["schema_version"] == 1, "unsupported schema_version")
    require(type(index["revision"]) is int and index["revision"] > 0, "revision: expected positive integer")
    date(index["updated_at"], "updated_at")
    https(index["repository"], "repository")
    text(index["branch"], "branch")
    require(isinstance(index["items"], list) and len(index["items"]) <= 1000, "items: expected bounded array")
    seen = set()
    for item in index["items"]:
        fields(item, "id name kind description repository source_ref source_commit license platforms game_ids neuro_api install permissions settings verification", "item")
        item_id = item["id"]
        require(isinstance(item_id, str) and ID.fullmatch(item_id), "item.id: invalid identifier")
        require(item_id not in seen, f"duplicate item: {item_id}")
        seen.add(item_id)
        for key in ("name", "description", "source_ref"):
            text(item[key], f"{item_id}.{key}")
        require(item["kind"] in {"game-integration", "tool-bridge", "editor-extension", "docker-extension"}, f"{item_id}: invalid kind")
        https(item["repository"], f"{item_id}.repository")
        require(isinstance(item["source_commit"], str) and COMMIT.fullmatch(item["source_commit"]), f"{item_id}: source_commit must pin 40 lowercase hexadecimal characters")
        license = item["license"]
        fields(license, "spdx status notes url", f"{item_id}.license")
        for key in ("spdx", "notes"):
            text(license[key], f"{item_id}.license.{key}")
        require(license["status"] in {"standard", "custom", "missing"}, f"{item_id}: invalid license status")
        require(license["status"] != "standard" or license["spdx"] not in {"NOASSERTION", "NONE"}, f"{item_id}: unknown license cannot be standard")
        https(license["url"], f"{item_id}.license.url")
        platforms = item["platforms"]
        fields(platforms, "documented tested notes", f"{item_id}.platforms")
        for key in ("documented", "tested"):
            strings(platforms[key], f"{item_id}.platforms.{key}")
            require(set(platforms[key]) <= {"windows", "macos", "linux"}, f"{item_id}: invalid platform")
        text(platforms["notes"], f"{item_id}.platforms.notes")
        strings(item["game_ids"], f"{item_id}.game_ids")
        require(all(ID.fullmatch(game) for game in item["game_ids"]), f"{item_id}: invalid game id")
        api = item["neuro_api"]
        fields(api, "transport configuration", f"{item_id}.neuro_api")
        require(api["transport"] == "websocket", f"{item_id}: unsupported Neuro API transport")
        text(api["configuration"], f"{item_id}.neuro_api.configuration")
        install = item["install"]
        fields(install, "mode readiness instructions_url requirements notes", f"{item_id}.install")
        require(install["mode"] == "manual", f"{item_id}: schema v1 contains listings, not managed installers")
        require(install["readiness"] in {"license-review", "upstream-setup-required"}, f"{item_id}: invalid install readiness")
        require(license["status"] == "standard" or install["readiness"] == "license-review", f"{item_id}: unresolved license must require review")
        https(install["instructions_url"], f"{item_id}.install.instructions_url")
        require(item["source_commit"] in urlsplit(install["instructions_url"]).path.split("/"), f"{item_id}: instructions must refer to the pinned source commit")
        for key in ("requirements", "notes"):
            strings(install[key], f"{item_id}.install.{key}")
        permissions = item["permissions"]
        fields(permissions, "review_status enforcement capabilities", f"{item_id}.permissions")
        require(permissions["review_status"] == "inferred-from-source", f"{item_id}: seed capability review must identify its origin")
        require(permissions["enforcement"] == "upstream", f"{item_id}: a listing cannot claim to sandbox an external process")
        require(isinstance(permissions["capabilities"], list), f"{item_id}: invalid capabilities")
        capability_ids = set()
        for capability in permissions["capabilities"]:
            fields(capability, "id reason", f"{item_id}.capability")
            require(isinstance(capability["id"], str) and CAPABILITY.fullmatch(capability["id"]), f"{item_id}: invalid capability id")
            require(capability["id"] not in capability_ids, f"{item_id}: duplicate capability")
            capability_ids.add(capability["id"])
            text(capability["reason"], f"{item_id}.capability.reason")
        require(isinstance(item["settings"], list), f"{item_id}: invalid settings")
        setting_keys = set()
        for setting in item["settings"]:
            fields(setting, "key upstream_key type description", f"{item_id}.setting")
            for key in ("key", "upstream_key", "description"):
                text(setting[key], f"{item_id}.setting.{key}")
            require(setting["key"] not in setting_keys, f"{item_id}: duplicate setting")
            setting_keys.add(setting["key"])
            require(setting["type"] in {"string", "boolean", "integer", "number"}, f"{item_id}: invalid setting type")
        verification = item["verification"]
        fields(verification, "reviewed_at source_links unverified", f"{item_id}.verification")
        date(verification["reviewed_at"], f"{item_id}.verification.reviewed_at")
        strings(verification["source_links"], f"{item_id}.verification.source_links")
        require(bool(verification["source_links"]), f"{item_id}: review needs source links")
        for link in verification["source_links"]:
            https(link, f"{item_id}.verification.source_links")
        strings(verification["unverified"], f"{item_id}.verification.unverified")
    return len(seen)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("index", nargs="?", type=Path, default=Path(__file__).with_name("index.json"))
    args = parser.parse_args()
    try:
        count = validate(json.loads(args.index.read_text(encoding="utf-8")))
    except (ValueError, OSError, TypeError) as error:
        parser.exit(1, f"Marketplace validation failed: {error}\n")
    print(f"Validated {count} pinned marketplace listings")


if __name__ == "__main__":
    main()
