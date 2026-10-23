import win32gui
from dataclasses import dataclass

@dataclass
class Position2D:
    x: int
    y: int

@dataclass
class WindowInfo:
    width: int
    height: int

    position: Position2D
    hwnd: int

class WindowInfoUtil:
    """## Window Information Utility
    
    Is a class that provides information about the provided window.
    """
    def __init__(self, hwnd = win32gui.GetForegroundWindow()):
        self.hwnd = hwnd

    def get_window_size(self):
        """Returns the width and height of the window"""
        left, top, right, bottom = win32gui.GetWindowRect(self.hwnd)

        width = right - left
        height = bottom - top

        return width, height

    def get_window_pos(self):
        """Returns the x and y position of the window"""
        left, top, right, bottom = win32gui.GetWindowRect(self.hwnd)
        return left, top
    
    def get_window_info(self):
        return WindowInfo(width=self.get_window_size()[0], height=self.get_window_size()[1], position=self.get_window_pos(), hwnd=self.hwnd)
