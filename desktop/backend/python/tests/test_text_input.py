"""Text typing refuses what the backend cannot reproduce exactly.

PyAutoGUI's character map is not a Unicode input method. Silently dropping a
character can turn a typed command or password into a different one, so the
controller validates the whole string before queueing anything and never falls
back to replacing the clipboard.
"""

import pathlib
import sys
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller import agent as agent_module  # noqa: E402
from controller.actions import ActionParseError, ActionParser  # noqa: E402
from controller.controls import keyboard, mouse  # noqa: E402


class Monitor:
    def __init__(self):
        self.events = []

    def record_action(self, **kwargs):
        self.events.append(kwargs)


class TextValidationTest(unittest.TestCase):
    def setUp(self):
        self.monitor = Monitor()
        self.kbd = keyboard.KeyboardController(self.monitor)

    def test_printable_ascii_is_queued(self):
        self.kbd.type("hello, World 123!")
        self.assertEqual(len(self.kbd.queue), 1)
        self.assertEqual(len(self.monitor.events), 1)

    def test_non_ascii_is_refused_before_queueing(self):
        for text in ("café", "こんにちは", "hello 😀", "naïve"):
            with self.subTest(text=text):
                self.kbd.clear()
                self.monitor.events.clear()
                with self.assertRaises(ValueError) as raised:
                    self.kbd.type(text)
                self.assertIn("printable ASCII", str(raised.exception))
                self.assertIn("Nothing was typed", str(raised.exception))
                self.assertIn("clipboard", str(raised.exception))
                self.assertEqual(self.kbd.queue, [])
                self.assertEqual(self.monitor.events, [])

    def test_control_characters_are_refused(self):
        for text in ("line\nbreak", "tab\there", "null\x00byte"):
            with self.subTest(text=repr(text)):
                with self.assertRaises(ValueError):
                    self.kbd.type(text)

    def test_action_parser_rejects_the_whole_script_at_plan_time(self):
        parser = ActionParser(
            self.kbd, mouse.MouseController(self.monitor, headless=True), self.monitor,
            platform="linux",
        )
        with self.assertRaises(ActionParseError) as raised:
            parser.parse('WAIT 0.1\nTYPE "café"')
        self.assertIn("printable ASCII", str(raised.exception))
        self.assertEqual(parser.queue, [])

    def test_agent_type_text_does_not_leave_a_partial_queue(self):
        agent = agent_module.Agent()
        self.addCleanup(agent.close)
        result = agent.execute(
            {"type": "type_text", "params": {"text": "café"}, "execute_now": False}
        )
        self.assertFalse(result["success"])
        self.assertIn("printable ASCII", result["error"])
        self.assertEqual(agent.keyboard.queue, [])

    def test_writer_is_not_called_for_refused_text(self):
        backend = mock.Mock()
        with mock.patch.object(keyboard, "pyautogui", backend):
            with self.assertRaises(ValueError):
                self.kbd.type("café")
        backend.write.assert_not_called()


if __name__ == "__main__":
    unittest.main()
