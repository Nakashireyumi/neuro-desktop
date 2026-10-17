#!/usr/bin/env python3
"""Check the installed SDK against a real, isolated Neuro Desktop server.

No Neuro backend, game, Discord connection, image, or input executor is needed.
Credentials and runtime state live only in a temporary directory and memory.
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from neuro_desktop_sdk import IntegrationClient, SDKError, ViewerClient, validate_manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server-binary", required=True, type=Path)
    args = parser.parse_args()
    binary = args.server_binary.resolve(strict=True)
    token = secrets.token_urlsafe(32)
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    origin = f"http://127.0.0.1:{port}"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(path, method="GET", body=None, *, approval=False, anonymous=False):
        headers = {"Content-Type": "application/json"}
        if not anonymous:
            headers["X-ND-Token"] = token
        if approval:
            headers["X-ND-Operator-Token"] = token
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(origin + path, data=data, headers=headers, method=method)
        with opener.open(req, timeout=5) as response:
            return json.loads(response.read(1 << 20))

    with tempfile.TemporaryDirectory(prefix="nd-sdk-contract-") as directory:
        work = Path(directory)
        policy = work / "permissions.json"
        policy.write_text(json.dumps({"default_allow": False, "scopes": {"extensions": True}}))
        env = {name: value for name, value in os.environ.items() if not name.startswith("NEURO_")}
        env.update(NEURO_HEADLESS="1", NEURO_ADMIN_TOKEN=token,
                   NEURO_ADMIN_LISTEN=f"127.0.0.1:{port}", NEURO_EXECUTOR_LISTEN="127.0.0.1:0",
                   NEURO_PERMISSIONS_FILE=str(policy), NEURO_IPC_FILE=str(work / "ipc.json"),
                   NEURO_INTEGRATIONS_FILE=str(work / "integrations.json"),
                   NEURO_EXTENSIONS_STATE_FILE=str(work / "extensions-state.json"),
                   NEURO_EXTENSION_PERMISSIONS_FILE=str(work / "extension-permissions.json"),
                   NEURO_AUDIT_LOG=str(work / "audit.jsonl"), NEURO_RELAY_ENABLED="0",
                   NEURO_CONTEXT_POLL_SECONDS="0")
        with (work / "server.log").open("wb") as log:
            process = subprocess.Popen([str(binary), "--ws-url", "ws://127.0.0.1:1"], cwd=work, env=env, stdout=log, stderr=log)
            client = viewer = None
            try:
                deadline = time.monotonic() + 15
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("isolated server exited before becoming ready")
                    try:
                        request("/health", anonymous=True)
                        break
                    except (urllib.error.URLError, OSError):
                        if time.monotonic() >= deadline:
                            raise RuntimeError("isolated server did not become ready") from None
                        time.sleep(0.05)
                integration_id = "sdk-contract"
                base = "/api/integrations/" + integration_id
                manifest = validate_manifest({
                    "schema_version": 1, "id": integration_id, "name": "SDK contract fixture",
                    "version": "0.1.0", "kind": "discord", "game_ids": ["minecraft"],
                    "neuro_api": {"game_name": "Existing private integration"},
                    "permissions": [{"id": "discord.viewer", "description": "Curated fixture activity", "required": False}],
                    "settings_schema": {"type": "object", "additionalProperties": False},
                })
                request(base, "PUT", {"manifest": manifest}, approval=True)
                integration_token = request(base + "/token", "POST", {"kind": "integration"}, approval=True)["token"]
                viewer_token = request(base + "/token", "POST", {"kind": "viewer"}, approval=True)["token"]
                client = IntegrationClient(integration_id, integration_token, server=origin)
                viewer = ViewerClient(integration_id, viewer_token, server=origin)
                try:
                    client.heartbeat()
                except SDKError as error:
                    assert error.status == 403, "unapproved runtime must fail with a scoped denial"
                else:
                    raise AssertionError("unapproved integration was allowed to become ready")
                item_policy = {"manage_allowed": False, "run_allowed": True, "grants": {"discord.viewer": True}, "settings": {}}
                policy_route = "/api/extensions/" + integration_id + "/permissions"
                request(policy_route, "PUT", item_policy, approval=True)
                response = client.heartbeat("ready", "SDK contract fixture")
                assert response["lease_seconds"] == 45
                assert client.configuration()["manifest"] == manifest, "SDK/server manifest round trip differs"
                assert viewer.snapshot()["visible"] is False, "new activity feed must start hidden"
                request(base + "/visibility", "POST", {"actor": "neuro", "visible": True, "minutes": 1}, approval=True)
                client.activity(actor="neuro", kind="status", summary="Public fixture activity")
                feed = viewer.snapshot()
                assert feed["visible"] is True and len(feed["items"]) == 1
                item_policy["grants"]["discord.viewer"] = False
                request(policy_route, "PUT", item_policy, approval=True)
                feed = viewer.snapshot()
                assert feed["visible"] is False and feed["items"] == [], "revocation retained viewer activity"
                client.hide()
                client.heartbeat("offline")
                print("SDK/server contract passed: approval, manifest, lease, curated activity, revocation and shutdown")
            finally:
                for scoped_client in (client, viewer):
                    if scoped_client:
                        scoped_client.close()
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
