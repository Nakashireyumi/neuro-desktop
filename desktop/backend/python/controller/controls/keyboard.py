import time
import pyautogui
from typing import List, Union
from ..desktop import DesktopMonitor


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
    def __init__(self, key: str):
        self.key = key

    def execute(self):
        pyautogui.keyDown(self.key)


class KeyUp(KeyboardInstruction):
    def __init__(self, key: str):
        self.key = key

    def execute(self):
        pyautogui.keyUp(self.key)


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
        self.queue.append(KeyDown(validated_key))

    def release(self, key: str):
        validated_key = self._validate_key(key)
        self.monitor.record_action(
            source="keyboard",
            action_type="RELEASE",
            data={"key": validated_key}
        )
        self.queue.append(KeyUp(validated_key))

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
