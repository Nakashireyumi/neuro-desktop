from neuro_api.command import Action
import pyautogui

def schema():
    screen_width, screen_height = pyautogui.size()
    return Action(
        "click",
        f"If coordinates for the mouse is provided, move the mouse to that area, then click. Screen dimensions are {screen_width}x{screen_height}. X must be between 0 and {screen_width-1}, Y must be between 0 and {screen_height-1}.",
        {
            "type": "object",
            "properties": {
                "x": {"type": "integer", "minimum": 0, "maximum": screen_width - 1},
                "y": {"type": "integer", "minimum": 0, "maximum": screen_height - 1},
                "button": {"type": "string", "enum": ["left", "right", "middle"]}
            },
            "required": ["button"]
        }
    )
