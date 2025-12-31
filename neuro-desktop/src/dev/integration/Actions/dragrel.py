from neuro_api.command import Action

def schema():
    return Action(
        "dragrel",
        "Drag by relative offset from current mouse position",
        {
            "type": "object",
            "properties": {
                "dx": {"type": "integer"},
                "dy": {"type": "integer"},
                "duration": {
                    "type": "number", 
                    "minimum": 0
                },
                "button": {
                    "type": "string", 
                    "enum": ["left", "right", "middle"]
                }
            },
            "required": ["dx", "dy"]
        }
    )