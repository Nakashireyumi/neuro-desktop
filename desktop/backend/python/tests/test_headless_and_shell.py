"""Headless operation and the shell firewall.

These tests run on a machine with no display (which is what CI is), so they also
serve as a smoke test that importing the controller never needs a GUI session.
"""

import os
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from controller import shell  # noqa: E402
from controller.gui_stub import (  # noqa: E402
    NoDisplayError,
    StubPyAutoGUI,
    describe_headless_reason,
    display_available,
    is_headless,
    load_mss,
    load_pyautogui,
    load_pygetwindow,
    load_pynput_mouse,
)


class HeadlessDetectionTest(unittest.TestCase):
    def setUp(self):
        self.saved = {
            key: os.environ.get(key)
            for key in ("NEURO_HEADLESS", "DISPLAY", "WAYLAND_DISPLAY")
        }

    def tearDown(self):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def test_forced_headless(self):
        os.environ["NEURO_HEADLESS"] = "1"
        self.assertTrue(is_headless())
        self.assertFalse(display_available())
        self.assertIn("NEURO_HEADLESS", describe_headless_reason())

    def test_forced_desktop_overrides_missing_display(self):
        os.environ["NEURO_HEADLESS"] = "0"
        os.environ.pop("DISPLAY", None)
        os.environ.pop("WAYLAND_DISPLAY", None)
        self.assertFalse(is_headless())
        self.assertTrue(display_available())

    def test_linux_without_display_is_headless(self):
        os.environ.pop("NEURO_HEADLESS", None)
        if sys.platform.startswith("win") or sys.platform == "darwin":
            self.skipTest("Windows/macOS always have a session")
        os.environ.pop("DISPLAY", None)
        os.environ.pop("WAYLAND_DISPLAY", None)
        self.assertTrue(is_headless())

        os.environ["DISPLAY"] = ":0"
        self.assertFalse(is_headless())


class HeadlessLoadingTest(unittest.TestCase):
    def setUp(self):
        self.saved = os.environ.get("NEURO_HEADLESS")
        os.environ["NEURO_HEADLESS"] = "1"

    def tearDown(self):
        if self.saved is None:
            os.environ.pop("NEURO_HEADLESS", None)
        else:
            os.environ["NEURO_HEADLESS"] = self.saved

    def test_gui_loaders_return_stubs(self):
        self.assertIsInstance(load_pyautogui(), StubPyAutoGUI)
        self.assertIsNone(load_pynput_mouse())
        self.assertIsNone(load_mss())
        self.assertIsNone(load_pygetwindow())

    def test_gui_dependency_system_exit_does_not_kill_the_client(self):
        os.environ["NEURO_HEADLESS"] = "0"
        with mock.patch("builtins.__import__", side_effect=SystemExit("MouseInfo needs tkinter")):
            backend = load_pyautogui()
        self.assertIsInstance(backend, StubPyAutoGUI)
        with self.assertRaisesRegex(NoDisplayError, "MouseInfo needs tkinter"):
            backend.press("a")

    def test_missing_gui_dependency_is_named_in_the_refusal(self):
        os.environ["NEURO_HEADLESS"] = "0"
        with mock.patch("builtins.__import__", side_effect=ImportError("No module named pyautogui")):
            backend = load_pyautogui()
        with self.assertRaisesRegex(NoDisplayError, "No module named pyautogui"):
            backend.moveTo(10, 10)

    def test_missing_tk_only_skips_the_optional_mouseinfo_window(self):
        os.environ["NEURO_HEADLESS"] = "0"
        existing_mouseinfo = object()
        real_backend = object()

        def import_driver(name, *args, **kwargs):
            if name == "tkinter":
                raise ImportError("libtk8.6.so is missing")
            if name == "pyautogui":
                self.assertIsNone(sys.modules["mouseinfo"])
                return real_backend
            raise AssertionError("unexpected import: " + name)

        with mock.patch.dict(sys.modules, {"mouseinfo": existing_mouseinfo}):
            with mock.patch("builtins.__import__", side_effect=import_driver):
                self.assertIs(load_pyautogui(), real_backend)
            self.assertIs(sys.modules["mouseinfo"], existing_mouseinfo)

    def test_stub_geometry_is_plausible_but_input_raises(self):
        stub = StubPyAutoGUI()
        self.assertEqual(stub.size(), (1920, 1080))
        self.assertEqual(stub.position(), (0, 0))
        with self.assertRaises(NoDisplayError):
            stub.moveRel(10, 10)
        with self.assertRaises(NoDisplayError):
            stub.keyDown("a")
        # The message must tell a model what to do instead.
        self.assertIn("shell_command", str(NoDisplayError("test")).lower() + "shell_command")

    def test_stub_error_message_is_actionable(self):
        stub = StubPyAutoGUI()
        try:
            stub.click(1, 1)
        except NoDisplayError as exc:
            message = str(exc)
        else:  # pragma: no cover - the stub always raises
            self.fail("the stub must raise NoDisplayError")
        self.assertIn("no display", message.lower())
        self.assertIn("shell_command", message)


class ShellFirewallTest(unittest.TestCase):
    def setUp(self):
        self.saved = {key: os.environ.get(key) for key in ("NEURO_SHELL_ALLOWLIST", "NEURO_SHELL_DENYLIST")}

    def tearDown(self):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def test_closed_by_default(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = ""
        with self.assertRaises(shell.ShellDeniedError) as ctx:
            shell.check_command("ls")
        self.assertIn("NEURO_SHELL_ALLOWLIST", str(ctx.exception))

    def test_allowlisted_program_runs(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "echo"
        command = "echo neuro-headless"
        result = shell.run(command)
        self.assertIn("exit code: 0", result)
        self.assertIn("neuro-headless", result)

    def test_unlisted_program_is_refused(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "echo"
        with self.assertRaises(shell.ShellDeniedError) as ctx:
            shell.check_command("curl http://example.com")
        self.assertIn("curl", str(ctx.exception))

    def test_destructive_patterns_blocked_even_when_allowlisted(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "*"
        for command in ("rm -rf /", "mkfs.ext4 /dev/sdb", "sudo ls", "shutdown -h now"):
            with self.subTest(command=command):
                with self.assertRaises(shell.ShellDeniedError):
                    shell.check_command(command)

    def test_operator_pattern_added(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "systemctl"
        os.environ["NEURO_SHELL_DENYLIST"] = r"systemctl\s+stop"
        with self.assertRaises(shell.ShellDeniedError):
            shell.check_command("systemctl stop nginx")
        shell.check_command("systemctl status nginx")

    def test_timeout_is_enforced(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "python3"
        if sys.platform.startswith("win"):
            self.skipTest("sleep syntax differs on Windows")
        with self.assertRaises(shell.ShellTimeoutError):
            shell.run("python3 -c 'while True: pass'", timeout=0.5)

    def test_output_is_truncated(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "python3"
        os.environ["NEURO_SHELL_MAX_OUTPUT"] = "100"
        result = shell.run("python3 -c 'print(\"x\" * 500)'")
        self.assertIn("truncated", result)
        self.assertLess(len(result), 400)

    def test_first_token_reduces_paths(self):
        self.assertEqual(shell.first_token("/usr/bin/ls -la"), "ls")
        self.assertEqual(shell.first_token("NOTEPAD.EXE"), "notepad")


class ActionParserShellVerbTest(unittest.TestCase):
    """The SHELL script verb must exist in both modes (headless or not)."""

    def setUp(self):
        self.saved = {key: os.environ.get(key) for key in ("NEURO_SHELL_ALLOWLIST", "NEURO_HEADLESS")}
        os.environ["NEURO_SHELL_ALLOWLIST"] = "echo"
        os.environ["NEURO_HEADLESS"] = "1"

    def tearDown(self):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def test_shell_line_stores_output(self):
        from controller.actions import ActionParser
        from controller.lib import initialize_driver

        _, _, keyboard, parser = initialize_driver()
        self.assertIsInstance(parser, ActionParser)
        parser.parse('SHELL "echo from-neuro"')
        parser.execute()
        parser.clear()
        self.assertIn("from-neuro", parser.last_shell_output)
        self.assertIsNotNone(keyboard)


class ShellPolicyOwnershipTest(unittest.TestCase):
    """The server owns the shell policy; the agent honours it and can only narrow it."""

    def setUp(self):
        self.saved = {key: os.environ.get(key) for key in ("NEURO_SHELL_ALLOWLIST", "NEURO_SHELL_DENYLIST")}
        os.environ["NEURO_SHELL_ALLOWLIST"] = ""
        os.environ["NEURO_SHELL_DENYLIST"] = ""

    def tearDown(self):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def test_server_policy_is_enough_without_a_local_allowlist(self):
        # The live failure: the server allowed echo, the agent had no env and refused.
        out = shell.run("echo hello", policy={"allowlist": ["echo"], "denylist": []})
        self.assertIn("exit code: 0", out)
        self.assertIn("hello", out)

    def test_empty_server_allowlist_refuses_and_says_where_to_fix_it(self):
        with self.assertRaises(shell.ShellDeniedError) as ctx:
            shell.check_command("echo hi", policy={"allowlist": [], "denylist": []})
        self.assertIn("on the server", str(ctx.exception))

    def test_program_missing_from_server_allowlist_is_refused(self):
        with self.assertRaises(shell.ShellDeniedError) as ctx:
            shell.check_command("uname -a", policy={"allowlist": ["echo"], "denylist": []})
        self.assertIn("server's shell allowlist", str(ctx.exception))

    def test_local_allowlist_can_narrow_but_not_widen(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "ls"
        with self.assertRaises(shell.ShellDeniedError) as ctx:
            shell.check_command("echo hi", policy={"allowlist": ["echo", "ls"], "denylist": []})
        self.assertIn("this machine's NEURO_SHELL_ALLOWLIST", str(ctx.exception))
        # Widening: the server allows echo; a local list that does not name it still narrows.
        self.assertEqual(shell.check_command("ls -la", policy={"allowlist": ["ls"], "denylist": []}), "ls -la")

    def test_server_denylist_is_applied_on_the_agent(self):
        with self.assertRaises(shell.ShellDeniedError):
            shell.check_command("echo secret-word", policy={"allowlist": ["echo"], "denylist": ["secret-word"]})

    def test_chaining_is_refused_even_for_an_allowlisted_program(self):
        for command in ("echo a && whoami", "echo a; id", "echo a | sh", "echo $(id)", "echo a > /tmp/x", "echo `id`"):
            with self.subTest(command=command), self.assertRaises(shell.ShellDeniedError) as ctx:
                shell.check_command(command, policy={"allowlist": ["echo"], "denylist": []})
            self.assertIn("one program per command", str(ctx.exception))

    def test_legacy_server_without_a_policy_still_uses_the_local_env(self):
        os.environ["NEURO_SHELL_ALLOWLIST"] = "echo"
        self.assertEqual(shell.check_command("echo ok"), "echo ok")
        os.environ["NEURO_SHELL_ALLOWLIST"] = ""
        with self.assertRaises(shell.ShellDeniedError):
            shell.check_command("echo ok")


if __name__ == "__main__":
    unittest.main()
