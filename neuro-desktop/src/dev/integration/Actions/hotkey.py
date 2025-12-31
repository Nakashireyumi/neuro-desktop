from neuro_api.command import Action

def schema():
    return Action(
        "hotkey",
        "Press a combination of keys simultaneously (like keyboard shortcuts)",
        {
            "type": "object",
            "properties": {
                "keys": {
                    "type": "array",
                    "items": {"type": "string"},
                    "minItems": 1
                }
            },
            "required": ["keys"]
        }
    )