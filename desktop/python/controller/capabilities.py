"""Passive capability probes for the desktop controller.

The controller used to publish one ``input_available`` flag that meant little
more than "PyAutoGUI imported". That overstated what it could do: PyAutoGUI is
X11/XWayland on Linux, needs Accessibility permission on macOS and needs an
interactive input desktop on Windows. Capture and window enumeration have their
own backends and their own failure modes.

Everything here is passive. Permission probes never show an OS prompt; granting
a permission stays an explicit operator setup step. No probe changes desktop
configuration or grants device access.
"""

from __future__ import annotations

import ctypes
import os
import sys
from typing import Any, Dict, Optional

from .gui_stub import (
    StubPyAutoGUI,
    allow_xwayland_input,
    describe_headless_reason,
    wayland_input_reason,
    wayland_session,
)

def current_platform(platform: Optional[str] = None) -> str:
    if platform:
        return platform
    if sys.platform.startswith("win"):
        return "windows"
    if sys.platform == "darwin":
        return "macos"
    return "linux"


def _capability(
    available: bool,
    backend: Optional[str] = None,
    reason: Optional[str] = None,
    warning: Optional[str] = None,
    **extra: Any,
) -> Dict[str, Any]:
    value: Dict[str, Any] = {
        "available": bool(available),
        "backend": backend,
        "reason": reason,
        "warning": warning,
    }
    value.update(extra)
    return value


def _is_real_pyautogui(backend: Any) -> bool:
    """True for the imported PyAutoGUI module, not a test or custom backend."""
    return getattr(backend, "__name__", "") == "pyautogui"


def probe_macos_accessibility() -> Optional[bool]:
    """Passive ``AXIsProcessTrusted`` probe.

    Returns ``None`` when the framework cannot be loaded, so callers do not
    claim a denial they could not actually observe.
    """
    if sys.platform != "darwin":
        return None
    try:
        framework = ctypes.cdll.LoadLibrary(
            "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
        )
        probe = framework.AXIsProcessTrusted
        probe.argtypes = []
        probe.restype = ctypes.c_bool
        return bool(probe())
    except Exception:
        return None


def probe_macos_screen_capture() -> Optional[bool]:
    """Passive ``CGPreflightScreenCaptureAccess`` probe; never prompts."""
    if sys.platform != "darwin":
        return None
    try:
        framework = ctypes.cdll.LoadLibrary(
            "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
        )
        probe = framework.CGPreflightScreenCaptureAccess
        probe.argtypes = []
        probe.restype = ctypes.c_bool
        return bool(probe())
    except Exception:
        return None


def probe_windows_input_desktop() -> Dict[str, Any]:
    """Check for a Win32 interactive input desktop without sending input.

    A service session, a disconnected RDP session and the secure/locked desktop
    do not expose the normal input desktop. Target-specific restrictions still
    belong to the action layer; this only answers the session-level question.
    """
    if not sys.platform.startswith("win"):
        return {"available": None, "reason": "not running on Windows"}
    try:
        from ctypes import wintypes

        user32 = ctypes.WinDLL("user32", use_last_error=True)
        user32.OpenInputDesktop.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        user32.OpenInputDesktop.restype = wintypes.HANDLE
        user32.CloseDesktop.argtypes = [wintypes.HANDLE]
        user32.CloseDesktop.restype = wintypes.BOOL

        # DESKTOP_READOBJECTS | DESKTOP_SWITCHDESKTOP
        desktop = user32.OpenInputDesktop(0, False, 0x0001 | 0x0100)
        if not desktop:
            error = ctypes.get_last_error()
            return {
                "available": False,
                "reason": (
                    "Windows has no interactive input desktop "
                    f"(OpenInputDesktop failed with error {error}). The session may be "
                    "locked or disconnected, or the controller may be running as a "
                    "service. Sign in to the interactive desktop and retry."
                ),
            }
        user32.CloseDesktop(desktop)
        return {"available": True, "reason": None}
    except Exception as exc:  # pragma: no cover - exercised on Windows only
        return {
            "available": None,
            "reason": f"could not probe the Windows input desktop: {exc}",
        }


def input_capability(
    headless: bool = False,
    backend: Any = None,
    platform: Optional[str] = None,
    verify_permissions: bool = True,
) -> Dict[str, Any]:
    """Describe whether keyboard/pointer input can affect this desktop."""
    if headless:
        return _capability(
            False,
            "none",
            describe_headless_reason(),
            keyboard=False,
            pointer=False,
            session="headless",
        )

    if backend is None or isinstance(backend, StubPyAutoGUI):
        reason = getattr(backend, "reason", "") or "the GUI input driver could not load"
        return _capability(
            False,
            "unavailable",
            reason,
            keyboard=False,
            pointer=False,
            session="unavailable",
        )

    plat = current_platform(platform)
    warning = None
    session = "desktop"

    if plat == "linux":
        if wayland_session():
            if not allow_xwayland_input():
                return _capability(
                    False,
                    "none",
                    wayland_input_reason(),
                    keyboard=False,
                    pointer=False,
                    session="wayland",
                )
            warning = (
                "Wayland session: this backend controls XWayland windows only; "
                "native Wayland windows are out of reach."
            )
            session = "wayland-xwayland"
        elif not os.environ.get("DISPLAY"):
            return _capability(
                False,
                "none",
                "No DISPLAY is set in this session, so X11 input cannot run here.",
                keyboard=False,
                pointer=False,
                session="no-display",
            )

    # Permission/session probes only make sense for the bundled backend. A
    # custom recording backend in tests has its own semantics.
    if verify_permissions and _is_real_pyautogui(backend):
        if plat == "macos":
            trusted = probe_macos_accessibility()
            if trusted is False:
                return _capability(
                    False,
                    "pyautogui",
                    "macOS Accessibility permission is not granted for the app or "
                    "terminal running the controller. Grant it in System Settings > "
                    "Privacy & Security > Accessibility, then restart the controller.",
                    keyboard=False,
                    pointer=False,
                    session="macos-permission-denied",
                )
            if trusted is None:
                warning = "macOS Accessibility permission could not be probed; verify it manually."
        elif plat == "windows":
            probe = probe_windows_input_desktop()
            if probe.get("available") is False:
                return _capability(
                    False,
                    "pyautogui",
                    str(probe.get("reason") or "no interactive Windows input desktop"),
                    keyboard=False,
                    pointer=False,
                    session="windows-no-input-desktop",
                )
            if probe.get("available") is None:
                warning = str(probe.get("reason") or "Windows input-desktop probe unavailable")

    backend_name = "pyautogui-xwayland" if session == "wayland-xwayland" else "pyautogui"
    return _capability(
        True,
        backend_name,
        None,
        warning,
        keyboard=True,
        pointer=True,
        session=session,
    )


def input_execution_refusal(
    headless: bool = False,
    backend: Any = None,
    platform: Optional[str] = None,
) -> Optional[str]:
    """A reason that must block real input even though loading succeeded.

    Stub/headless backends already raise on use; this covers states where the
    library is importable but input would go to the wrong desktop or be denied.
    """
    if headless or backend is None:
        return None
    if isinstance(backend, StubPyAutoGUI):
        # A Wayland stub is a real refusal worth applying before any mixed
        # script starts (SHELL/LAUNCH would otherwise run first). A plain
        # headless stub still fails naturally when an input instruction runs.
        if wayland_session() and not allow_xwayland_input():
            return str(getattr(backend, "reason", "") or wayland_input_reason())
        return None
    if not _is_real_pyautogui(backend):
        return None
    capability = input_capability(
        headless=False, backend=backend, platform=platform, verify_permissions=True
    )
    if capability["available"]:
        return None
    return str(capability.get("reason") or "desktop input is unavailable")


def capture_capability(
    headless: bool = False,
    mss_available: bool = False,
    pillow_available: bool = True,
    platform: Optional[str] = None,
    grim_path: Optional[str] = None,
) -> Dict[str, Any]:
    """Describe the capture backend and why it can or cannot run."""
    if headless:
        return _capability(False, "none", describe_headless_reason(), session="headless")
    if not pillow_available:
        return _capability(
            False,
            "none",
            "Screenshot unavailable: Pillow is not installed on this machine.",
            session="missing-dependency",
        )

    plat = current_platform(platform)
    warning = None

    if plat == "linux" and wayland_session():
        if not grim_path:
            return _capability(
                False,
                "none",
                "Wayland screenshot unavailable: grim is not installed. MSS would "
                "capture only XWayland windows, so that misleading fallback is "
                "disabled. Install grim (or a compositor capture backend) and retry.",
                session="wayland",
            )
        return _capability(
            True,
            "grim",
            None,
            warning=(
                "grim captures the compositor output; capture bounds and scale are "
                "reported with each screenshot."
            ),
            session="wayland",
        )

    if plat == "macos":
        permitted = probe_macos_screen_capture()
        if permitted is False:
            return _capability(
                False,
                "mss",
                "macOS Screen Recording permission is not granted. Grant it in System "
                "Settings > Privacy & Security > Screen Recording, restart the "
                "controller, then retry.",
                session="macos-permission-denied",
            )
        if permitted is None:
            warning = "macOS Screen Recording permission could not be probed; verify it manually."

    if not mss_available:
        return _capability(
            False,
            "none",
            "Screenshot unavailable: install mss for X11/Windows/macOS capture.",
            warning=warning,
            session="missing-dependency",
        )
    return _capability(True, "mss", None, warning, session="desktop")


def window_capability(
    headless: bool = False,
    backend_available: bool = False,
    platform: Optional[str] = None,
) -> Dict[str, Any]:
    """Describe window enumeration, which PyGetWindow only partly supports."""
    if headless:
        return _capability(False, "none", describe_headless_reason(), session="headless")
    if not backend_available:
        return _capability(
            False,
            "none",
            "Window enumeration is unavailable: PyGetWindow is not installed on this machine.",
            session="missing-dependency",
        )
    plat = current_platform(platform)
    if plat == "linux" and wayland_session():
        return _capability(
            False,
            "pygetwindow",
            "Window enumeration is unavailable on Wayland: PyGetWindow reads X11 "
            "windows and cannot list native Wayland windows.",
            session="wayland",
        )
    return _capability(True, "pygetwindow", None, session="desktop")
