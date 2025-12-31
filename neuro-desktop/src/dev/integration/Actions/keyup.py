from neuro_api.command import Action

def schema():
    return Action(
        "keyup",
        "Release a key that was previously pressed down",
        {
            "type": "object",
            "properties": {
                "key": {"type": "string"}
            },
            "required": ["key"]
        }
    )