from neuro_api.command import Action

def schema():
    return Action(
        "press",
        "Press a key",
        {
            "type": "object",
            "properties": {
                "key": {"type": "string"}
            },
            "required": ["key"]
        }
    )
