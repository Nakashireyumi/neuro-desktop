"""
Action: get_more_text
Request more detected text items from the screen when context is truncated
"""
from neuro_api.command import Action

def schema():
    return Action(
        "get_more_text",
        "Request more detected text items from the screen. Use this when the context shows '... and X more items'. Parameters: offset (int, skip N items), limit (int, max items 1-100, default 50), filter_type (string: all/buttons/links/text/inputs, default all).",
        {
            "type": "object",
            "properties": {
                "offset": {
                    "type": "integer",
                    "minimum": 0
                },
                "limit": {
                    "type": "integer",
                    "minimum": 1,
                    "maximum": 100
                },
                "filter_type": {
                    "type": "string",
                    "enum": ["all", "buttons", "links", "text", "inputs"]
                }
            },
            "required": []
        }
    )
