"""OS-specific mappings for high-level desktop intents.

Action script commands stay OS-neutral; this module resolves them to
keyboard shortcuts (or marks them unsupported) per platform.
"""

from __future__ import annotations

import sys
from dataclasses import dataclass
from typing import Dict, List, Optional, Tuple


@dataclass(frozen=True)
class IntentAction:
    """How to realize a high-level intent on the current OS."""

    # Keys for KeyboardController.shortcut / press
    keys: Tuple[str, ...]
    # True → call press(keys[0]); False → shortcut(*keys)
    press_only: bool = False
    # Human-readable note when behavior differs across OSes
    note: str = ""


def current_platform() -> str:
    if sys.platform.startswith("win"):
        return "windows"
    if sys.platform == "darwin":
        return "macos"
    return "linux"


# Meta key name used by pyautogui / our keyboard controller:
# Windows: win, macOS: command, Linux: win (Super) where supported.
_WIN = "win"
_CMD = "command"
_ALT = "alt"
_CTRL = "ctrl"
_SHIFT = "shift"


def _windows_intents() -> Dict[str, IntentAction]:
    return {
        "OPEN_START_MENU": IntentAction((_WIN,), press_only=True),
        "OPEN_WINDOWS_MENU": IntentAction((_WIN,), press_only=True),
        "SHOW_DESKTOP": IntentAction((_WIN, "d")),
        "MINIMIZE_ALL_WINDOWS": IntentAction((_WIN, "m")),
        "CLOSE_FOREGROUND_APP": IntentAction((_ALT, "f4")),
        "OPEN_TASK_MANAGER": IntentAction((_CTRL, _SHIFT, "esc")),
        "CLOSE_ALL_APPS": IntentAction((_WIN, "d"), note="fallback: show desktop"),
        "OPEN_FILE_EXPLORER": IntentAction((_WIN, "e")),
        "OPEN_RUN_DIALOG": IntentAction((_WIN, "r")),
        "OPEN_SEARCH": IntentAction((_WIN, "s")),
        "SNAP_WINDOW_LEFT": IntentAction((_WIN, "left")),
        "SNAP_WINDOW_RIGHT": IntentAction((_WIN, "right")),
        "OPEN_SETTINGS": IntentAction((_WIN, "i")),
        "OPEN_WINDOWS_SETTINGS": IntentAction((_WIN, "i")),
        "OPEN_NOTIFICATION_CENTER": IntentAction((_WIN, "a")),
        "OPEN_CLIPBOARD_HISTORY": IntentAction((_WIN, "v")),
        "LOCK_WORKSTATION": IntentAction((_WIN, "l")),
        "SWITCH_APP_NEXT": IntentAction((_ALT, "tab")),
        "SWITCH_APP_PREVIOUS": IntentAction((_ALT, _SHIFT, "tab")),
        "OPEN_POWER_USER_MENU": IntentAction((_WIN, "x")),
        "TAKE_SCREEN_SNIP": IntentAction((_WIN, _SHIFT, "s")),
    }


def _macos_intents() -> Dict[str, IntentAction]:
    return {
        "OPEN_START_MENU": IntentAction((_CMD, "space"), note="Spotlight as launcher"),
        "OPEN_WINDOWS_MENU": IntentAction((_CMD, "space"), note="Spotlight as launcher"),
        "SHOW_DESKTOP": IntentAction((_CMD, "f3"), note="Mission Control / desktop; may vary by macOS version"),
        "MINIMIZE_ALL_WINDOWS": IntentAction((_CMD, _ALT, "h"), note="hide others approximation"),
        "CLOSE_FOREGROUND_APP": IntentAction((_CMD, "q")),
        "OPEN_TASK_MANAGER": IntentAction((_CMD, _ALT, "esc"), note="Force Quit"),
        "CLOSE_ALL_APPS": IntentAction((_CMD, "f3"), note="fallback: Mission Control"),
        "OPEN_FILE_EXPLORER": IntentAction((_CMD, _SHIFT, "n"), note="new Finder window"),
        "OPEN_RUN_DIALOG": IntentAction((_CMD, "space"), note="Spotlight"),
        "OPEN_SEARCH": IntentAction((_CMD, "space")),
        "SNAP_WINDOW_LEFT": IntentAction((_CTRL, _CMD, "left"), note="requires window manager support"),
        "SNAP_WINDOW_RIGHT": IntentAction((_CTRL, _CMD, "right"), note="requires window manager support"),
        "OPEN_SETTINGS": IntentAction((_CMD, ","), note="app preferences; System Settings via Spotlight"),
        "OPEN_WINDOWS_SETTINGS": IntentAction((_CMD, ",")),
        "OPEN_NOTIFICATION_CENTER": IntentAction((_CTRL, "n"), note="Notification Center gesture alternative"),
        "LOCK_WORKSTATION": IntentAction((_CTRL, _CMD, "q")),
        "SWITCH_APP_NEXT": IntentAction((_CMD, "tab")),
        "SWITCH_APP_PREVIOUS": IntentAction((_CMD, _SHIFT, "tab")),
        "OPEN_POWER_USER_MENU": IntentAction((_CMD, "space"), note="no Win+X equivalent"),
        "TAKE_SCREEN_SNIP": IntentAction((_CMD, _SHIFT, "4")),
    }


def _linux_intents() -> Dict[str, IntentAction]:
    # GNOME/KDE-ish Super-key conventions; desktop environments vary.
    return {
        "OPEN_START_MENU": IntentAction((_WIN,), press_only=True, note="Super / Activities"),
        "OPEN_WINDOWS_MENU": IntentAction((_WIN,), press_only=True),
        "SHOW_DESKTOP": IntentAction((_WIN, "d")),
        "MINIMIZE_ALL_WINDOWS": IntentAction((_WIN, "d"), note="often same as show desktop"),
        "CLOSE_FOREGROUND_APP": IntentAction((_ALT, "f4")),
        "OPEN_TASK_MANAGER": IntentAction((_CTRL, _SHIFT, "esc"), note="DE-dependent; may need gnome-system-monitor"),
        "CLOSE_ALL_APPS": IntentAction((_WIN, "d"), note="fallback: show desktop"),
        "OPEN_FILE_EXPLORER": IntentAction((_WIN, "e"), note="DE-dependent"),
        "OPEN_RUN_DIALOG": IntentAction((_ALT, "f2")),
        "OPEN_SEARCH": IntentAction((_WIN,), press_only=True),
        "SNAP_WINDOW_LEFT": IntentAction((_WIN, "left")),
        "SNAP_WINDOW_RIGHT": IntentAction((_WIN, "right")),
        "OPEN_SETTINGS": IntentAction((_WIN, "i"), note="DE-dependent"),
        "OPEN_WINDOWS_SETTINGS": IntentAction((_WIN, "i")),
        "OPEN_NOTIFICATION_CENTER": IntentAction((_WIN, "v"), note="GNOME calendar/notifications often Super+V"),
        "OPEN_CLIPBOARD_HISTORY": IntentAction((_WIN, "v"), note="if clipboard manager bound"),
        "LOCK_WORKSTATION": IntentAction((_WIN, "l")),
        "SWITCH_APP_NEXT": IntentAction((_ALT, "tab")),
        "SWITCH_APP_PREVIOUS": IntentAction((_ALT, _SHIFT, "tab")),
        "OPEN_POWER_USER_MENU": IntentAction((_WIN, "x"), note="DE-dependent"),
        "TAKE_SCREEN_SNIP": IntentAction((_WIN, _SHIFT, "s"), note="GNOME screenshot UI; else PrintScreen"),
    }


# macOS clipboard history has no built-in shortcut; leave empty → unsupported
_UNSUPPORTED = IntentAction((), note="not supported on this platform")


def _finalize_macos(intents: Dict[str, IntentAction]) -> Dict[str, IntentAction]:
    out = dict(intents)
    out["OPEN_CLIPBOARD_HISTORY"] = _UNSUPPORTED
    return out


_PLATFORM_MAP = {
    "windows": _windows_intents,
    "macos": lambda: _finalize_macos(_macos_intents()),
    "linux": _linux_intents,
}


def resolve_intent(
    command: str,
    platform: Optional[str] = None,
) -> IntentAction:
    """Resolve an action-script intent name to keys for the given OS."""
    plat = platform or current_platform()
    factory = _PLATFORM_MAP.get(plat, _linux_intents)
    intents = factory()
    key = command.upper()
    if key not in intents:
        raise KeyError(f"Unknown desktop intent: {command}")
    return intents[key]


def apply_intent(keyboard, command: str, platform: Optional[str] = None) -> None:
    """Execute a high-level intent via the keyboard controller."""
    intent = resolve_intent(command, platform=platform)
    if not intent.keys:
        raise ValueError(
            f"{command} is not supported on {platform or current_platform()}"
            + (f" ({intent.note})" if intent.note else "")
        )
    if intent.press_only:
        keyboard.press(intent.keys[0])
    else:
        keyboard.shortcut(*intent.keys)


def supported_intents(platform: Optional[str] = None) -> List[str]:
    plat = platform or current_platform()
    factory = _PLATFORM_MAP.get(plat, _linux_intents)
    return sorted(factory().keys())
