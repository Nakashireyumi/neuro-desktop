import time
from typing import List, Optional, Union
from ..desktop import DesktopMonitor
from ..gui_stub import NoDisplayError, load_pyautogui

# On a machine with no display this is a stub whose input calls raise a readable
# NoDisplayError instead of taking the whole controller down at import time.
pyautogui = load_pyautogui()


KEY_ALIASES = {
    "windows": "win",
    "control": "ctrl",
    "return": "enter",
    "escape": "esc",
}


class KeyboardInstruction:
    def execute(self):
        raise NotImplementedError


class KeyTap(KeyboardInstruction):
    def __init__(self, key: str, delay: float = 0.02):
        self.key = key
        self.delay = delay

    def execute(self):
        pyautogui.press(self.key)
        time.sleep(self.delay)


class KeyDown(KeyboardInstruction):
    def __init__(self, key: str, tracker: Optional[set] = None):
        self.key = key
        self.tracker = tracker

    def execute(self):
        pyautogui.keyDown(self.key, _pause=False)
        if self.tracker is not None:
            self.tracker.add(self.key)


class KeyUp(KeyboardInstruction):
    def __init__(self, key: str, tracker: Optional[set] = None):
        self.key = key
        self.tracker = tracker

    def execute(self):
        pyautogui.keyUp(self.key, _pause=False)
        if self.tracker is not None:
            self.tracker.discard(self.key)


class HoldForInstruction(KeyboardInstruction):
    """Press a key, hold it for a bounded time, then release it.

    Game movement ("walk forward for a second") arrives as a single
    hold-for command so a dropped connection cannot leave a key down.
    """

    def __init__(self, key: str, seconds: float, tracker: Optional[set] = None):
        self.key = key
        self.seconds = max(0.0, min(float(seconds), 30.0))
        self.tracker = tracker

    def execute(self):
        pyautogui.keyDown(self.key, _pause=False)
        if self.tracker is not None:
            self.tracker.add(self.key)
        try:
            time.sleep(self.seconds)
        finally:
            pyautogui.keyUp(self.key, _pause=False)
            if self.tracker is not None:
                self.tracker.discard(self.key)


class TypeText(KeyboardInstruction):
    def __init__(self, text: str, interval: float = 0.02):
        self.text = text
        self.interval = interval

    def execute(self):
        pyautogui.write(self.text, interval=self.interval)


class Shortcut(KeyboardInstruction):
    def __init__(self, *keys: str):
        self.keys = keys

    def execute(self):
        pyautogui.hotkey(*self.keys)


class Wait(KeyboardInstruction):
    def __init__(self, duration: float):
        self.duration = duration

    def execute(self):
        time.sleep(self.duration)


# -------------------------------------------------
# High-level Keyboard Controller
# -------------------------------------------------

class KeyboardController:
    """
    High-level, AI-friendly keyboard abstraction.
    """

    def __init__(self, monitor: DesktopMonitor):
        self.queue: List[KeyboardInstruction] = []
        self.monitor = monitor
        self._valid_keys = set(pyautogui.KEYBOARD_KEYS)
        # Keys currently held down, so they can always be released.
        self.held_keys: set = set()

    def _normalize_key(self, key: str) -> str:
        normalized = key.strip().lower()
        return KEY_ALIASES.get(normalized, normalized)

    def _validate_key(self, key: str) -> str:
        normalized = self._normalize_key(key)
        if normalized not in self._valid_keys:
            raise ValueError(f"Unsupported key: {key}")
        return normalized

    # ------------------------
    # Intent-level API
    # ------------------------

    def type(self, text: str, interval: float = 0.02):
        self.monitor.record_action(
            source="keyboard",
            action_type="TYPE",
            data={"text": text}
        )
        self.queue.append(TypeText(text, interval))

    def press(self, key: str):
        validated_key = self._validate_key(key)
        self.monitor.record_action(
            source="keyboard",
            action_type="PRESS",
            data={"key": validated_key}
        )
        self.queue.append(KeyTap(validated_key))

    def shortcut(self, *keys: str):
        validated_keys = tuple(self._validate_key(key) for key in keys)
        self.monitor.record_action(
            source="keyboard",
            action_type="SHORTCUT",
            data={"keys": validated_keys}
        )
        self.queue.append(Shortcut(*validated_keys))

    def hold(self, key: str):
        validated_key = self._validate_key(key)
        self.monitor.record_action(
            source="keyboard",
            action_type="HOLD",
            data={"key": validated_key}
        )
        self.queue.append(KeyDown(validated_key, self.held_keys))

    def release(self, key: str):
        validated_key = self._validate_key(key)
        self.monitor.record_action(
            source="keyboard",
            action_type="RELEASE",
            data={"key": validated_key}
        )
        self.queue.append(KeyUp(validated_key, self.held_keys))

    def hold_for(self, key: str, seconds: float):
        """Hold a key for a bounded time (game movement)."""
        validated_key = self._validate_key(key)
        self.monitor.record_action(
            source="keyboard",
            action_type="HOLD_FOR",
            data={"key": validated_key, "seconds": seconds},
        )
        self.queue.append(HoldForInstruction(validated_key, seconds, self.held_keys))

    def combo(self, *keys: str):
        """Press several keys together (alias of shortcut, kept for game input)."""
        self.shortcut(*keys)

    def release_all(self):
        """Release every key we believe is held down."""
        for key in list(self.held_keys):
            try:
                pyautogui.keyUp(key, _pause=False)
            except Exception:
                pass
            self.held_keys.discard(key)

    @property
    def headless(self) -> bool:
        return isinstance(pyautogui, type(None)) or bool(getattr(pyautogui, "_headless_stub", False))

    def wait(self, seconds: float):
        self.monitor.record_action(
            source="keyboard",
            action_type="WAIT",
            data={"seconds": seconds}
        )
        self.queue.append(Wait(seconds))

    # ------------------------
    # Macro helpers
    # ------------------------

    def enter(self):
        self.monitor.record_action(
            source="keyboard",
            action_type="ENTER",
            data={}
        )
        self.press("enter")

    def backspace(self, times: int = 1):
        self.monitor.record_action(
            source="keyboard",
            action_type="BACKSPACE",
            data={"times": times}
        )
        for _ in range(times):
            self.press("backspace")

    def delete_line(self):
        self.shortcut("ctrl", "a")
        self.press("backspace")

    # ------------------------
    # Execution
    # ------------------------

    def execute(self):
        for instr in self.queue:
            instr.execute()

    def clear(self):
        self.queue.clear()

    def dump(self):
        for i, instr in enumerate(self.queue):
            print(f"{i:02d}: {instr.__class__.__name__}")

# # Type a command safely
# kbd = KeyboardController()
#
# kbd.type("pip install requests")
# kbd.enter()
#
# kbd.execute()

# # Shortcut + text edit
# kbd.shortcut("ctrl", "c")
# kbd.wait(0.2)
# kbd.shortcut("ctrl", "v")
# kbd.execute()

# # ASM style queue
# kbd.hold("shift")
# kbd.press("a")
# kbd.release("shift")
# kbd.wait(0.1)
# kbd.type("bc")
# kbd.execute()
