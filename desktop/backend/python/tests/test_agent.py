"""The Python agent: the process that executes commands on the controlled PC.

It replaces the Rust executor, so these tests are the contract the Go bridge
relies on:

* every command the bridge can send is handled (pinned against the Go source,
  so the two sides cannot drift apart silently),
* the wire handshake and result frames match `executor_hub.go`,
* the file-IPC fallback works without any TCP connection,
* headless machines refuse input actions instead of pretending to work.
"""

import json
import os
import pathlib
import socket
import sys
import tempfile
import threading
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller.agent import (  # noqa: E402
    EXECUTOR_COMMANDS,
    PROTOCOL_VERSION,
    Agent,
    process_ipc_file,
    serve_session,
)

REPO = pathlib.Path(__file__).resolve().parents[4]
GO_DIR = REPO / "desktop" / "apps" / "neuro-integration"


def go_command_constants():
    """Constant name -> wire string, read from the Go bridge source."""
    import re

    source = (GO_DIR / "action-registry.go").read_text(encoding="utf-8")
    return dict(
        re.findall(r'\b(Cmd\w+)\s+CommandType\s*=\s*"([a-z_]+)"', source)
    )


def go_executor_commands():
    """The command strings the Go bridge declares as executor-bound."""
    constants = go_command_constants()
    source = (GO_DIR / "executor_commands.go").read_text(encoding="utf-8")
    names = []
    for line in source.splitlines():
        line = line.strip()
        if not line or line.startswith("//"):
            continue
        name = line.split(":")[0].strip()
        if name.startswith("Cmd"):
            names.append(name)
    return {constants.get(name, name): True for name in names}


class ProtocolParityTest(unittest.TestCase):
    """Both languages must agree on which commands the agent executes."""

    def test_go_executor_list_is_readable(self):
        commands = go_executor_commands()
        self.assertGreater(len(commands), 5)
        for wire in commands:
            self.assertRegex(wire, r"^[a-z_]+$")

    def test_python_agent_handles_every_go_executor_command(self):
        go_commands = set(go_executor_commands())
        agent_commands = set(EXECUTOR_COMMANDS)
        missing = go_commands - agent_commands
        extra = agent_commands - go_commands
        self.assertEqual(
            missing,
            set(),
            f"the Go bridge can send these but the agent does not handle them: {sorted(missing)}",
        )
        self.assertEqual(
            extra,
            set(),
            f"the agent claims commands the bridge never sends: {sorted(extra)}",
        )

    def test_protocol_version_matches_the_bridge(self):
        import re

        source = (GO_DIR / "utils.go").read_text(encoding="utf-8")
        match = re.search(r'bridgeProtocolVersion\s*=\s*"(\d+)"', source)
        self.assertIsNotNone(match, "bridgeProtocolVersion not found in utils.go")
        self.assertEqual(match.group(1), PROTOCOL_VERSION)


class CommandCoverageTest(unittest.TestCase):
    """`execute` must understand the protocol, not just the happy path."""

    def setUp(self):
        self.agent = Agent()

    def test_every_declared_command_is_dispatched(self):
        for name in EXECUTOR_COMMANDS:
            result = self.agent.execute({"type": name, "params": {}})
            self.assertNotIn(
                "is not an executor command",
                result.get("error") or "",
                f"{name} is declared but not handled by execute()",
            )
            self.assertIsInstance(result, dict)
            self.assertIn("success", result)

    def test_unknown_command_explains_the_mismatch(self):
        result = self.agent.execute({"type": "game_move", "params": {}})
        self.assertFalse(result["success"])
        self.assertIn("bridge handles it", result["error"])

    def test_missing_params_produce_actionable_errors(self):
        cases = {
            "type_text": "text",
            "key_press": "key",
            "key_combo": "keys",
            "key_hold_for": "key",
            "run_script": "script",
            "shell_command": "command",
        }
        for name, param in cases.items():
            result = self.agent.execute({"type": name, "params": {}})
            self.assertFalse(result["success"], f"{name} accepted empty params")
            self.assertIn(param, result["error"], f"{name} error should name the parameter")

    def test_bad_params_type_is_rejected(self):
        result = self.agent.execute({"type": "get_status", "params": "not-an-object"})
        self.assertFalse(result["success"])
        self.assertIn("object", result["error"])

    def test_status_reports_headless_and_platform(self):
        result = self.agent.execute({"type": "get_status", "params": {}})
        self.assertTrue(result["success"], result.get("error"))
        data = result["data"]
        for key in ("headless", "platform", "open_windows", "running_processes"):
            self.assertIn(key, data)
        self.assertIsInstance(data["headless"], bool)

    def test_missing_input_driver_does_not_report_stub_geometry_as_a_screen(self):
        from controller.gui_stub import StubPyAutoGUI

        self.agent.headless = False
        with mock.patch("controller.agent.keyboard_driver.pyautogui", StubPyAutoGUI("driver unavailable")):
            data = self.agent.status({})
        self.assertFalse(data["input_available"])
        self.assertEqual(data["input_error"], "driver unavailable")
        self.assertIsNone(data["screen"])
        self.assertIsNone(data["mouse_position"])

    def test_shell_command_returns_exit_code_and_output(self):
        result = self.agent.execute(
            {"type": "shell_command", "params": {"command": "echo agent-ok"}}
        )
        if not result["success"]:
            # The shipped firewall denies commands that are not allow-listed.
            self.assertIn("allowlist", result["error"].lower())
            return
        self.assertIn("exit code: 0", result["data"]["output"])
        self.assertIn("agent-ok", result["data"]["output"])

    def test_execute_now_false_defers_the_queue(self):
        # A real input command is used because the queue is what is under test,
        # not the display: on a headless box the deferred queue is cleared
        # before anything tries to touch a screen.
        result = self.agent.execute(
            {
                "type": "move_mouse_to",
                "params": {"x": 10, "y": 10},
                "execute_now": False,
                "clear_after": False,
            }
        )
        self.assertTrue(result["success"], result.get("error"))
        self.assertEqual(len(self.agent.mouse.instruction_queue), 1)

        cleared = self.agent.execute({"type": "clear_action_queue", "params": {}})
        self.assertTrue(cleared["success"])
        self.assertEqual(len(self.agent.mouse.instruction_queue), 0)

    def test_shutdown_reports_the_bridge_request(self):
        result = self.agent.execute({"type": "shutdown_gracefully", "params": {}})
        self.assertTrue(result["success"])
        self.assertTrue(result["data"]["shutdown"])


class SessionTest(unittest.TestCase):
    """The wire protocol between the server and the agent, on the Python side."""

    def _bridge(self, conn, token_ok=True, commands=None):
        """Act as the Go bridge on the other end of a socket pair."""
        conn.settimeout(10)
        reader = conn.makefile("r", encoding="utf-8", newline="\n")
        hello = json.loads(reader.readline())
        self.assertEqual(hello["type"], "hello")
        self.assertEqual(hello["role"], "executor")
        self.assertEqual(hello["version"], PROTOCOL_VERSION)
        self.seen_hello = hello

        if not token_ok:
            conn.sendall(b'{"type":"hello_nack","error":"invalid or missing executor token"}\n')
            return

        conn.sendall(
            json.dumps({"type": "hello_ack", "role": "bridge", "version": PROTOCOL_VERSION}).encode()
            + b"\n"
        )
        conn.sendall(b'{"type":"ping","id":"ping-1"}\n')
        for index, command in enumerate(commands or []):
            conn.sendall(
                json.dumps({"type": "command", "id": f"cmd-{index}", "command": command}).encode()
                + b"\n"
            )

    def test_handshake_command_and_ping(self):
        bridge, agent_side = socket.socketpair()
        self.addCleanup(bridge.close)
        self.addCleanup(agent_side.close)

        commands = [
            {"type": "get_status", "params": {"max_open_windows": 1}},
            {"type": "shutdown_gracefully", "params": {}},
        ]
        thread = threading.Thread(
            target=serve_session, args=(agent_side, Agent(), "shared", "test"), daemon=True
        )
        try:
            thread.start()
            self._bridge(bridge, commands=commands)

            reader = bridge.makefile("r", encoding="utf-8", newline="\n")
            self.assertEqual(json.loads(reader.readline())["type"], "pong")
            first = json.loads(reader.readline())
            self.assertEqual(first["type"], "result")
            self.assertEqual(first["id"], "cmd-0")
            self.assertTrue(first["success"], first.get("error"))
            self.assertIn("headless", first["data"])

            second = json.loads(reader.readline())
            self.assertTrue(second["data"]["shutdown"])
        finally:
            thread.join(timeout=5)
        self.assertEqual(self.seen_hello.get("token"), "shared")
        self.assertFalse(thread.is_alive(), "the session should end after a shutdown command")

    def test_rejected_token_is_reported_not_retried(self):
        bridge, agent_side = socket.socketpair()
        self.addCleanup(bridge.close)
        self.addCleanup(agent_side.close)

        errors = []

        def run():
            try:
                serve_session(agent_side, Agent(), "wrong", "test")
            except Exception as exc:  # noqa: BLE001 - captured for the assertion
                errors.append(exc)

        thread = threading.Thread(target=run, daemon=True)
        thread.start()
        self._bridge(bridge, token_ok=False)
        thread.join(timeout=5)

        self.assertTrue(errors, "a rejected token must raise instead of hanging")
        self.assertIsInstance(errors[0], PermissionError)
        self.assertIn("token", str(errors[0]))

    def test_malformed_frame_is_ignored(self):
        bridge, agent_side = socket.socketpair()
        self.addCleanup(bridge.close)
        self.addCleanup(agent_side.close)

        thread = threading.Thread(
            target=lambda: serve_session(agent_side, Agent(), None, "test"), daemon=True
        )
        thread.start()
        self._bridge(bridge, commands=[])
        bridge.sendall(b"this is not json\n")
        bridge.sendall(b'{"type":"command","id":"ok","command":{"type":"get_status"}}\n')

        reader = bridge.makefile("r", encoding="utf-8", newline="\n")
        self.assertEqual(json.loads(reader.readline())["type"], "pong")
        result = json.loads(reader.readline())
        self.assertEqual(result["id"], "ok")
        self.assertTrue(result["success"])
        bridge.close()
        thread.join(timeout=5)


class FileIpcTest(unittest.TestCase):
    """The co-located fallback used when no TCP executor is connected."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = os.path.join(self.tmp.name, "ipc.json")
        self.response = self.path + ".response"
        self.agent = Agent()

    def test_nothing_pending_returns_none(self):
        self.assertIsNone(process_ipc_file(self.path, self.response, self.agent))

    def test_command_file_is_consumed_and_answered_atomically(self):
        with open(self.path, "w", encoding="utf-8") as handle:
            json.dump({"type": "get_status", "params": {}}, handle)

        handled = process_ipc_file(self.path, self.response, self.agent)
        self.assertFalse(handled)
        self.assertFalse(os.path.exists(self.path), "the command file must be consumed")
        self.assertFalse(os.path.exists(self.response + ".tmp"))

        with open(self.response, encoding="utf-8") as handle:
            payload = json.load(handle)
        self.assertTrue(payload["success"])
        self.assertIn("headless", payload["data"])

    def test_invalid_json_is_answered_with_a_failure(self):
        with open(self.path, "w", encoding="utf-8") as handle:
            handle.write("{not json")

        process_ipc_file(self.path, self.response, self.agent)
        with open(self.response, encoding="utf-8") as handle:
            payload = json.load(handle)
        self.assertFalse(payload["success"])
        self.assertIn("invalid command JSON", payload["error"])

    def test_shutdown_is_reported_to_the_loop(self):
        with open(self.path, "w", encoding="utf-8") as handle:
            json.dump({"type": "shutdown_immediately", "params": {}}, handle)

        self.assertTrue(process_ipc_file(self.path, self.response, self.agent))


if __name__ == "__main__":
    unittest.main()
