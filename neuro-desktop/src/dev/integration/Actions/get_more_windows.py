"""
Action: get_more_windows
Request complete list of all windows when truncated
"""
from neuro_api.command import Action

def schema():
    return Action(
        "get_more_windows",
        "Request complete list of all visible windows. Use this when the context shows '... and X more windows'. Parameters: offset (int, skip N windows), limit (int, max 1-50, default 20), include_minimized (bool, default false).",
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
                    "maximum": 50
                },
                "include_minimized": {
                    "type": "boolean"
                }
            },
            "required": []
        }
    )
