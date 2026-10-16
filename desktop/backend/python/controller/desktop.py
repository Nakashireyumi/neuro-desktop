import io
import time
import threading
from typing import List, Tuple, Optional, Dict, Any
import os
import shutil
import subprocess
import sys

try:
    import psutil
except ImportError:  # pragma: no cover - minimal/CLI-only installs
    psutil = None

from .gui_stub import (
    NoDisplayError,
    is_headless,
    load_mss,
    load_pyautogui,
    load_pygetwindow,
    load_pynput_mouse,
)

# Each of these is a real library on a desktop session and a graceful stand-in
# (or None) on a machine without one. Importing them must never crash: a
# command-line-only box still runs the shell capability and reports status.
pyautogui = load_pyautogui()
pynput_mouse = load_pynput_mouse()
mss = load_mss()
gw = load_pygetwindow()

try:
    from PIL import Image
except ImportError:  # pragma: no cover - pillow is a hard dependency of the executor
    Image = None

# A screenshot is sent to the server as PNG inside one JSON line. These bounds keep
# that line well under the executor's 8 MiB limit, even for a large or busy screen.
SCREENSHOT_MAX_SIDE = 1600
SCREENSHOT_MAX_BYTES = 3 * 1024 * 1024


def _fit_within(image, max_side: int):
    """Scale an image down so its longest side is at most max_side pixels."""
    width, height = image.size
    longest = max(width, height)
    if longest <= max_side:
        return image
    scale = max_side / float(longest)
    return image.resize((max(1, round(width * scale)), max(1, round(height * scale))))


def _encode_png(image) -> bytes:
    buffer = io.BytesIO()
    image.save(buffer, format="PNG")
    return buffer.getvalue()


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
        self.headless = headless
        self.track_mouse = track_mouse and not headless and pynput_mouse is not None
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

        self._mouse_listener = pynput_mouse.Listener(on_move=on_move)
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

    def capture_screen(self) -> "Image.Image":
        if self.headless:
            raise NoDisplayError(
                "Screenshot unavailable: this machine has no display session, so there is "
                "nothing to capture. Use shell_command or desktop context instead."
            )
        if Image is None:
            raise NoDisplayError("Screenshot unavailable: Pillow is not installed on this machine.")
        # XGetImage cannot capture native Wayland windows. On wlroots desktops
        # (including Hyprland), grim captures through the compositor instead.
        if sys.platform.startswith("linux") and os.environ.get("WAYLAND_DISPLAY"):
            grim = shutil.which("grim")
            if grim:
                try:
                    result = subprocess.run(
                        [grim, "-t", "png", "-"], stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, timeout=5, check=True,
                    )
                    with Image.open(io.BytesIO(result.stdout)) as image:
                        return image.convert("RGB")
                except (OSError, subprocess.SubprocessError, ValueError) as exc:
                    raise NoDisplayError(
                        "Wayland screenshot failed. Check that grim can access this compositor: " + str(exc)
                    ) from exc
        if mss is None:
            raise NoDisplayError(
                "Screenshot unavailable: install mss for X11/Windows/macOS, or grim for a supported Wayland compositor."
            )
        with mss.mss() as sct:
            monitor = sct.monitors[1]
            try:
                screenshot = sct.grab(monitor)
            except Exception as exc:
                if os.environ.get("WAYLAND_DISPLAY"):
                    raise NoDisplayError("Screenshot capture failed under Wayland. Install grim on a supported compositor.") from exc
                raise
            return Image.frombytes("RGB", screenshot.size, screenshot.rgb)

    def capture_screen_png(
        self, max_side: int = SCREENSHOT_MAX_SIDE, max_bytes: int = SCREENSHOT_MAX_BYTES
    ) -> bytes:
        """Capture the screen as PNG bytes, scaled to fit the executor's line limit.

        The bytes are returned in the status reply. They are never written to a
        path that the server could then open.
        """
        if Image is None:
            raise NoDisplayError("Screenshot unavailable: Pillow is not installed on this machine.")
        image = _fit_within(self.capture_screen(), max_side)
        data = _encode_png(image)
        while len(data) > max_bytes and max(image.size) > 320:
            image = _fit_within(image, int(max(image.size) * 0.75))
            data = _encode_png(image)
        return data

    def get_platform(self) -> str:
        if sys.platform.startswith("win"):
            return "windows"
        if sys.platform == "darwin":
            return "macos"
        return "linux"

    def get_running_processes(self) -> List[str]:
        if psutil is not None:
            return [p.name() for p in psutil.process_iter(attrs=["name"])]

        # Minimal installs (a CLI-only image with stdlib only): read /proc where
        # that exists, otherwise report nothing rather than failing the status call.
        if sys.platform.startswith("linux"):
            names: List[str] = []
            try:
                for entry in os.listdir("/proc"):
                    if not entry.isdigit():
                        continue
                    try:
                        with open(f"/proc/{entry}/comm", "r", encoding="utf-8") as handle:
                            names.append(handle.read().strip())
                    except OSError:
                        continue
            except OSError:
                return []
            return [name for name in names if name]

        return []

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
