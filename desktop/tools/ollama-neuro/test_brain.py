"""Unit tests that do not require a running Ollama daemon."""

from __future__ import annotations

import unittest

from ollama_brain import OllamaBrain


class OllamaBrainParseTests(unittest.TestCase):
    def setUp(self):
        self.brain = OllamaBrain(model="heredos/rwkv7:2.9b")

    def test_parse_tool_calls(self):
        result = {
            "message": {
                "tool_calls": [
                    {
                        "function": {
                            "name": "type_text",
                            "arguments": {"text": "hi", "execute_now": True},
                        }
                    }
                ]
            }
        }
        choice = self.brain._parse_tool_response(result, {"type_text", "mouse_click"})
        self.assertIsNotNone(choice)
        assert choice is not None
        self.assertEqual(choice.name, "type_text")
        self.assertEqual(choice.data["text"], "hi")

    def test_parse_json_in_content(self):
        choice = self.brain._parse_json_choice(
            'Sure!\n{"name":"mouse_click","data":{"button":"left"},"reason":"click"}\n',
            {"mouse_click"},
            reason="test",
        )
        self.assertIsNotNone(choice)
        assert choice is not None
        self.assertEqual(choice.name, "mouse_click")
        self.assertEqual(choice.data["button"], "left")

    def test_rejects_unknown_action(self):
        choice = self.brain._parse_json_choice(
            '{"name":"rm_rf","data":null}',
            {"type_text"},
            reason="test",
        )
        self.assertIsNone(choice)


if __name__ == "__main__":
    unittest.main()
