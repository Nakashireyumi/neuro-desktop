"""Neuro Desktop agent — the process that runs on the machine Neuro controls.

This is the "server side" of the pair: the Go bridge (``neuro-integration``)
holds the Neuro connection, the permission policy, the game interface and the
dashboard, and *this* process is what actually touches the desktop. It replaces
the old Rust executor, which meant a second toolchain, a second IPC
implementation and a second place for the two to disagree.

Protocol (newline-delimited JSON — the same one ``executor_hub.go`` speaks):

    agent -> {"type": "hello", "role": "executor", "version": "2", "token": "..."}
    bridge -> {"type": "hello_ack", "version": "2"} | {"type": "hello_nack", "error": "..."}
    bridge -> {"type": "command", "id": "...", "command": {"type": "...", "params": {...}}}
    agent -> {"type": "result", "id": "...", "success": true, "data": {...}}
    bridge -> {"type": "ping", "id": "..."} ; agent -> {"type": "pong", "id": "..."}

Ways to run it:

    # connect out to the bridge's executor hub (works across machines, and the
    # agent needs no inbound port; the hub checks the token)
    python3 -m controller.agent --bridge 127.0.0.1:9876 --token "$NEURO_EXECUTOR_TOKEN"

    # or pick up commands from the bridge's file-IPC fallback (no TCP at all)
    python3 -m controller.agent --ipc-file "$NEURO_IPC_FILE"

Everything it can do is bounded by the Python controller's own safety rules
(shell allowlist and patterns in ``shell.py``, ``NoDisplayError`` on headless
machines), so a compromised bridge still cannot make it do more than the policy
on this machine allows.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import shutil
import socket
import stat
import sys
import tempfile
import time
from typing import Any, Dict, Optional

from . import capabilities
from . import desktop as desktop_module
from . import shell as shell_module
from .actions import MAX_PENDING_ACTIONS, MAX_QUEUE_SECONDS, instruction_seconds
from . import gui_stub
from .gui_stub import describe_headless_reason, is_headless
from .lib import initialize_driver
from .controls import keyboard as keyboard_driver

PROTOCOL_VERSION = "2"
# One JSON line per message. A screenshot travels inside one line as base64 PNG,
# scaled down by desktop.capture_screen_png to a few MB at most.
MAX_LINE_BYTES = 8 * 1024 * 1024

# Commands this agent executes. Anything else in the bridge's command list is
# handled inside the bridge (desktop shell intents, catalog, extensions, games).
# `desktop/python/tests/test_agent.py` checks this against the Go list in
# `headless/apps/neuro-integration/executor_commands.go`, so a new command cannot
# be added on one side only. The agent additionally answers `heartbeat`, which
# the Rust executor used to receive but the Go bridge never sends (kept so an
# older bridge still works).
EXECUTOR_COMMANDS = (
    "move_mouse_to",
    "move_mouse_relative",
    "mouse_click",
    "mouse_hold_for",
    "type_text",
    "key_press",
    "key_combo",
    "key_hold_for",
    "key_release_all",
    "run_script",
    "shell_command",
    "get_status",
    "execute_queue",
    "clear_action_queue",
    "shutdown_gracefully",
    "shutdown_immediately",
)

# Input commands that the bridge can batch: `execute_now` runs the queue,
# `clear_after` empties it (the same contract the old Rust executor used).
QUEUE_COMMANDS = (
    "move_mouse_to",
    "move_mouse_relative",
    "mouse_click",
    "mouse_hold_for",
    "type_text",
    "key_press",
    "key_combo",
    "key_hold_for",
    "run_script",
)

# Script verbs that can run even when desktop input is refused. Everything
# else (including platform intents) is input. A mixed script must not be
# blocked merely because it also contains such a verb.
SCRIPT_NON_INPUT_VERBS = frozenset({"SHELL", "LAUNCH", "WAIT"})


def _instruction_touches_input(instruction: Any) -> bool:
    marker = getattr(instruction, "touches_input", None)
    if marker is not None:
        return bool(marker)
    tokens = getattr(instruction, "tokens", None)
    if tokens:
        return str(tokens[0]).upper() not in SCRIPT_NON_INPUT_VERBS
    return False


def _timestamp() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def _numeric(params: Dict[str, Any], key: str, default: float) -> float:
    value = params.get(key, default)
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value.strip())
        except ValueError:
            return default
    return default


def _int_param(params: Dict[str, Any], key: str, default: int) -> int:
    return int(_numeric(params, key, default))


def _bool_param(command: Dict[str, Any], key: str, default: bool) -> bool:
    """Read a flag from the envelope, then from `params` (the bridge sets both)."""
    for source in (command, command.get("params") or {}):
        if isinstance(source, dict) and key in source:
            value = source[key]
            if isinstance(value, bool):
                return value
            if isinstance(value, str):
                return value.strip().lower() in ("1", "true", "yes", "on")
            if isinstance(value, (int, float)):
                return bool(value)
    return default


class Agent:
    """Executes bridge commands against the local controller."""

    def __init__(self) -> None:
        self.monitor, self.mouse, self.keyboard, self.parser = initialize_driver()
        self.headless = is_headless()
        self._ordered_queue = []
        self._closed = False
        self._file_execution_session = None

    # ------------------------------------------------------------------
    # Status
    # ------------------------------------------------------------------

    def _safe(self, call, default=None):
        try:
            return call()
        except Exception:
            return default

    def status(self, params: Dict[str, Any]) -> Dict[str, Any]:
        max_windows = _int_param(params, "max_open_windows", 20)
        max_processes = _int_param(params, "max_processes", 40)
        max_actions = _int_param(params, "max_actions", 20)

        processes = self._safe(self.monitor.get_running_processes, []) or []
        actions = self._safe(self.monitor.get_action_history, []) or []

        backend = keyboard_driver.pyautogui
        input_state = capabilities.input_capability(
            headless=self.headless, backend=backend
        )
        input_available = bool(input_state["available"])
        input_error = input_state.get("reason")

        window_state = capabilities.window_capability(
            headless=self.headless, backend_available=desktop_module.gw is not None
        )
        windows: list = []
        windows_error = window_state.get("reason")
        if window_state["available"]:
            try:
                windows = self.monitor.get_open_windows() or []
                windows_error = None
            except Exception as error:
                window_state = dict(window_state)
                window_state.update(
                    {
                        "available": False,
                        "reason": str(error),
                    }
                )
                windows_error = str(error)

        capture_state = capabilities.capture_capability(
            headless=self.headless,
            mss_available=desktop_module.mss is not None,
            pillow_available=desktop_module.Image is not None,
            grim_path=shutil.which("grim"),
        )
        hint = self._safe(self.monitor.capture_geometry_hint)
        capture_state["geometry"] = hint

        # Stub geometry is only for queue maths; never report it as a real screen.
        screen = self._safe(self.monitor.get_screen_size) if input_available else None
        position = self._safe(self.monitor.get_current_mouse_position) if input_available else None
        screen_info = None
        if screen:
            screen_info = {
                "width": screen[0],
                "height": screen[1],
                "pointer_bounds": {
                    "left": 0,
                    "top": 0,
                    "right": screen[0] - 1,
                    "bottom": screen[1] - 1,
                },
                "capture_geometry": hint,
            }

        # The screenshot is returned as bytes. The server may be on another PC, and
        # it must never be asked to open a file that this machine names.
        screenshot_png_b64 = None
        screenshot_geometry = None
        screenshot_error = describe_headless_reason() if params.get("capture_screenshot") and self.headless else None
        if params.get("capture_screenshot") and not self.headless:
            if hasattr(self.monitor, "_last_capture_geometry"):
                self.monitor._last_capture_geometry = None
            try:
                png = self.monitor.capture_screen_png()
                if png:
                    screenshot_png_b64 = base64.b64encode(png).decode("ascii")
                    screenshot_geometry = getattr(self.monitor, "last_capture_geometry", None)
            except Exception as error:
                screenshot_error = str(error)
        if screenshot_geometry:
            capture_state["geometry"] = screenshot_geometry
            if screen_info is not None:
                screen_info["capture_geometry"] = screenshot_geometry

        return {
            "status": "running",
            "timestamp": _timestamp(),
            # On a headless machine there is no window, cursor or screenshot;
            # saying so is what lets the bridge (and Neuro) pick shell_command
            # instead of guessing.
            "headless": self.headless,
            # Legacy flag retained for older bridges. Prefer capabilities.input.
            "input_available": input_available,
            "input_error": input_error,
            "capabilities": {
                "input": input_state,
                "capture": capture_state,
                "windows": window_state,
            },
            "platform": self._safe(self.monitor.get_platform, sys.platform),
            "active_window": self._safe(self.monitor.get_active_window) if window_state["available"] else None,
            "open_windows": sorted(dict.fromkeys(windows))[:max_windows],
            "open_windows_error": windows_error,
            "running_processes": sorted(dict.fromkeys(processes))[:max_processes],
            "recent_actions": actions[-max_actions:] if actions else [],
            "screen": screen_info,
            "mouse_position": {"x": position[0], "y": position[1]} if position else None,
            "screenshot_png_b64": screenshot_png_b64,
            "screenshot_geometry": screenshot_geometry,
            "screenshot_error": screenshot_error,
        }

    # ------------------------------------------------------------------
    # Commands
    # ------------------------------------------------------------------

    def execute(self, command: Dict[str, Any]) -> Dict[str, Any]:
        """Run one IPC command; returns {"success", "data", "error"}."""
        if self._closed:
            return self._failure("executor session is closed; reconnect before sending commands")
        if not isinstance(command, dict):
            return self._failure("command must be an object")
        name = str(command.get("type") or "").strip()
        params = command.get("params") or {}
        if not isinstance(params, dict):
            return self._failure("params must be an object")

        queue_starts = (len(self.keyboard.queue), len(self.mouse.instruction_queue), len(self.parser.queue))
        self.parser.last_shell_output = ""
        try:
            result = self._dispatch(name, params)
            # Queued input is flushed here, inside the guard: on a headless
            # machine draining a queue is exactly where NoDisplayError comes
            # from, and the caller must see it as a failed action.
            if result.get("success") and name in QUEUE_COMMANDS:
                queues = (self.keyboard.queue, self.mouse.instruction_queue, self.parser.queue)
                added = [instruction for queue, start in zip(queues, queue_starts) for instruction in queue[start:]]
                pending = self._ordered_queue + added
                error = None
                if len(pending) > MAX_PENDING_ACTIONS:
                    error = "At most 512 pending actions are allowed; execute and clear the queue"
                elif sum(instruction_seconds(instruction) for instruction in pending) > MAX_QUEUE_SECONDS + 1e-9:
                    error = "Pending actions exceed the 20-second delay budget; execute and clear a shorter queue"
                if error:
                    for queue, start in zip(queues, queue_starts):
                        del queue[start:]
                    return self._failure(error)
                self._ordered_queue.extend(added)
                if _bool_param(command, "execute_now", True):
                    self._drain_queues()
                if _bool_param(command, "clear_after", True):
                    self._clear_queues()
                output = (getattr(self.parser, "last_shell_output", "") or "").strip()
                if output and not result.get("data"):
                    self.parser.last_shell_output = ""
                    result["data"] = {"output": output}
        except shell_module.ShellDeniedError as exc:  # firewall refusal
            return self._failure(str(exc))
        except shell_module.ShellTimeoutError as exc:
            return self._failure(str(exc))
        except gui_stub.NoDisplayError as exc:
            return self._failure(self._input_hint(str(exc)))
        except Exception as exc:  # noqa: BLE001 - reported to the bridge
            return self._failure(self._input_hint(f"{type(exc).__name__}: {exc}"))

        return result

    def _input_hint(self, message: str) -> str:
        """Add the "what to do instead" sentence small models need.

        Only for messages that mean "the graphical session is not there" —
        appending it to a permission refusal or a script syntax error would send
        the model down the wrong path.
        """
        lowered = message.lower()
        if "shell_command" in lowered:
            return message
        if not any(word in lowered for word in ("display", "headless", "screen", "x server", "xserver")):
            return message
        return f"{message} (use `shell_command` on a machine with no display)"

    def _clear_queues(self) -> None:
        self._ordered_queue.clear()
        self.mouse.instruction_queue.clear()
        self.keyboard.clear()
        self.parser.clear()

    def _dispatch(self, name: str, params: Dict[str, Any]) -> Dict[str, Any]:
        if name == "move_mouse_to":
            self.mouse.queue_move(
                _int_param(params, "x", 0),
                _int_param(params, "y", 0),
                _numeric(params, "duration", 0.1),
            )
            return self._ok()

        if name == "mouse_click":
            self.mouse.queue_click(str(params.get("button") or "left"))
            return self._ok()

        if name == "mouse_hold_for":
            self.mouse.queue_hold(
                str(params.get("button") or "left"),
                _numeric(params, "seconds", 0.2),
            )
            return self._ok()

        if name == "move_mouse_relative":
            self.mouse.queue_move_rel(
                _int_param(params, "dx", 0),
                _int_param(params, "dy", 0),
                _numeric(params, "duration", 0.0),
            )
            return self._ok()

        if name == "type_text":
            text = params.get("text")
            if not isinstance(text, str) or not text:
                return self._failure("type_text needs a non-empty `text` string")
            self.keyboard.type(text)
            return self._ok()

        if name == "key_press":
            key = params.get("key")
            if not isinstance(key, str) or not key:
                return self._failure("key_press needs a `key` string")
            self.keyboard.press(key)
            return self._ok()

        if name == "key_combo":
            keys = params.get("keys")
            if not isinstance(keys, list) or not keys:
                return self._failure("key_combo needs a non-empty `keys` list")
            self.keyboard.combo(*[str(key) for key in keys])
            return self._ok()

        if name == "key_hold_for":
            key = params.get("key")
            if not isinstance(key, str) or not key:
                return self._failure("key_hold_for needs a `key` string")
            self.keyboard.hold_for(key, _numeric(params, "seconds", 0.2))
            return self._ok()

        if name == "key_release_all":
            self.release_all_input()
            return self._ok()

        if name == "run_script":
            script = params.get("script")
            if not isinstance(script, str) or not script.strip():
                return self._failure("run_script needs a `script` string")
            self.parser.last_shell_output = ""
            self.parser.shell_policy = ({"allowlist": params.get("allowlist"), "denylist": params.get("denylist") or []}
                                        if "allowlist" in params else None)
            self.parser.shell_cwd = params.get("cwd") or None
            self.parser.parse(script)
            return self._ok()

        if name == "shell_command":
            command_line = params.get("command")
            if not isinstance(command_line, str) or not command_line.strip():
                return self._failure("shell_command needs a `command` string")
            timeout = params.get("timeout")
            # The server attaches its effective shell policy to every command.
            policy = None
            if "allowlist" in params:
                policy = {
                    "allowlist": params.get("allowlist"),
                    "denylist": params.get("denylist") or [],
                }
            output = shell_module.run(
                command_line,
                cwd=params.get("cwd") or None,
                timeout=float(timeout) if timeout else None,
                policy=policy,
            )
            # The transcript is the whole point of this command, so it goes
            # back inside `data`.
            return self._ok({"output": output, "command": command_line})

        if name == "get_status":
            return self._ok(self.status(params))

        if name == "heartbeat":
            return self._ok({"heartbeat": "alive", "timestamp": _timestamp()})

        if name == "execute_queue":
            self.parser.last_shell_output = ""
            self._drain_queues()
            output = (self.parser.last_shell_output or "").strip()
            return self._ok({"output": output}) if output else self._ok()

        if name == "clear_action_queue":
            self._clear_queues()
            return self._ok()

        if name in ("shutdown_gracefully", "shutdown_immediately"):
            self.close()
            return self._ok({"shutdown": True})

        # Commands that the bridge handles itself (game_*, catalog, extensions).
        # Reaching them here means a bridge/agent version mismatch: say so
        # instead of failing silently.
        return self._failure(
            f"{name!r} is not an executor command; the bridge handles it. "
            "Check that the bridge and agent are the same version."
        )

    def _drain_queues(self) -> None:
        """Run pending input and scripts in wire/source order.

        Queues are retained when clear_after=False, matching the wire contract.
        A script's shell, launch and release operations occupy the same ordered
        sequence as keyboard and mouse instructions.
        """
        previous_deadline = self.parser.execution_deadline
        self.parser.execution_deadline = time.monotonic() + MAX_QUEUE_SECONDS
        try:
            if sum(instruction_seconds(instruction) for instruction in self._ordered_queue) > MAX_QUEUE_SECONDS + 1e-9:
                raise ValueError("Pending actions exceed the 20-second delay budget")
            # Recheck session/permission state immediately before effects. Do
            # this before the first instruction so a mixed script cannot run
            # its SHELL/LAUNCH lines and only then discover that input is denied.
            if any(_instruction_touches_input(i) for i in self._ordered_queue):
                refusal = capabilities.input_execution_refusal(
                    headless=self.headless, backend=keyboard_driver.pyautogui
                )
                if refusal:
                    raise gui_stub.NoDisplayError(refusal)
            for instruction in tuple(self._ordered_queue):
                self.parser._remaining_seconds()
                instruction.execute()
                self.parser._remaining_seconds()
        except Exception:
            # A failed flush must never replay a partially executed launch or
            # shell command on the next ordinary input, or strand a held key.
            self._clear_queues()
            self.parser.last_shell_output = ""
            self.release_all_input()
            raise
        finally:
            self.parser.execution_deadline = previous_deadline

    def release_all_input(self) -> None:
        self._safe(self.keyboard.release_all)
        release_mouse = getattr(self.mouse, "release_all", None)
        if callable(release_mouse):
            self._safe(release_mouse)

    def close(self) -> None:
        """End this executor session without retaining input or listeners."""
        if self._closed:
            return
        self._closed = True
        self._clear_queues()
        self.parser.last_shell_output = ""
        self.release_all_input()
        shutdown = getattr(self.monitor, "shutdown", None)
        if callable(shutdown):
            self._safe(shutdown)

    def accept_file_session(self, command: Dict[str, Any]) -> Optional[str]:
        """Discard work approved by a previous file-IPC server instance.

        Legacy servers may still send immediate, self-clearing commands. They
        cannot retain or drain a queue because they have no session identity.
        """
        session = command.get("execution_session")
        if session is not None and (not isinstance(session, str) or len(session) > 128
                                    or any(ord(char) < 33 or ord(char) > 126 for char in session)):
            self._clear_queues()
            self.release_all_input()
            return "execution_session must be a printable identifier of at most 128 characters"
        session = session or None
        if session is None or session != self._file_execution_session:
            self._clear_queues()
            self.parser.last_shell_output = ""
            self.release_all_input()
            self._file_execution_session = session
        name = str(command.get("type") or "").strip()
        if session is None and (name == "execute_queue" or (name in QUEUE_COMMANDS and
                (not _bool_param(command, "execute_now", True) or not _bool_param(command, "clear_after", True)))):
            return "Queued file IPC needs execution_session; update the server or send immediate commands with clear_after=true"
        return None

    def _ok(self, data: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        result: Dict[str, Any] = {"success": True}
        if data:
            result["data"] = data
        return result

    def _failure(self, message: str) -> Dict[str, Any]:
        return {"success": False, "error": message}


# ----------------------------------------------------------------------
# Wire protocol
# ----------------------------------------------------------------------


def _read_line(stream) -> Optional[str]:
    """Read a bounded UTF-8 frame, including a final line without a newline."""
    raw = stream.readline(MAX_LINE_BYTES + 1)
    if not raw:
        return None
    size = len(raw) if isinstance(raw, bytes) else len(raw.encode("utf-8"))
    if size > MAX_LINE_BYTES:
        raise ValueError("frame too large")
    return raw.decode("utf-8") if isinstance(raw, bytes) else raw


def _write(stream, payload: Dict[str, Any]) -> None:
    data = (json.dumps(payload) + "\n").encode("utf-8")
    sendall = getattr(stream, "sendall", None)
    if callable(sendall):  # a raw socket, which is what connect_role passes in
        sendall(data)
        return
    stream.write(data)
    stream.flush()


def serve_session(conn, agent: Agent, token: Optional[str], peer: str = "bridge") -> bool:
    """Run one session. Returns True when the bridge asked us to shut down."""
    reader = None
    try:
        reader = conn.makefile("rb")
        writer = conn

        hello: Dict[str, Any] = {
            "type": "hello",
            "role": "executor",
            "version": PROTOCOL_VERSION,
        }
        if token:
            hello["token"] = token
        _write(writer, hello)

        first = _read_line(reader)
        if not first:
            raise ConnectionError("bridge closed the connection during the handshake")
        ack = json.loads(first)
        if not isinstance(ack, dict):
            raise ConnectionError("bridge handshake must be an object")
        if ack.get("type") == "hello_nack":
            raise PermissionError(
                f"bridge rejected this agent: {ack.get('error') or 'unspecified'}"
            )
        if ack.get("type") != "hello_ack":
            raise ConnectionError(f"expected hello_ack, got {ack.get('type')!r}")

        version = ack.get("version") or "?"
        print(f"[agent] connected to {peer} (protocol {version}) — waiting for commands")

        while True:
            line = _read_line(reader)
            if line is None:
                return False
            line = line.strip()
            if not line:
                continue

            try:
                envelope = json.loads(line)
            except json.JSONDecodeError as exc:
                print(f"[agent] ignoring malformed frame: {exc}")
                continue

            if not isinstance(envelope, dict):
                print("[agent] ignoring a frame that is not an object")
                continue
            kind = envelope.get("type")
            if kind == "ping":
                _write(writer, {"type": "pong", "id": envelope.get("id", "")})
                continue
            if kind != "command":
                continue

            command = envelope.get("command") or {}
            result = agent.execute(command)
            reply: Dict[str, Any] = {
                "type": "result",
                "id": envelope.get("id", ""),
                "success": bool(result.get("success")),
            }
            if result.get("data") is not None:
                reply["data"] = result["data"]
            if result.get("error"):
                reply["error"] = result["error"]
            _write(writer, reply)

            if (result.get("data") or {}).get("shutdown"):
                print("[agent] shutdown requested by the bridge")
                return True
    finally:
        try:
            if reader is not None:
                reader.close()
        finally:
            agent.close()


# ----------------------------------------------------------------------
# Entry points
# ----------------------------------------------------------------------


def connect_role(bridge: str, token: Optional[str], once: bool) -> int:
    host, _, port = bridge.partition(":")
    port = int(port or "9876")
    attempt = 0
    while True:
        conn, session_agent = None, None
        delay = 2
        try:
            conn = socket.create_connection((host, port), timeout=10)
            conn.settimeout(None)
            session_agent = Agent()
            shutdown = serve_session(conn, session_agent, token, peer=f"{host}:{port}")
            if shutdown:
                return 0
            print("[agent] bridge closed the connection")
            if once:
                return 0
            attempt = 0
        except PermissionError as exc:
            # PermissionError subclasses OSError; authentication failures must
            # be handled first, so a rejected token never enters a retry loop.
            print(f"[agent] not retrying: {exc}")
            return 1
        except (ConnectionError, OSError, ValueError) as exc:
            attempt += 1
            if once:
                print(f"[agent] giving up: {exc}")
                return 1
            delay = min(2 ** min(attempt, 5), 30)
            print(f"[agent] could not reach {host}:{port} ({exc}); retrying in {delay}s")
        finally:
            if session_agent is not None:
                session_agent.close()
            if conn is not None:
                try:
                    conn.close()
                except OSError:
                    pass
        time.sleep(delay)


def ipc_file_role(path: str, once: bool = False) -> int:
    """Serve the bridge's file-IPC fallback: poll `path`, answer `path.response`.

    File IPC is what the bridge falls back to when no TCP executor is connected,
    which is the co-located case (bridge and agent on the same machine). The
    command file is atomically claimed *before* it is read, so a replacement
    request cannot be deleted by a slow previous poll. The response is written
    atomically because the bridge polls for it.
    """
    response_path = path + ".response"
    print(f"[agent] watching {path} (file IPC); answers go to {response_path}")
    agent = Agent()
    if is_headless():
        print(f"[agent] headless: {describe_headless_reason()}")

    try:
        while True:
            handled = process_ipc_file(path, response_path, agent)
            if handled is None:
                time.sleep(0.05)
                continue
            if handled:
                print("[agent] shutdown requested by the bridge")
                return 0
    finally:
        agent.close()


def process_ipc_file(path: str, response_path: str, agent: Agent) -> Optional[bool]:
    """Handle one pending file-IPC command.

    Returns None when there was nothing to do, otherwise whether the command was
    a shutdown request.
    """
    # Claim before reading. Reading the shared path and then unlinking it could
    # delete a replacement request written after the bridge timed out. Once the
    # rename succeeds, this process owns only its unique claimed file; the
    # bridge may safely write the next request to the original path.
    claimed_path = None
    try:
        if not stat.S_ISREG(os.lstat(path).st_mode):
            raise OSError("IPC command must be a regular file, not a symlink or directory")
        descriptor, claimed_path = tempfile.mkstemp(
            prefix=f".{os.path.basename(path)}.claimed-",
            dir=os.path.dirname(os.path.abspath(path)),
        )
        os.close(descriptor)
        os.replace(path, claimed_path)
        if not stat.S_ISREG(os.lstat(claimed_path).st_mode):
            raise OSError("IPC command changed to a nonregular file while being claimed")
        with open(claimed_path, "rb") as handle:
            raw = handle.read(MAX_LINE_BYTES + 1)
    except FileNotFoundError:
        return None
    except OSError as exc:
        print(f"[agent] cannot claim/read {path}: {exc}")
        agent._clear_queues()
        agent.release_all_input()
        return None
    finally:
        if claimed_path is not None:
            try:
                os.remove(claimed_path)
            except FileNotFoundError:
                pass

    if not raw.strip():
        return False

    command = None
    try:
        if len(raw) > MAX_LINE_BYTES:
            raise ValueError("frame too large")
        command = json.loads(raw)
        if not isinstance(command, dict):
            raise ValueError("command must be an object")
    except (ValueError, UnicodeError) as exc:
        result = {"success": False, "error": f"invalid command JSON: {exc}"}
    else:
        error = agent.accept_file_session(command)
        result = agent._failure(error) if error else agent.execute(command)
    if isinstance(command, dict):
        for field in ("execution_session", "request_id"):
            if isinstance(command.get(field), str) and len(command[field]) <= 128:
                result[field] = command[field]

    # The bridge polls for this file, so it has to appear complete: write a
    # temporary file and rename it into place.
    descriptor, temp_path = tempfile.mkstemp(
        prefix=f".{os.path.basename(response_path)}.tmp-",
        dir=os.path.dirname(os.path.abspath(response_path)),
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(result, handle)
        os.replace(temp_path, response_path)
    finally:
        try:
            os.remove(temp_path)
        except FileNotFoundError:
            pass

    return bool((result.get("data") or {}).get("shutdown"))


def main(argv: Optional[list] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Neuro Desktop agent (runs on the machine Neuro controls)"
    )
    target = parser.add_mutually_exclusive_group()
    target.add_argument(
        "--bridge",
        default=os.environ.get("NEURO_AGENT_BRIDGE", "127.0.0.1:9876"),
        help="host:port of the bridge executor hub to connect to (default: %(default)s)",
    )
    target.add_argument(
        "--ipc-file",
        default=None,
        help="serve the bridge file-IPC fallback at this path (NEURO_IPC_FILE)",
    )
    parser.add_argument(
        "--token",
        default=os.environ.get("NEURO_EXECUTOR_TOKEN"),
        help="shared secret the bridge requires (NEURO_EXECUTOR_TOKEN)",
    )
    parser.add_argument(
        "--once",
        action="store_true",
        help="exit after the first session instead of reconnecting",
    )
    args = parser.parse_args(argv)

    if args.ipc_file:
        print("[agent] role: file IPC (same machine as the bridge)")
        return ipc_file_role(args.ipc_file, args.once)

    print("[agent] role: client (connecting out); the bridge owns the socket")
    if is_headless():
        print(f"[agent] headless: {describe_headless_reason()}")
    return connect_role(args.bridge, args.token or None, args.once)


if __name__ == "__main__":
    raise SystemExit(main())
