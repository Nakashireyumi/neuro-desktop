"""
Action: refresh_context
Force a full context refresh with custom settings
"""
from neuro_api.command import Action

def schema():
    return Action(
        "refresh_context",
        "Request an immediate context refresh with custom detail level. Use this when you need updated information or want more/less detail. Parameters: detail_level (string: minimal/standard/detailed/full, default standard), include_ocr (bool, default true), include_vision (bool, default false), max_items_per_category (int 5-500, default 15).",
        {
            "type": "object",
            "properties": {
                "detail_level": {
                    "type": "string",
                    "enum": ["minimal", "standard", "detailed", "full"]
                },
                "include_ocr": {
                    "type": "boolean"
                },
                "include_vision": {
                    "type": "boolean"
                },
                "max_items_per_category": {
                    "type": "integer",
                    "minimum": 5,
                    "maximum": 500
                }
            },
            "required": []
        }
    )
