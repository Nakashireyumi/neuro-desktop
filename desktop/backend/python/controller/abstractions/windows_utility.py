"""Windows-only fullscreen window helpers (win32gui).

On non-Windows platforms this module imports as a stub so the package
loads; calling FSWUtils methods raises RuntimeError.
"""

from __future__ import annotations

import random
import sys
import time

import pyautogui

from ..libraries.mouse_pathfinder import AlgorithmicPath

_IS_WINDOWS = sys.platform.startswith("win")

if _IS_WINDOWS:
    import win32con
    import win32gui

    from ..libraries.window_info import WindowInfo
else:
    win32con = None  # type: ignore
    win32gui = None  # type: ignore
    WindowInfo = object  # type: ignore


def _require_windows() -> None:
    if not _IS_WINDOWS:
        raise RuntimeError("FSWUtils requires Windows (win32gui)")


# FSW = Fullscreen Window
class FSWUtils:
    """Fullscreen window helpers.

    Static methods use pyautogui (fragile hardcoded coords — Windows chrome).
    Instance methods use win32gui when available.
    """

    pathfinder = AlgorithmicPath()

    @staticmethod
    def minimize():
        """Minimize the current fullscreen window (title-bar click heuristic)."""
        _require_windows()
        FSWUtils.pathfinder.move_to(1800 + random.randint(-3, 4), 50, 0.05)
        time.sleep(random.uniform(0.15, 0.4))
        pyautogui.click(button="left")

    @staticmethod
    def close(confirm=False):
        """Close the current fullscreen window (title-bar click heuristic)."""
        _require_windows()
        FSWUtils.pathfinder.move_to(1900, 50 + random.randint(-3, 2), 0.05)
        time.sleep(random.uniform(0.15, 0.4))
        if confirm:
            pyautogui.click(button="left")

    @staticmethod
    def unfullscreen():
        """Leave fullscreen via title-bar click heuristic."""
        _require_windows()
        FSWUtils.pathfinder.move_to(
            1850 + random.randint(-3, 3), 50 + random.randint(-2, 2), duration=0.05
        )
        time.sleep(random.uniform(0.15, 0.4))
        pyautogui.click(button="left")

    def __init__(self, window: "WindowInfo"):
        _require_windows()
        self.window = window

    def maximize(self):
        """Maximize a given window."""
        win32gui.ShowWindow(self.window.hwnd, win32con.SW_MAXIMIZE)

    def restore(self):
        """Restore the given window."""
        win32gui.ShowWindow(self.window.hwnd, win32con.SW_RESTORE)

    def close_window(self):
        """Close the given window via WM_CLOSE."""
        win32gui.PostMessage(self.window.hwnd, win32con.WM_CLOSE, 0, 0)
