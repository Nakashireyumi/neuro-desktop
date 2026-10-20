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

from . import capabilities
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


def _scale(pixels, logical):
    if not logical:
        return None
    return round(float(pixels) / float(logical), 6)


def _capture_geometry(
    source: str,
    origin_x: Optional[int],
    origin_y: Optional[int],
    logical_width: Optional[int],
    logical_height: Optional[int],
    pixel_width: int,
    pixel_height: int,
) -> Dict[str, Any]:
    """Describe one capture in both compositor/logical and image pixels.

    The controller downsizes screenshots before sending them. Reporting the
    original capture bounds, its pixel size and the encoded output size keeps a
    model from treating image pixels as pointer coordinates.
    """
    geometry: Dict[str, Any] = {
        "source": source,
        "origin": (
            {"x": int(origin_x), "y": int(origin_y)}
            if origin_x is not None and origin_y is not None
            else None
        ),
        "logical": (
            {"width": int(logical_width), "height": int(logical_height)}
            if logical_width is not None and logical_height is not None
            else None
        ),
        "pixel": {"width": int(pixel_width), "height": int(pixel_height)},
        "pixel_scale": {
            "x": _scale(pixel_width, logical_width),
            "y": _scale(pixel_height, logical_height),
        },
        "output": None,
        "output_scale": None,
    }
    return geometry


def _with_output_geometry(geometry: Dict[str, Any], size) -> Dict[str, Any]:
    geometry = dict(geometry)
    width, height = size
    logical = geometry.get("logical") or {}
    geometry["output"] = {"width": int(width), "height": int(height)}
    geometry["output_scale"] = {
        "x": _scale(width, logical.get("width")),
        "y": _scale(height, logical.get("height")),
    }
    return geometry


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
        self._last_capture_geometry: Optional[Dict[str, Any]] = None

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
        """List titled windows, or raise when the backend cannot do it.

        Returning an empty list on failure made unsupported enumeration look
        like a desktop with no windows. The agent publishes the reason next to
        the (empty) sample instead.
        """
        if gw is None or self.headless:
            return []
        try:
            windows = gw.getAllWindows()
            return [w.title for w in windows if w.title]
        except Exception as exc:
            raise RuntimeError(
                "Window enumeration failed; PyGetWindow may not support this "
                f"desktop session: {exc}"
            ) from exc

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

    @property
    def last_capture_geometry(self) -> Optional[Dict[str, Any]]:
        return self._last_capture_geometry

    def capture_geometry_hint(self) -> Optional[Dict[str, Any]]:
        """Best passive description of capture bounds before taking a screenshot.

        Pixel size and scale are only known for certain after a capture, so they
        stay ``None`` here instead of being guessed. The hint names the backend
        and the compositor-space origin when that can be read without grabbing.
        """
        if self.headless:
            return None
        if sys.platform.startswith("linux") and capabilities.wayland_session():
            if not shutil.which("grim"):
                return None
            return {
                "source": "grim",
                "origin": None,
                "logical": None,
                "pixel": None,
                "pixel_scale": None,
                "output": None,
                "output_scale": None,
                "note": (
                    "grim reports compositor bounds and pixel scale only when a "
                    "screenshot is captured; pointer coordinates remain separate."
                ),
            }
        if mss is None:
            return None
        try:
            with mss.mss() as sct:
                monitor = sct.monitors[1]
            return {
                "source": "mss",
                "origin": {
                    "x": int(monitor.get("left", 0)),
                    "y": int(monitor.get("top", 0)),
                },
                "logical": {
                    "width": int(monitor["width"]),
                    "height": int(monitor["height"]),
                },
                "pixel": None,
                "pixel_scale": None,
                "output": None,
                "output_scale": None,
                "note": (
                    "Logical capture bounds are known now; pixel dimensions and "
                    "scale are measured when a screenshot is captured."
                ),
            }
        except Exception:
            return None

    def capture_screen(self) -> "Image.Image":
        self._last_capture_geometry = None
        if self.headless:
            raise NoDisplayError(
                "Screenshot unavailable: this machine has no display session, so there is "
                "nothing to capture. Use shell_command or desktop context instead."
            )
        if Image is None:
            raise NoDisplayError("Screenshot unavailable: Pillow is not installed on this machine.")
        if sys.platform == "darwin":
            permitted = capabilities.probe_macos_screen_capture()
            if permitted is False:
                raise NoDisplayError(
                    "Screenshot unavailable: macOS Screen Recording permission is not "
                    "granted. Grant it in System Settings > Privacy & Security > Screen "
                    "Recording, restart the controller, then retry."
                )
        # XGetImage cannot capture native Wayland windows. On wlroots desktops
        # (including Hyprland), grim captures through the compositor instead.
        if sys.platform.startswith("linux") and capabilities.wayland_session():
            grim = shutil.which("grim")
            if not grim:
                raise NoDisplayError(
                    "Wayland screenshot unavailable: grim is not installed. MSS would "
                    "capture only XWayland windows, so that misleading fallback is "
                    "disabled. Install grim (or a compositor capture backend) and retry."
                )
            try:
                result = subprocess.run(
                    [grim, "-t", "png", "-"], stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE, timeout=5, check=True,
                )
                with Image.open(io.BytesIO(result.stdout)) as image:
                    image = image.convert("RGB")
                width, height = image.size
                self._last_capture_geometry = _capture_geometry(
                    "grim", None, None, width, height, width, height
                )
                self._last_capture_geometry["note"] = (
                    "grim did not report a compositor origin; logical size assumes "
                    "one output pixel per logical pixel."
                )
                return image
            except (OSError, subprocess.SubprocessError, ValueError) as exc:
                raise NoDisplayError(
                    "Wayland screenshot failed. Check that grim can access this compositor: " + str(exc)
                ) from exc
        if mss is None:
            raise NoDisplayError(
                "Screenshot unavailable: install mss for X11/Windows/macOS capture."
            )
        with mss.mss() as sct:
            monitor = sct.monitors[1]
            try:
                screenshot = sct.grab(monitor)
            except Exception as exc:
                raise NoDisplayError(
                    "Screenshot capture failed. Check display access on this machine: " + str(exc)
                ) from exc
            image = Image.frombytes("RGB", screenshot.size, screenshot.rgb)
            self._last_capture_geometry = _capture_geometry(
                "mss",
                monitor.get("left"),
                monitor.get("top"),
                monitor.get("width"),
                monitor.get("height"),
                image.size[0],
                image.size[1],
            )
            return image

    def capture_screen_png(
        self, max_side: int = SCREENSHOT_MAX_SIDE, max_bytes: int = SCREENSHOT_MAX_BYTES
    ) -> bytes:
        """Capture the screen as PNG bytes, scaled to fit the executor's line limit.

        The bytes are returned in the status reply. They are never written to a
        path that the server could then open. ``last_capture_geometry`` describes
        both the original capture and this encoded output.
        """
        data, geometry = self.capture_screen_png_with_geometry(
            max_side=max_side, max_bytes=max_bytes
        )
        self._last_capture_geometry = geometry
        return data

    def capture_screen_png_with_geometry(
        self, max_side: int = SCREENSHOT_MAX_SIDE, max_bytes: int = SCREENSHOT_MAX_BYTES
    ):
        if Image is None:
            raise NoDisplayError("Screenshot unavailable: Pillow is not installed on this machine.")
        image = self.capture_screen()
        geometry = self._last_capture_geometry
        image = _fit_within(image, max_side)
        data = _encode_png(image)
        while len(data) > max_bytes and max(image.size) > 320:
            image = _fit_within(image, int(max(image.size) * 0.75))
            data = _encode_png(image)
        if geometry:
            geometry = _with_output_geometry(geometry, image.size)
        self._last_capture_geometry = geometry
        return data, geometry

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
