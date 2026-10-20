"""Exercise the real instruction builders against a recording OS adapter.

No input, process launch or shell command reaches the test machine. These tests
cover observable ordering and atomic preflight rather than parser internals.
"""

import io
import json
import pathlib
import socket
import tempfile
import threading
import sys
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller import actions, agent, gui_stub  # noqa: E402
from controller.controls import keyboard, mouse  # noqa: E402


class Monitor:
    def record_action(self, **kwargs):
        pass


class RecordingOS:
    KEYBOARD_KEYS = gui_stub.KEYBOARD_KEYS

    def __init__(self):
        self.events = []

    def size(self):
        return (1000, 600)

    def keyDown(self, key, **kwargs):
        self.events.append(("down", key))

    def keyUp(self, key, **kwargs):
        self.events.append(("up", key))

    def press(self, key):
        self.events.append(("press", key))

    def write(self, text, **kwargs):
        self.events.append(("type", text))

    def moveRel(self, dx, dy, **kwargs):
        self.events.append(("move", dx, dy))

    def click(self, button):
        self.events.append(("click", button))


class ScriptExecutionTests(unittest.TestCase):
    def setUp(self):
        self.os = RecordingOS()
        for patch in (
            mock.patch.object(keyboard, "pyautogui", self.os),
            mock.patch.object(mouse, "pyautogui", self.os),
            mock.patch.dict("os.environ", {"NEURO_SHELL_ALLOWLIST": "echo", "NEURO_SHELL_DENYLIST": ""}),
            mock.patch.object(keyboard.time, "sleep", side_effect=lambda seconds: self.os.events.append(("wait", seconds))),
            mock.patch.object(actions, "run_shell_command", side_effect=self.shell),
            mock.patch.object(actions, "launch_target", side_effect=lambda target, **kwargs: self.os.events.append(("launch", target))),
        ):
            patch.start()
            self.addCleanup(patch.stop)
        monitor = Monitor()
        self.mouse = mouse.MouseController(monitor)
        self.keyboard = keyboard.KeyboardController(monitor)
        self.parser = actions.ActionParser(self.keyboard, self.mouse, monitor, platform="linux")
        with mock.patch.object(agent, "initialize_driver", return_value=(monitor, self.mouse, self.keyboard, self.parser)):
            self.agent = agent.Agent()
        self.addCleanup(self.agent.close)

    def shell(self, command, **kwargs):
        self.os.events.append(("shell", command))
        return "exit code: 0\nstdout: done"

    def command(self, name, params, **flags):
        return self.agent.execute({"type": name, "params": params, **flags})

    def test_mixed_script_executes_once_in_source_order(self):
        result = self.command("run_script", {"script": '\n'.join([
            "HOLD w", "MOVE_REL 4 -2", "WAIT 0.7", 'TYPE "hello"',
            'SHELL "echo done"', 'LAUNCH "steam://run/1"', "RELEASE_ALL",
        ])})
        self.assertTrue(result["success"], result)
        self.assertEqual(self.os.events, [
            ("down", "w"), ("move", 4, -2), ("wait", 0.7), ("type", "hello"),
            ("shell", "echo done"), ("launch", "steam://run/1"), ("up", "w"),
        ])
        self.assertFalse(self.keyboard.held_keys)
        self.assertIn("done", result["data"]["output"])
        self.command("execute_queue", {})
        self.assertEqual(len(self.os.events), 7, "clear_after=True must prevent replay")

    def test_runtime_failure_releases_input_held_by_an_earlier_line(self):
        with mock.patch.object(actions, "launch_target", side_effect=actions.LaunchError("opener failed")):
            result = self.command("run_script", {"script": 'HOLD w\nLAUNCH "steam://run/1"\nRELEASE_ALL'})
        self.assertFalse(result["success"], result)
        self.assertIn("opener failed", result["error"])
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
        self.assertFalse(self.keyboard.held_keys)

    def test_failed_flush_never_replays_earlier_launch_or_shell(self):
        def shell(command, **kwargs):
            self.os.events.append(("shell", command))
            if command == "echo fail":
                raise actions.ShellTimeoutError("timed out")
            return "first command output"

        with mock.patch.object(actions, "run_shell_command", side_effect=shell):
            result = self.command("run_script", {"script": 'LAUNCH "steam://run/1"\nSHELL "echo first"\nSHELL "echo fail"'})
        self.assertFalse(result["success"], result)
        expected = [("launch", "steam://run/1"), ("shell", "echo first"), ("shell", "echo fail")]
        self.assertEqual(self.os.events, expected)
        later = self.command("mouse_click", {})
        self.assertTrue(later["success"], later)
        self.assertNotIn("data", later, "old shell output must not leak into the new result")
        self.assertEqual(self.os.events, expected + [("click", "left")])
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, expected + [("click", "left")])

    def test_every_effect_is_deferred_and_clear_discards_it(self):
        script = 'HOLD w\nSHELL "echo done"\nLAUNCH "steam://run/1"\nRELEASE_ALL'
        result = self.command("run_script", {"script": script}, execute_now=False, clear_after=False)
        self.assertTrue(result["success"], result)
        self.assertEqual(self.os.events, [])
        self.command("clear_action_queue", {})
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, [])

    def test_deferred_commands_keep_wire_order_and_explicit_replay(self):
        self.command("key_press", {"key": "a"}, execute_now=False, clear_after=False)
        self.command("mouse_click", {}, execute_now=False, clear_after=False)
        self.command("run_script", {"script": 'TYPE "third"'}, execute_now=False, clear_after=False)
        self.assertEqual(self.os.events, [])
        expected = [("press", "a"), ("wait", 0.02), ("click", "left"), ("type", "third")]
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, expected)
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, expected * 2)

    def test_invalid_later_line_cannot_perform_or_enqueue_earlier_effects(self):
        bad_lines = ["UNKNOWN", "HOLD definitely_not_a_key", "WAIT nan", "WAIT inf", "WAIT -1",
                     "MOVE 1 2 -1", "MOVE_N 1.1 0", "MOVE_REL 1 2 inf", "ENTER extra",
                     "RELEASE_ALL extra", "LINE 0 0 1 1 STEPS 0", "PATH 1 2 3", 'TYPE "unterminated']
        for bad in bad_lines:
            with self.subTest(bad=bad):
                result = self.command("run_script", {"script": 'HOLD w\nSHELL "echo done"\nLAUNCH "steam://run/1"\n' + bad})
                self.assertFalse(result["success"], result)
                self.assertIn("Line 4", result["error"])
                self.assertEqual(self.os.events, [])
                self.command("execute_queue", {})
                self.assertEqual(self.os.events, [])

    def test_failed_preflight_leaves_previous_deferred_command_intact(self):
        self.command("type_text", {"text": "earlier"}, execute_now=False, clear_after=False)
        result = self.command("run_script", {"script": 'TYPE "unwanted"\nBAD'})
        self.assertFalse(result["success"])
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, [("type", "earlier")])

    def test_shell_policy_is_preflighted_and_snapshotted_per_script(self):
        result = self.command("run_script", {"script": 'TYPE "unwanted"\nSHELL "echo denied"', "allowlist": ["ls"]})
        self.assertFalse(result["success"])
        self.assertEqual(self.os.events, [])
        result = self.command("run_script", {"script": 'SHELL "echo first"', "allowlist": ["echo"]}, execute_now=False, clear_after=False)
        self.assertTrue(result["success"], result)
        self.parser.shell_policy = {"allowlist": ["ls"]}
        with mock.patch.object(actions, "run_shell_command", return_value="output") as run:
            self.command("execute_queue", {})
        self.assertEqual(run.call_args.kwargs["policy"], {"allowlist": ["echo"], "denylist": []})

    def test_script_and_cross_request_queue_are_bounded(self):
        for script in ('TYPE "' + 'x' * 65536 + '"', "WAIT 0\n" * 513,
                       "PATH " + "0 0 " * 257):
            with self.subTest(size=len(script)):
                result = self.command("run_script", {"script": script}, execute_now=False, clear_after=False)
                self.assertFalse(result["success"], result)
                self.assertEqual(self.os.events, [])
        for _ in range(512):
            self.assertTrue(self.command("mouse_click", {}, execute_now=False, clear_after=False)["success"])
        self.assertFalse(self.command("mouse_click", {}, execute_now=False, clear_after=False)["success"])
        self.assertEqual(len(self.mouse.instruction_queue), 512)

    def test_deliberate_delays_are_bounded_before_any_effect(self):
        scripts = ["WAIT 10\nWAIT 10", 'TYPE "' + 'x' * 1000 + '"',
                   "LINE 0 0 1 1 STEPS 1000", "PATH " + "0 0 " * 34]
        for script in scripts:
            with self.subTest(script=script[:40]):
                result = self.command("run_script", {"script": 'HOLD w\n' + script})
                self.assertFalse(result["success"], result)
                self.assertIn("20-second delay budget", result["error"])
                self.assertEqual(self.os.events, [])
        with mock.patch.object(self.os, "PAUSE", 1.0, create=True):
            result = self.command("run_script", {"script": "CLICK\n" * 20})
        self.assertFalse(result["success"], result)
        self.assertEqual(self.os.events, [])

    def test_retained_queue_delay_budget_cannot_grow_across_requests(self):
        first = self.command("run_script", {"script": "WAIT 11"}, execute_now=False, clear_after=False)
        second = self.command("run_script", {"script": "WAIT 11"}, execute_now=False, clear_after=False)
        self.assertTrue(first["success"], first)
        self.assertFalse(second["success"], second)
        self.assertEqual(self.os.events, [])
        self.command("execute_queue", {})
        self.assertEqual(self.os.events, [("wait", 11.0)])

    def test_shell_uses_remaining_wall_clock_budget_and_stops_later_actions(self):
        now = [0.0]

        def delayed_shell(command, **kwargs):
            self.assertGreater(kwargs["timeout"], 0)
            self.assertLessEqual(kwargs["timeout"], 20.0)
            now[0] = 21.0
            return "shell completed late"

        with mock.patch.object(actions.time, "monotonic", side_effect=lambda: now[0]), \
                mock.patch.object(actions, "run_shell_command", side_effect=delayed_shell):
            result = self.command("run_script", {"script": 'HOLD w\nSHELL "echo done"\nTYPE "later"'})
        self.assertFalse(result["success"], result)
        self.assertIn("20-second execution budget", result["error"])
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
        self.assertFalse(self.keyboard.held_keys)

    def test_session_eof_clears_input_and_stops_monitor_once(self):
        frames = [
            {"type": "hello_ack"},
            {"type": "command", "id": "hold", "command": {"type": "run_script", "params": {"script": "HOLD w"}}},
            {"type": "command", "id": "queued", "command": {"type": "run_script", "params": {"script": 'LAUNCH "later"'}, "execute_now": False, "clear_after": False}},
        ]
        reader = io.BytesIO(b"".join(json.dumps(frame).encode() + b"\n" for frame in frames))
        connection = mock.Mock()
        connection.makefile.return_value = reader
        self.agent.monitor.shutdown = mock.Mock()
        self.assertFalse(agent.serve_session(connection, self.agent, None))
        self.agent.close()
        self.assertTrue(reader.closed)
        self.agent.monitor.shutdown.assert_called_once_with()
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
        self.assertFalse(self.agent._ordered_queue)
        self.assertFalse(self.parser.queue)
        self.assertFalse(self.keyboard.held_keys)

    def test_real_socket_disconnect_discards_queued_effects_and_releases_input(self):
        bridge, executor = socket.socketpair()
        bridge.settimeout(3)
        self.addCleanup(bridge.close)
        self.addCleanup(executor.close)
        self.agent.monitor.shutdown = mock.Mock()
        failures = []

        def serve():
            try:
                agent.serve_session(executor, self.agent, None)
            except Exception as error:
                failures.append(error)

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        reader = bridge.makefile("rb")
        self.addCleanup(reader.close)
        self.assertEqual(json.loads(reader.readline())["type"], "hello")
        bridge.sendall(b'{"type":"hello_ack"}\n')
        for command in (
            {"type": "run_script", "params": {"script": "HOLD w"}},
            {"type": "run_script", "params": {"script": 'LAUNCH "stale"'}, "execute_now": False, "clear_after": False},
        ):
            bridge.sendall(json.dumps({"type": "command", "id": "test", "command": command}).encode() + b"\n")
            self.assertTrue(json.loads(reader.readline())["success"])
        self.assertEqual(self.os.events, [("down", "w")])
        bridge.shutdown(socket.SHUT_WR)
        thread.join(timeout=3)
        self.assertFalse(thread.is_alive(), "EOF must close the executor session")
        self.assertFalse(failures, failures)
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
        self.assertFalse(self.agent._ordered_queue)
        self.agent.monitor.shutdown.assert_called_once_with()

    def test_closed_agent_cannot_retain_or_execute_new_work(self):
        self.command("run_script", {"script": "HOLD w"})
        self.agent.close()
        self.agent.close()
        result = self.command("run_script", {"script": 'LAUNCH "after-close"'})
        self.assertFalse(result["success"], result)
        self.assertIn("session is closed", result["error"])
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])

    def test_file_server_restart_discards_previous_approval_and_held_input(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "ipc.json"
            response = str(path) + ".response"

            def send(command):
                path.write_text(json.dumps(command))
                agent.process_ipc_file(str(path), response, self.agent)
                return json.loads(pathlib.Path(response).read_text())

            first = send({"type": "run_script", "execution_session": "server-a", "params": {"script": "HOLD w"}})
            self.assertTrue(first["success"], first)
            queued = send({"type": "run_script", "execution_session": "server-a", "params": {"script": 'SHELL "echo stale"\nLAUNCH "old"'}, "execute_now": False, "clear_after": False})
            self.assertTrue(queued["success"], queued)
            drained = send({"type": "execute_queue", "execution_session": "server-b", "request_id": "3"})
            self.assertTrue(drained["success"], drained)
            self.assertEqual(drained["execution_session"], "server-b")
            self.assertEqual(drained["request_id"], "3")
            self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
            self.assertFalse(self.agent._ordered_queue)

    def test_legacy_file_server_can_execute_immediate_but_not_retain_work(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "ipc.json"
            response = str(path) + ".response"
            for command in (
                {"type": "run_script", "params": {"script": 'LAUNCH "old"'}, "execute_now": False, "clear_after": False},
                {"type": "execute_queue"},
            ):
                path.write_text(json.dumps(command))
                agent.process_ipc_file(str(path), response, self.agent)
                result = json.loads(pathlib.Path(response).read_text())
                self.assertFalse(result["success"], result)
                self.assertIn("execution_session", result["error"])
            immediate = {"type": "run_script", "params": {"script": 'TYPE "legacy"'}}
            path.write_text(json.dumps(immediate))
            agent.process_ipc_file(str(path), response, self.agent)
            self.assertTrue(json.loads(pathlib.Path(response).read_text())["success"])
            self.assertEqual(self.os.events, [("type", "legacy")])

    def test_invalid_file_session_discards_prior_work_without_an_effect(self):
        self.command("run_script", {"script": "HOLD w"})
        self.command("run_script", {"script": 'LAUNCH "stale"'}, execute_now=False, clear_after=False)
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "ipc.json"
            response = str(path) + ".response"
            path.write_text(json.dumps({"type": "execute_queue", "execution_session": "bad\nidentifier", "request_id": "bad"}))
            agent.process_ipc_file(str(path), response, self.agent)
            result = json.loads(pathlib.Path(response).read_text())
        self.assertFalse(result["success"], result)
        self.assertIn("execution_session", result["error"])
        self.assertEqual(result["request_id"], "bad")
        self.assertEqual(self.os.events, [("down", "w"), ("up", "w")])
        self.assertFalse(self.agent._ordered_queue)

    def test_headless_script_refuses_all_input_before_running_shell(self):
        with mock.patch.object(keyboard, "pyautogui", gui_stub.StubPyAutoGUI()):
            result = self.command("run_script", {"script": 'SHELL "echo no"\nCLICK'})
        self.assertFalse(result["success"], result)
        self.assertIn("shell_command", result["error"])
        self.assertEqual(self.os.events, [])


if __name__ == "__main__":
    unittest.main()
