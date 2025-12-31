from .controls.mouse import MouseController
from .controls.keyboard import KeyboardController

from .actions import ActionParser
from .desktop import DesktopMonitor

def initialize_driver():
    monitor = DesktopMonitor()
    mouse = MouseController(monitor)
    keyboard = KeyboardController(monitor)
    parser = ActionParser(keyboard, mouse, monitor)
    return monitor, mouse, keyboard, parser