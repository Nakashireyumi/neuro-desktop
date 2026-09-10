import time
import threading
from typing import List, Tuple, Optional, Dict, Any
import os
import sys

import pyautogui
import psutil
from pynput import mouse
import mss
from PIL import Image

try:
    import pygetwindow as gw
except ImportError:  # pragma: no cover - optional dependency
    gw = None


class DesktopMonitor:
    """
    Gathers high-level information about desktop activity AND action history.
    """

    def __init__(
        self,
        track_mouse: bool = True,
        max_mouse_history: int = 500,
        max_action_history: int = 1000,
        headless: bool = False,
    ):
        self.track_mouse = track_mouse and not headless
        self.headless = headless
        self.max_mouse_history = max_mouse_history
        self.max_action_history = max_action_history

        self.mouse_history: List[Tuple[float, int, int]] = []
        self.last_mouse_position: Optional[Tuple[int, int]] = None
        self.last_mouse_move_time: Optional[float] = None

        self.action_history: List[Dict[str, Any]] = []

        self._mouse_listener = None
        self._lock = threading.Lock()

        if self.track_mouse:
            try:
                self._start_mouse_listener()
            except Exception:
                # Display auth failures must not crash process startup.
                self.track_mouse = False

    def record_action(
        self,
        source: str,
        action_type: str,
        data: Optional[Dict[str, Any]] = None,
    ):
        """Record a structured action event."""
        event = {
            "time": time.time(),
            "source": source,
            "type": action_type,
            "data": data or {},
        }

        with self._lock:
            self.action_history.append(event)
            if len(self.action_history) > self.max_action_history:
                self.action_history.pop(0)

    def get_action_history(self) -> List[Dict[str, Any]]:
        with self._lock:
            return list(self.action_history)

    def clear_action_history(self):
        with self._lock:
            self.action_history.clear()

    def _start_mouse_listener(self):
        def on_move(x, y):
            with self._lock:
                now = time.time()
                self.last_mouse_position = (x, y)
                self.last_mouse_move_time = now

                self.mouse_history.append((now, x, y))
                if len(self.mouse_history) > self.max_mouse_history:
                    self.mouse_history.pop(0)

        self._mouse_listener = mouse.Listener(on_move=on_move)
        self._mouse_listener.daemon = True
        self._mouse_listener.start()

    def get_current_mouse_position(self) -> Tuple[int, int]:
        if self.headless:
            return (0, 0)
        try:
            return pyautogui.position()
        except Exception as exc:
            raise RuntimeError(f"Cannot read mouse position (display unavailable): {exc}") from exc

    def get_last_mouse_position(self) -> Optional[Tuple[int, int]]:
        return self.last_mouse_position

    def get_last_mouse_move_time(self) -> Optional[float]:
        return self.last_mouse_move_time

    def get_mouse_history(self) -> List[Tuple[float, int, int]]:
        with self._lock:
            return list(self.mouse_history)

    def get_open_windows(self) -> List[str]:
        if gw is None or self.headless:
            return []
        try:
            windows = gw.getAllWindows()
            return [w.title for w in windows if w.title]
        except Exception:
            return []

    def get_active_window(self) -> Optional[str]:
        if gw is None or self.headless:
            return None
        try:
            win = gw.getActiveWindow()
            return win.title if win else None
        except Exception:
            return None

    def get_screen_size(self) -> Tuple[int, int]:
        if self.headless:
            return (1920, 1080)
        try:
            return pyautogui.size()
        except Exception as exc:
            raise RuntimeError(f"Cannot read screen size (display unavailable): {exc}") from exc

    def capture_screen(self) -> Image.Image:
        if self.headless:
            raise RuntimeError("Screenshot unavailable in headless mode")
        with mss.mss() as sct:
            monitor = sct.monitors[1]
            screenshot = sct.grab(monitor)
            return Image.frombytes("RGB", screenshot.size, screenshot.rgb)

    def capture_screen_to_file(self, path: str) -> str:
        image = self.capture_screen()
        directory = os.path.dirname(path)
        if directory:
            os.makedirs(directory, exist_ok=True)
        image.save(path)
        return path

    def get_platform(self) -> str:
        if sys.platform.startswith("win"):
            return "windows"
        if sys.platform == "darwin":
            return "macos"
        return "linux"

    def get_running_processes(self) -> List[str]:
        return [p.name() for p in psutil.process_iter(attrs=["name"])]

    def shutdown(self):
        if self._mouse_listener:
            self._mouse_listener.stop()


if __name__ == "__main__":
    monitor = DesktopMonitor()

    time.sleep(2)

    print("Platform:", monitor.get_platform())
    print("Mouse:", monitor.get_current_mouse_position())
    print("Last move time:", monitor.get_last_mouse_move_time())
    print("Open windows:", monitor.get_open_windows())
    print("Active window:", monitor.get_active_window())

    img = monitor.capture_screen()
    img.save("screen_sample.png")

    monitor.shutdown()
