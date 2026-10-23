#!/usr/bin/env python3
"""Fake Neuro Desktop executor — a protocol simulator for the dashboard.

It speaks the same protocol as the real executor (Rust + pyo3 + pyautogui) so
the Go bridge, the dashboard and the game interface can be exercised on a
machine that has no display, no Windows and no Python GUI stack:

    client -> {"type": "hello", "role": "executor", "version": "2"}
    server -> {"type": "hello_ack", ...}
    server -> {"type": "command", "id": "...", "command": {...}}
    client -> {"type": "result", "id": "...", "success": true, "data": {...}}

It NEVER touches the real mouse or keyboard: every input command is logged and
answered with success. That makes it safe to run while you are working, and it
is the fastest way to see the Games tab light up (the reported active window and
process list come from the environment variables below).

Usage:
    python3 fake_executor.py --addr 127.0.0.1:9876
    NEURO_FAKE_WINDOW="Minecraft" python3 fake_executor.py

Options:
    --addr ADDR     executor hub address (default 127.0.0.1:9876)
    --token TOKEN   shared secret, when the bridge requires one
    --once          exit after the first disconnect instead of reconnecting
    --quiet         only log commands, not every envelope
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import sys
import time

PROTOCOL_VERSION = "2"


QUIET = False


def log(message: str) -> None:
    if QUIET:
        return
    print(f"[fake-executor] {message}", flush=True)


def read_line(reader) -> str | None:
    line = reader.readline()
    if not line:
        return None
    return line.decode("utf-8", "replace").strip()


def send(writer, payload: dict) -> None:
    writer.write((json.dumps(payload) + "\n").encode("utf-8"))
    writer.flush()


def status_payload() -> dict:
    """A believable get_status answer, configurable through the environment."""
    window = os.environ.get("NEURO_FAKE_WINDOW", "Neuro Desktop Dashboard")
    processes = os.environ.get("NEURO_FAKE_PROCESSES", "python.exe,javaw.exe,explorer.exe")

    return {
        "status": "running",
        "timestamp": int(time.time()),
        "active_window": window,
        "open_windows": [window],
        "screen": {"width": 1920, "height": 1080},
        "mouse_position": {"x": 960, "y": 540},
        "running_processes": [name.strip() for name in processes.split(",") if name.strip()],
        "recent_actions": [],
        "screenshot_png_b64": None,
    }


def handle_command(command: dict, quiet: bool) -> tuple[dict, bool]:
    """Returns (response payload, shutdown_requested)."""
    kind = command.get("type", "")
    params = command.get("params") or {}

    if kind == "get_status":
        return {"data": status_payload()}, False

    if kind in ("shutdown_gracefully", "shutdown_immediately"):
        log("bridge asked the executor to shut down")
        return {"data": {"shutdown": True}}, True

    if kind in ("heartbeat", "ping"):
        return {"data": {"heartbeat": "alive"}}, False

    # Input commands are always logged (that is the point of the simulator),
    # even in --quiet mode.
    always_log = (
        "key_hold_for",
        "key_combo",
        "key_release_all",
        "mouse_hold_for",
        "move_mouse_relative",
        "run_script",
    )
    if not quiet or kind in always_log:
        detail = " ".join(f"{key}={value}" for key, value in sorted(params.items()))
        log(f"{kind} {detail}".rstrip())

    return {}, False


def run_session(addr: str, token: str | None, quiet: bool) -> bool:
    """Returns True when the bridge requested shutdown."""
    host, _, port = addr.rpartition(":")
    conn = socket.create_connection((host or "127.0.0.1", int(port)), timeout=10)
    conn.settimeout(120)

    reader = conn.makefile("rb")
    writer = conn.makefile("wb")

    hello = {"type": "hello", "role": "executor", "version": PROTOCOL_VERSION}
    if token:
        hello["token"] = token
    send(writer, hello)

    reply = read_line(reader)
    if reply is None:
        raise ConnectionError("bridge closed the connection during the handshake")

    envelope = json.loads(reply)
    if envelope.get("type") == "hello_nack":
        raise PermissionError(f"bridge rejected the executor: {envelope.get('error', 'no reason given')}")
    if envelope.get("type") != "hello_ack":
        raise ValueError(f"expected hello_ack, got {envelope.get('type')}")

    log(f"connected to {addr} (protocol {envelope.get('version', 'unknown')})")

    while True:
        line = read_line(reader)
        if line is None:
            return False
        if not line:
            continue

        try:
            message = json.loads(line)
        except json.JSONDecodeError:
            log(f"ignoring malformed envelope: {line[:120]}")
            continue

        kind = message.get("type")
        if kind == "ping":
            send(writer, {"type": "pong", "id": message.get("id")})
            continue
        if kind != "command":
            continue

        response, shutdown = handle_command(message.get("command") or {}, quiet)
        send(
            writer,
            {
                "type": "result",
                "id": message.get("id", ""),
                "success": True,
                "data": response.get("data"),
                "error": None,
            },
        )
        if shutdown:
            return True


def main() -> int:
    parser = argparse.ArgumentParser(description="Fake Neuro Desktop executor (protocol simulator)")
    parser.add_argument("--addr", default=os.environ.get("NEURO_EXECUTOR_ADDR", "127.0.0.1:9876"))
    parser.add_argument("--token", default=os.environ.get("NEURO_EXECUTOR_TOKEN", ""))
    parser.add_argument("--once", action="store_true", help="do not reconnect after a disconnect")
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args()

    global QUIET
    QUIET = args.quiet

    log("simulator only: no mouse or keyboard input is ever generated")
    log(f"active window reported as {os.environ.get('NEURO_FAKE_WINDOW', 'Neuro Desktop Dashboard')!r}")

    attempt = 0
    while True:
        started = time.monotonic()
        try:
            if run_session(args.addr, args.token or None, args.quiet):
                log("shutdown complete")
                return 0
            log("bridge closed the connection")
        except (ConnectionError, OSError) as error:
            attempt += 1
            if args.once:
                log(f"giving up: {error}")
                return 1
            delay = min(2 ** min(attempt, 5), 30)
            log(f"could not reach {args.addr} ({error}); retrying in {delay}s")
            time.sleep(delay)
            continue
        except (ValueError, PermissionError) as error:
            log(f"not retrying: {error}")
            return 1

        if args.once:
            return 0

        # A session that ends immediately means we were replaced: the bridge
        # keeps only one executor and drops the older one. Reconnecting at once
        # turns that into a hot loop between the two, so back off like a failed
        # connection instead of spinning.
        lived = time.monotonic() - started
        if lived < 10:
            attempt += 1
            delay = min(2 ** min(attempt, 5), 30)
            log(
                f"was only connected for {lived:.1f}s (another executor may be running); "
                f"retrying in {delay}s"
            )
            time.sleep(delay)
            continue

        attempt = 0
        time.sleep(2)


if __name__ == "__main__":
    sys.exit(main())
