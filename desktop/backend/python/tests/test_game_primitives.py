"""Cross-language protocol tests for the game-input primitives.

The Go bridge sends these scripts (and the Rust executor calls the same methods
through pyo3), so a renamed method or a changed argument order here silently
breaks game control. These tests pin the names and shapes the other two
languages depend on.
"""

import pathlib
import sys
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller.actions import ActionParseError, ActionParser  # noqa: E402


class FakeMonitor:
    def __init__(self):
        self.events = []

    def record_action(self, source, action_type, data):
        self.events.append((source, action_type, data))


class FakeKeyboard:
    def __init__(self):
        self.calls = []
        self.valid_keys = {"w", "a", "s", "d", "shift", "ctrl", "space", "e", "esc", "q"}

    def _validate(self, key):
        if key not in self.valid_keys:
            raise ValueError(f"Unsupported key: {key}")

    # --- API used by the executor ----------------------------------------
    def type(self, text):
        self.calls.append(("type", text))

    def press(self, key):
        self._validate(key)
        self.calls.append(("press", key))

    def hold(self, key):
        self._validate(key)
        self.calls.append(("hold", key))

    def release(self, key):
        self._validate(key)
        self.calls.append(("release", key))

    def shortcut(self, *keys):
        for key in keys:
            self._validate(key)
        self.calls.append(("shortcut", keys))

    def hold_for(self, key, seconds):
        self._validate(key)
        self.calls.append(("hold_for", key, seconds))

    def combo(self, *keys):
        self.shortcut(*keys)

    def release_all(self):
        self.calls.append(("release_all",))

    def wait(self, seconds):
        self.calls.append(("wait", seconds))

    def enter(self):
        self.calls.append(("enter",))


class FakeMouse:
    def __init__(self):
        self.calls = []

    def queue_move(self, x, y, duration=0.1):
        self.calls.append(("move", x, y, duration))

    def queue_move_rel(self, dx, dy, duration=0.0):
        self.calls.append(("move_rel", dx, dy, duration))

    def queue_click(self, button="left"):
        self.calls.append(("click", button))

    def queue_hold(self, button="left", seconds=0.2):
        self.calls.append(("hold", button, seconds))

    def release_all(self):
        self.calls.append(("release_all",))

    def queue_wait(self, seconds):
        self.calls.append(("wait", seconds))

    def queue_path(self, path):
        self.calls.append(("path", path))

    def map_normalized(self, nx, ny):
        return int(nx * 100), int(ny * 100)

    def draw_line(self, start, end, steps=50):
        return [start, end]

    def draw_polyline(self, points, steps_per_segment=30):
        return list(points)


def build_parser():
    keyboard = FakeKeyboard()
    mouse = FakeMouse()
    monitor = FakeMonitor()
    return ActionParser(keyboard, mouse, monitor, platform="linux"), keyboard, mouse, monitor


class GamePrimitiveTests(unittest.TestCase):
    def test_hold_for_moves_a_key_for_a_bounded_time(self):
        parser, keyboard, _, _ = build_parser()

        parser.parse("HOLD_FOR w 1.5")
        parser.execute()
        parser.clear()

        self.assertEqual(keyboard.calls, [("hold_for", "w", 1.5)])

    def test_hold_for_rejects_unbounded_or_negative_times(self):
        parser, keyboard, _, _ = build_parser()

        for bad in ("HOLD_FOR w 60", "HOLD_FOR w -1", "HOLD_FOR w 0"):
            with self.assertRaises(ActionParseError):
                parser.parse(bad)
                parser.execute()
                parser.clear()

        self.assertEqual(keyboard.calls, [])

    def test_combo_presses_keys_together(self):
        parser, keyboard, _, _ = build_parser()

        parser.parse("COMBO ctrl shift s")
        parser.execute()
        parser.clear()

        self.assertEqual(keyboard.calls, [("shortcut", ("ctrl", "shift", "s"))])

    def test_move_rel_queues_a_relative_mouse_move(self):
        parser, _, mouse, _ = build_parser()

        parser.parse("MOVE_REL 40 -15 0.2")
        parser.execute()
        parser.clear()

        self.assertEqual(mouse.calls, [("move_rel", 40, -15, 0.2)])

    def test_move_rel_defaults_to_no_animation(self):
        parser, _, mouse, _ = build_parser()

        parser.parse("MOVE_REL 12 3")
        parser.execute()
        parser.clear()

        self.assertEqual(mouse.calls, [("move_rel", 12, 3, 0.0)])

    def test_move_rel_rejects_durations_the_protocol_does_not_allow(self):
        parser, _, mouse, _ = build_parser()

        with self.assertRaises(ActionParseError):
            parser.parse("MOVE_REL 10 10 5")
            parser.execute()
            parser.clear()

        self.assertEqual(mouse.calls, [])

    def test_release_all_clears_keyboard_and_mouse(self):
        parser, keyboard, mouse, _ = build_parser()

        parser.parse("RELEASE_ALL")
        parser.execute()
        parser.clear()

        self.assertEqual(keyboard.calls, [("release_all",)])
        self.assertEqual(mouse.calls, [("release_all",)])

    def test_launch_is_parsed_by_the_script_parser(self):
        # The bridge gates LAUNCH behind the system permission scope before a
        # script ever reaches the executor, so the parser itself only has to
        # recognise it. Verify it does not get mistaken for a key press.
        parser, keyboard, mouse, _ = build_parser()

        with self.assertRaises(ActionParseError):
            # A missing target is the parser's error, not "unknown command".
            parser.parse("LAUNCH")
            parser.execute()
            parser.clear()

        self.assertEqual(keyboard.calls, [])
        self.assertEqual(mouse.calls, [])

    def test_game_scripts_run_as_a_single_queue(self):
        parser, keyboard, mouse, _ = build_parser()

        parser.parse(
            "\n".join(
                [
                    "# walk forward while looking right",
                    "HOLD_FOR w 1.0",
                    "MOVE_REL 60 0 0.4",
                    "RELEASE_ALL",
                ]
            )
        )
        parser.execute()
        parser.clear()

        self.assertEqual(keyboard.calls, [("hold_for", "w", 1.0), ("release_all",)])
        self.assertEqual(mouse.calls, [("move_rel", 60, 0, 0.4), ("release_all",)])

    def test_unknown_command_is_still_rejected(self):
        parser, _, _, _ = build_parser()

        with self.assertRaises(ActionParseError):
            parser.parse("TELEPORT 1 2")
            parser.execute()
            parser.clear()

    def test_monitor_records_every_primitive(self):
        parser, _, _, monitor = build_parser()

        parser.parse("HOLD_FOR w 0.5\nCOMBO ctrl s\nRELEASE_ALL")
        parser.execute()
        parser.clear()

        kinds = [event[1] for event in monitor.events]
        self.assertEqual(kinds, ["HOLD_FOR", "COMBO", "RELEASE_ALL"])


if __name__ == "__main__":
    unittest.main()
