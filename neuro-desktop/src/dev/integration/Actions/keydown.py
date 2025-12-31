from neuro_api.command import Action

def schema():
    return Action(
        "keydown",
        "Hold a key down",
        {
            "type": "object",
            "properties": {
                "key": {"type": "string"}
            },
            "required": ["key"]
        }
    )
