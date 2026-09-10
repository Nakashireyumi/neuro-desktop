import pathlib
import sys
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller.actions import ActionParseError, ActionParser  # noqa: E402
from controller.platform_intents import apply_intent, resolve_intent  # noqa: E402


class FakeMonitor:
    def __init__(self):
        self.events = []

    def record_action(self, source, action_type, data):
        self.events.append((source, action_type, data))


class FakeKeyboard:
    def __init__(self):
        self.calls = []
        self.valid_keys = {
            "win",
            "command",
            "d",
            "m",
            "i",
            "a",
            "v",
            "l",
            "x",
            "n",
            "q",
            "alt",
            "f2",
            "f3",
            "f4",
            "tab",
            "ctrl",
            "shift",
            "esc",
            "e",
            "r",
            "s",
            "right",
            "left",
            "middle",
            "space",
            "4",
            ",",
            "h",
        }

    def _validate_key(self, key):
        if key not in self.valid_keys:
            raise ValueError(f"Unsupported key: {key}")

    def type(self, text):
        self.calls.append(("type", text))

    def enter(self):
        self.calls.append(("enter",))

    def press(self, key):
        self._validate_key(key)
        self.calls.append(("press", key))

    def hold(self, key):
        self._validate_key(key)
        self.calls.append(("hold", key))

    def release(self, key):
        self._validate_key(key)
        self.calls.append(("release", key))

    def shortcut(self, *keys):
        for key in keys:
            self._validate_key(key)
        self.calls.append(("shortcut", keys))

    def wait(self, seconds):
        self.calls.append(("wait", seconds))


class FakeMouse:
    def __init__(self):
        self.calls = []

    def queue_move(self, x, y, duration=0.1):
        self.calls.append(("queue_move", x, y, duration))

    def map_normalized(self, nx, ny):
        return int(nx * 1000), int(ny * 1000)

    def queue_click(self, button="left"):
        self.calls.append(("queue_click", button))

    def draw_line(self, start, end, steps):
        return [start, end]

    def queue_path(self, points):
        self.calls.append(("queue_path", points))

    def draw_polyline(self, points):
        return points

    def queue_wait(self, seconds):
        self.calls.append(("queue_wait", seconds))


class ActionParserTests(unittest.TestCase):
    def setUp(self):
        self.monitor = FakeMonitor()
        self.kbd = FakeKeyboard()
        self.mouse = FakeMouse()
        self.parser = ActionParser(
            self.kbd, self.mouse, self.monitor, platform="windows"
        )

    def test_high_level_commands_windows(self):
        self.parser.parse(
            "\n".join(
                [
                    "OPEN_WINDOWS_MENU",
                    "SHOW_DESKTOP",
                    "MINIMIZE_ALL_WINDOWS",
                    "CLOSE_FOREGROUND_APP",
                    "OPEN_TASK_MANAGER",
                    "CLOSE_ALL_APPS",
                    "OPEN_FILE_EXPLORER",
                    "OPEN_RUN_DIALOG",
                    "OPEN_SEARCH",
                    "SNAP_WINDOW_LEFT",
                    "SNAP_WINDOW_RIGHT",
                    "OPEN_WINDOWS_SETTINGS",
                    "OPEN_NOTIFICATION_CENTER",
                    "OPEN_CLIPBOARD_HISTORY",
                    "LOCK_WORKSTATION",
                    "SWITCH_APP_NEXT",
                    "SWITCH_APP_PREVIOUS",
                    "OPEN_POWER_USER_MENU",
                    "TAKE_SCREEN_SNIP",
                ]
            )
        )

        self.assertEqual(
            self.kbd.calls,
            [
                ("press", "win"),
                ("shortcut", ("win", "d")),
                ("shortcut", ("win", "m")),
                ("shortcut", ("alt", "f4")),
                ("shortcut", ("ctrl", "shift", "esc")),
                ("shortcut", ("win", "d")),
                ("shortcut", ("win", "e")),
                ("shortcut", ("win", "r")),
                ("shortcut", ("win", "s")),
                ("shortcut", ("win", "left")),
                ("shortcut", ("win", "right")),
                ("shortcut", ("win", "i")),
                ("shortcut", ("win", "a")),
                ("shortcut", ("win", "v")),
                ("shortcut", ("win", "l")),
                ("shortcut", ("alt", "tab")),
                ("shortcut", ("alt", "shift", "tab")),
                ("shortcut", ("win", "x")),
                ("shortcut", ("win", "shift", "s")),
            ],
        )

    def test_linux_run_dialog_uses_alt_f2(self):
        parser = ActionParser(self.kbd, self.mouse, self.monitor, platform="linux")
        parser.parse("OPEN_RUN_DIALOG")
        self.assertEqual(self.kbd.calls, [("shortcut", ("alt", "f2"))])

    def test_macos_close_app_uses_cmd_q(self):
        parser = ActionParser(self.kbd, self.mouse, self.monitor, platform="macos")
        parser.parse("CLOSE_FOREGROUND_APP")
        self.assertEqual(self.kbd.calls, [("shortcut", ("command", "q"))])

    def test_macos_clipboard_history_unsupported(self):
        parser = ActionParser(self.kbd, self.mouse, self.monitor, platform="macos")
        with self.assertRaises(ActionParseError):
            parser.parse("OPEN_CLIPBOARD_HISTORY")

    def test_click_syntax(self):
        self.parser.parse("CLICK")
        self.parser.parse("CLICK right")

        self.assertEqual(
            self.mouse.calls,
            [
                ("queue_click", "left"),
                ("queue_click", "right"),
            ],
        )

    def test_click_rejects_coordinates(self):
        with self.assertRaises(ActionParseError):
            self.parser.parse("CLICK 500 400")

    def test_invalid_key_reports_error(self):
        with self.assertRaises(ActionParseError):
            self.parser.parse("PRESS definitely_not_a_key")


class PlatformIntentUnitTests(unittest.TestCase):
    def test_resolve_windows_show_desktop(self):
        intent = resolve_intent("SHOW_DESKTOP", platform="windows")
        self.assertEqual(intent.keys, ("win", "d"))

    def test_apply_unsupported_raises(self):
        kbd = FakeKeyboard()
        with self.assertRaises(ValueError):
            apply_intent(kbd, "OPEN_CLIPBOARD_HISTORY", platform="macos")


if __name__ == "__main__":
    unittest.main()
