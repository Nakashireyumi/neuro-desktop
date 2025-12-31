from neuro_api.command import Action

def schema():
    return Action(
        "screenshot",
        "Take a screenshot of the screen or a specific region",
        {
            "type": "object",
            "properties": {
                "name": {"type": "string"},
                "region": {
                    "type": "array",
                    "items": {"type": "integer"},
                    "minItems": 4,
                    "maxItems": 4
                }
            }
        }
    )