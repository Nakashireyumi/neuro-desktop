import os

from .controls.mouse import MouseController
from .controls.keyboard import KeyboardController

from .actions import ActionParser
from .desktop import DesktopMonitor


def _headless() -> bool:
    return os.environ.get("NEURO_HEADLESS", "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def initialize_driver():
    """Create drivers. In headless/CI, skip live display listeners."""
    headless = _headless()
    monitor = DesktopMonitor(track_mouse=not headless, headless=headless)
    mouse = MouseController(monitor, headless=headless)
    keyboard = KeyboardController(monitor)
    parser = ActionParser(keyboard, mouse, monitor)
    return monitor, mouse, keyboard, parser
