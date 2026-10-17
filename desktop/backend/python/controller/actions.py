import copy
import math
import shlex
import time
from dataclasses import dataclass
from typing import TYPE_CHECKING, List, Optional, Tuple

from .launcher import LaunchError, launch_target
from .platform_intents import apply_intent, resolve_intent, supported_intents
from .shell import ShellDeniedError, ShellTimeoutError, check_command, run as run_shell_command, shell_timeout

if TYPE_CHECKING:
    from .controls.keyboard import KeyboardController
    from .controls.mouse import MouseController
    from .desktop import DesktopMonitor

Point = Tuple[int, int]

# High-level desktop intents resolved via platform_intents (OS-specific shortcuts).
_DESKTOP_INTENTS = frozenset(supported_intents("windows")) | frozenset(
    supported_intents("linux")
) | frozenset(supported_intents("macos"))


class ActionParseError(Exception):
    pass


MAX_SCRIPT_BYTES = 64 * 1024
MAX_PENDING_ACTIONS = 512
MAX_QUEUE_SECONDS = 20.0
ACTION_OVERHEAD_SECONDS = 0.01


@dataclass(frozen=True)
class PlannedAction:
    """A validated command; constructing it never touches input or starts programs."""

    parser: "ActionParser"
    line_no: int
    tokens: Tuple[str, ...]
    shell_policy: Optional[dict]
    shell_cwd: Optional[str]

    @property
    def estimated_seconds(self):
        return self.parser._estimate_tokens(self.tokens)

    def execute(self):
        self.parser._execute_planned(self)


class ActionParser:
    """
    Unified keyboard + mouse action parser.
    """

    def __init__(
        self,
        keyboard: "KeyboardController",
        mouse: "MouseController",
        monitor: "DesktopMonitor",
        platform: Optional[str] = None,
    ):
        self.kbd = keyboard
        self.mouse = mouse
        self.monitor = monitor
        self.platform = platform
        # Populated by SHELL lines so a caller can read the transcript back.
        self.last_shell_output = ""
        self.shell_cwd = None
        self.shell_policy = None
        self.queue: List[PlannedAction] = []
        self.execution_deadline = None

    # ------------------------
    # Public API
    # ------------------------

    def parse(self, script: str):
        """Validate the complete script, then append an ordered, deferred plan.

        A later syntax or policy error cannot leave an earlier key, launch or
        shell command queued. Execution is explicit, including for SHELL and
        RELEASE_ALL, so execute_now=False has the same meaning for every verb.
        """
        if not isinstance(script, str) or len(script.encode("utf-8")) > MAX_SCRIPT_BYTES:
            raise ActionParseError("Script must be at most 65536 UTF-8 bytes")
        if "\x00" in script:
            raise ActionParseError("Script cannot contain NUL characters")
        plan = []
        shell_policy = copy.deepcopy(self.shell_policy)
        for line_no, raw_line in enumerate(script.splitlines(), start=1):
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            if len(plan) + len(self.queue) >= MAX_PENDING_ACTIONS:
                raise ActionParseError("At most 512 pending actions are allowed; execute and clear the queue")
            try:
                tokens = shlex.split(line)
                self._validate_tokens(tokens)
            except Exception as exc:
                raise ActionParseError(f"Line {line_no}: {exc}") from exc
            plan.append(PlannedAction(self, line_no, tuple(tokens), shell_policy, self.shell_cwd))
        if sum(action.estimated_seconds for action in self.queue + plan) > MAX_QUEUE_SECONDS + 1e-9:
            raise ActionParseError("Pending actions exceed the 20-second delay budget; split the script and clear the queue")
        self.queue.extend(plan)
        return tuple(plan)

    def execute(self):
        """Execute the retained plan in source order; clear() controls replay."""
        if sum(action.estimated_seconds for action in self.queue) > MAX_QUEUE_SECONDS + 1e-9:
            raise ActionParseError("Pending actions exceed the 20-second delay budget")
        previous_deadline = self.execution_deadline
        self.execution_deadline = time.monotonic() + MAX_QUEUE_SECONDS
        try:
            for action in tuple(self.queue):
                self._remaining_seconds()
                action.execute()
                self._remaining_seconds()
        except Exception:
            self.clear()
            self.last_shell_output = ""
            self.release_all()
            raise
        finally:
            self.execution_deadline = previous_deadline

    def clear(self):
        self.queue.clear()

    @staticmethod
    def _arity(tokens, *lengths):
        if len(tokens) not in lengths:
            raise ActionParseError(f"Invalid arguments for {tokens[0].upper()}")

    @staticmethod
    def _number(value, minimum, maximum):
        number = float(value)
        if not math.isfinite(number) or not minimum <= number <= maximum:
            raise ActionParseError(f"Number must be finite and between {minimum:g} and {maximum:g}")
        return number

    @staticmethod
    def _integer(value):
        number = int(value)
        if not -(2 ** 31) <= number < 2 ** 31:
            raise ActionParseError("Coordinates must be signed 32-bit integers")
        return number

    def _validate_tokens(self, tokens):
        if not tokens:
            raise ActionParseError("An action command is required")
        cmd = tokens[0].upper()
        if cmd == "TYPE":
            if len(tokens) < 2:
                raise ActionParseError("TYPE requires quoted text")
        elif cmd in ("ENTER", "RELEASE_ALL"):
            self._arity(tokens, 1)
        elif cmd in ("PRESS", "HOLD", "RELEASE", "HOLD_FOR", "SHORTCUT", "COMBO"):
            if cmd == "HOLD_FOR":
                self._arity(tokens, 3)
                if self._number(tokens[2], 0, 30) == 0:
                    raise ActionParseError("HOLD_FOR seconds must be greater than 0")
                keys = tokens[1:2]
            elif cmd in ("SHORTCUT", "COMBO"):
                if len(tokens) < 2:
                    raise ActionParseError(f"{cmd} requires at least one key")
                keys = tokens[1:]
            else:
                self._arity(tokens, 2)
                keys = tokens[1:]
            validate = getattr(self.kbd, "_validate_key", None) or getattr(self.kbd, "_validate", None)
            for key in keys:
                validate(key)
        elif cmd in _DESKTOP_INTENTS:
            self._arity(tokens, 1)
            intent = resolve_intent(cmd, platform=self.platform)
            if not intent.keys:
                raise ActionParseError(f"{cmd} is not supported on this platform")
            validate = getattr(self.kbd, "_validate_key", None) or getattr(self.kbd, "_validate", None)
            for key in intent.keys:
                validate(key)
        elif cmd in ("MOVE", "MOVE_REL"):
            self._arity(tokens, 3, 4)
            self._integer(tokens[1])
            self._integer(tokens[2])
            if len(tokens) == 4:
                self._number(tokens[3], 0, 2 if cmd == "MOVE_REL" else 30)
        elif cmd in ("MOVE_N", "CLICK_N"):
            self._arity(tokens, 3, 4) if cmd == "CLICK_N" else self._arity(tokens, 3)
            self._number(tokens[1], 0, 1)
            self._number(tokens[2], 0, 1)
            if len(tokens) == 4:
                self._parse_mouse_button(tokens[3])
        elif cmd == "CLICK":
            self._arity(tokens, 1, 2)
            if len(tokens) == 2:
                self._parse_mouse_button(tokens[1])
        elif cmd == "LINE":
            self._arity(tokens, 5, 7)
            for value in tokens[1:5]:
                self._integer(value)
            if len(tokens) == 7:
                if tokens[5].upper() != "STEPS" or not 1 <= int(tokens[6]) <= 1000:
                    raise ActionParseError("LINE steps must be between 1 and 1000")
        elif cmd == "PATH":
            if not 3 <= len(tokens) <= 513 or (len(tokens) - 1) % 2:
                raise ActionParseError("PATH requires 1 to 256 coordinate pairs")
            for value in tokens[1:]:
                self._integer(value)
        elif cmd in ("SHELL", "LAUNCH"):
            if len(tokens) < 2 or not " ".join(tokens[1:]).strip():
                raise ActionParseError(f"{cmd} requires a command or target")
            if cmd == "SHELL":
                check_command(" ".join(tokens[1:]), policy=self.shell_policy)
        elif cmd == "WAIT":
            self._arity(tokens, 2)
            self._number(tokens[1], 0, 30)
        else:
            raise ActionParseError(f"Unknown command: {cmd}")
        if cmd not in ("SHELL", "LAUNCH", "WAIT", "RELEASE_ALL") and getattr(self.kbd, "headless", False):
            raise ActionParseError("No display is available; use shell_command on a headless machine")

    @staticmethod
    def _backend_pause():
        from .controls.keyboard import pyautogui
        pause = float(getattr(pyautogui, "PAUSE", 0.0))
        return pause if math.isfinite(pause) and pause >= 0 else math.inf

    def _estimate_tokens(self, tokens):
        """Conservative deliberate input delay, shared with the bridge grammar.

        OS scheduling is not predictable; a cooperative wall-clock deadline and
        the shell timeout separately bound work once execution starts.
        """
        cmd = tokens[0].upper()
        delay, pause_calls = 0.0, 0
        if cmd == "TYPE":
            delay, pause_calls = len(" ".join(tokens[1:])) * 0.02, 1
        elif cmd in ("PRESS", "ENTER") or cmd in _DESKTOP_INTENTS:
            delay, pause_calls = 0.02, 1
        elif cmd in ("WAIT", "HOLD_FOR"):
            delay = float(tokens[-1])
        elif cmd in ("MOVE", "MOVE_N", "CLICK_N"):
            duration = float(tokens[3]) if cmd == "MOVE" and len(tokens) == 4 else 0.1
            delay, pause_calls = duration * 1.5 + 0.14, 5 if cmd == "CLICK_N" else 4
        elif cmd == "MOVE_REL":
            delay = float(tokens[3]) if len(tokens) == 4 else 0.0
        elif cmd in ("CLICK", "SHORTCUT", "COMBO"):
            pause_calls = 1
        elif cmd == "LINE":
            points = (int(tokens[6]) if len(tokens) == 7 else 50) + 1
            delay, pause_calls = points * 0.02, points
        elif cmd == "PATH":
            pairs = (len(tokens) - 1) // 2
            points = (pairs - 1) * 31 if pairs > 1 else 1
            delay, pause_calls = points * 0.02, points
        return ACTION_OVERHEAD_SECONDS + delay + (pause_calls * self._backend_pause() if pause_calls else 0.0)

    def _remaining_seconds(self):
        remaining = MAX_QUEUE_SECONDS if self.execution_deadline is None else self.execution_deadline - time.monotonic()
        if remaining <= 0:
            raise ActionParseError("The 20-second execution budget expired; continue with a shorter script")
        return remaining

    def _execute_planned(self, action):
        # Controllers are instruction builders. Run only the instructions this
        # source line creates, preserving mixed keyboard/mouse order and leaving
        # unrelated deferred wire commands untouched.
        keyboard_queue = getattr(self.kbd, "queue", None)
        mouse_queue = getattr(self.mouse, "instruction_queue", None)
        keyboard_start = len(keyboard_queue) if keyboard_queue is not None else 0
        mouse_start = len(mouse_queue) if mouse_queue is not None else 0
        previous_policy, previous_cwd = self.shell_policy, self.shell_cwd
        self.shell_policy, self.shell_cwd = action.shell_policy, action.shell_cwd
        try:
            self._remaining_seconds()
            self._parse_line(list(action.tokens))
            for queue, start in ((keyboard_queue, keyboard_start), (mouse_queue, mouse_start)):
                if queue is not None:
                    for instruction in queue[start:]:
                        self._remaining_seconds()
                        instruction.execute()
            self._remaining_seconds()
        except Exception as exc:
            raise ActionParseError(f"Line {action.line_no}: {exc}") from exc
        finally:
            if keyboard_queue is not None:
                del keyboard_queue[keyboard_start:]
            if mouse_queue is not None:
                del mouse_queue[mouse_start:]
            self.shell_policy, self.shell_cwd = previous_policy, previous_cwd

    # ------------------------
    # Line parser
    # ------------------------

    def _parse_line(self, tokens):
        if not tokens:
            return

        cmd = tokens[0].upper()

        self.monitor.record_action(
            source="parser",
            action_type=cmd,
            data={"tokens": tokens}
        )

        # -------- Keyboard --------
        if cmd == "TYPE":
            self._kbd_type(tokens)

        elif cmd == "ENTER":
            self.kbd.enter()

        elif cmd == "PRESS":
            self._kbd_press(tokens)

        elif cmd == "HOLD":
            self._kbd_hold(tokens)

        elif cmd == "RELEASE":
            self._kbd_release(tokens)

        elif cmd == "SHORTCUT":
            self._kbd_shortcut(tokens)

        # -------- Keyboard: game-style held input --------
        elif cmd == "HOLD_FOR":
            self._kbd_hold_for(tokens)

        elif cmd == "COMBO":
            self._kbd_combo(tokens)

        elif cmd == "RELEASE_ALL":
            self.release_all()

        # -------- High-level desktop intents (cross-platform) --------
        elif cmd in _DESKTOP_INTENTS or cmd == "OPEN_SETTINGS":
            try:
                apply_intent(self.kbd, cmd, platform=self.platform)
            except (KeyError, ValueError) as exc:
                raise ActionParseError(str(exc)) from exc

        # -------- Mouse --------
        elif cmd == "MOVE":
            self._mouse_move(tokens)

        elif cmd == "MOVE_REL":
            self._mouse_move_relative(tokens)

        elif cmd == "MOVE_N":
            self._mouse_move_normalized(tokens)

        elif cmd == "CLICK":
            self._mouse_click(tokens)

        elif cmd == "CLICK_N":
            self._mouse_click_normalized(tokens)

        elif cmd == "LINE":
            self._mouse_line(tokens)

        elif cmd == "PATH":
            self._mouse_path(tokens)

        # -------- System --------
        elif cmd == "LAUNCH":
            self._launch(tokens)

        # -------- Command line (headless-friendly) --------
        elif cmd == "SHELL":
            self._shell(tokens)

        # -------- Shared --------
        elif cmd == "WAIT":
            self._wait(tokens)

        else:
            raise ActionParseError(f"Unknown command: {cmd}")

    # ========================
    # Keyboard commands
    # ========================

    def _kbd_type(self, tokens: List[str]):
        if len(tokens) < 2:
            raise ActionParseError("TYPE requires quoted text")
        text = " ".join(tokens[1:])
        self.kbd.type(text)

    def _kbd_press(self, tokens: List[str]):
        if len(tokens) != 2:
            raise ActionParseError("PRESS key")
        self.kbd.press(tokens[1])

    def _kbd_hold(self, tokens: List[str]):
        if len(tokens) != 2:
            raise ActionParseError("HOLD key")
        self.kbd.hold(tokens[1])

    def _kbd_release(self, tokens: List[str]):
        if len(tokens) != 2:
            raise ActionParseError("RELEASE key")
        self.kbd.release(tokens[1])

    def _kbd_shortcut(self, tokens: List[str]):
        if len(tokens) < 2:
            raise ActionParseError("SHORTCUT key1 key2 ...")
        self.kbd.shortcut(*tokens[1:])

    def _kbd_combo(self, tokens: List[str]):
        if len(tokens) < 2:
            raise ActionParseError("COMBO key1 key2 ...")
        self.kbd.combo(*tokens[1:])

    def _kbd_hold_for(self, tokens: List[str]):
        if len(tokens) != 3:
            raise ActionParseError("HOLD_FOR key seconds")
        try:
            seconds = float(tokens[2])
        except ValueError as exc:
            raise ActionParseError("HOLD_FOR seconds must be a number") from exc
        if seconds <= 0 or seconds > 30:
            raise ActionParseError("HOLD_FOR seconds must be between 0 and 30")
        self.kbd.hold_for(tokens[1], seconds)

    # ========================
    # Mouse commands
    # ========================

    @staticmethod
    def _parse_mouse_button(button: str) -> str:
        normalized = button.lower()
        if normalized not in ("left", "right", "middle"):
            raise ActionParseError("Mouse button must be one of: left, right, middle")
        return normalized

    def _mouse_move(self, tokens: List[str]):
        if len(tokens) not in (3, 4):
            raise ActionParseError("MOVE x y [duration]")
        x, y = int(tokens[1]), int(tokens[2])
        duration = float(tokens[3]) if len(tokens) == 4 else 0.1
        self.mouse.queue_move(x, y, duration)

    def _mouse_move_normalized(self, tokens: List[str]):
        if len(tokens) != 3:
            raise ActionParseError("MOVE_N nx ny")
        nx, ny = float(tokens[1]), float(tokens[2])
        x, y = self.mouse.map_normalized(nx, ny)
        self.mouse.queue_move(x, y)

    def _mouse_move_relative(self, tokens: List[str]):
        if len(tokens) not in (3, 4):
            raise ActionParseError("MOVE_REL dx dy [duration]")
        dx, dy = int(tokens[1]), int(tokens[2])
        duration = float(tokens[3]) if len(tokens) == 4 else 0.0
        if duration < 0 or duration > 2:
            raise ActionParseError("MOVE_REL duration must be between 0 and 2")
        self.mouse.queue_move_rel(dx, dy, duration)

    def _mouse_click(self, tokens: List[str]):
        if len(tokens) > 2:
            raise ActionParseError("CLICK [button]")
        button = self._parse_mouse_button(tokens[1]) if len(tokens) == 2 else "left"
        self.mouse.queue_click(button)

    def _mouse_click_normalized(self, tokens: List[str]):
        if len(tokens) not in (3, 4):
            raise ActionParseError("CLICK_N nx ny [button]")
        nx, ny = float(tokens[1]), float(tokens[2])
        button = self._parse_mouse_button(tokens[3]) if len(tokens) == 4 else "left"
        x, y = self.mouse.map_normalized(nx, ny)
        self.mouse.queue_move(x, y)
        self.mouse.queue_click(button)

    def _mouse_line(self, tokens: List[str]):
        if len(tokens) < 5:
            raise ActionParseError("LINE x1 y1 x2 y2 [STEPS n]")

        x1, y1, x2, y2 = map(int, tokens[1:5])
        steps = 50

        if len(tokens) == 7:
            idx = 5
            if idx + 1 >= len(tokens):
                raise ActionParseError("LINE x1 y1 x2 y2 [STEPS n]")
            steps = int(tokens[idx + 1])

        path = self.mouse.draw_line((x1, y1), (x2, y2), steps)
        self.mouse.queue_path(path)

    def _mouse_path(self, tokens: List[str]):
        if len(tokens) < 3:
            raise ActionParseError("PATH requires at least one coordinate pair")
        if (len(tokens) - 1) % 2 != 0:
            raise ActionParseError("PATH requires even number of coordinates")

        coords = list(map(int, tokens[1:]))
        points: List[Point] = [
            (coords[i], coords[i + 1])
            for i in range(0, len(coords), 2)
        ]

        path = self.mouse.draw_polyline(points) if len(points) > 1 else points
        self.mouse.queue_path(path)

    # ========================
    # System
    # ========================

    def _launch(self, tokens: List[str]):
        if len(tokens) < 2:
            raise ActionParseError("LAUNCH path-or-url")
        target = " ".join(tokens[1:])
        try:
            launch_target(target, platform=self.platform)
        except LaunchError as exc:
            raise ActionParseError(str(exc)) from exc

    def _shell(self, tokens: List[str]):
        if len(tokens) < 2:
            raise ActionParseError('SHELL "command line"')
        command = " ".join(tokens[1:])
        try:
            self.last_shell_output = run_shell_command(command, cwd=self.shell_cwd, policy=self.shell_policy,
                                                       timeout=min(shell_timeout(), self._remaining_seconds()))
        except (ShellDeniedError, ShellTimeoutError) as exc:
            raise ActionParseError(str(exc)) from exc

    def release_all(self):
        """Safety hatch: release every key and mouse button we hold."""
        self.kbd.release_all()
        self.mouse.release_all()

    # ========================
    # Shared
    # ========================

    def _wait(self, tokens: List[str]):
        if len(tokens) != 2:
            raise ActionParseError("WAIT seconds")
        seconds = float(tokens[1])
        self.kbd.wait(seconds)


def instruction_seconds(instruction):
    """Estimate queued wire primitives using the same defaults as macro lines."""
    if isinstance(instruction, PlannedAction):
        return instruction.estimated_seconds
    pause = ActionParser._backend_pause()
    kind = type(instruction).__name__
    if kind == "TypeText":
        delay = len(instruction.text) * instruction.interval + pause
    elif kind == "KeyTap":
        delay = instruction.delay + pause
    elif kind in ("Wait", "WaitInstruction"):
        delay = instruction.duration
    elif kind in ("HoldForInstruction", "HoldClickInstruction"):
        delay = instruction.seconds
    elif kind == "MoveInstruction":
        delay = instruction.duration * 1.5 + 0.14 + 4 * pause
    elif kind == "MoveRelativeInstruction":
        delay = instruction.duration
    elif kind == "PathInstruction":
        delay = len(instruction.points) * (instruction.step_duration + pause)
    elif kind in ("ClickInstruction", "Shortcut"):
        delay = pause
    else:
        delay = 0.0
    if not math.isfinite(delay) or delay < 0:
        return math.inf
    return ACTION_OVERHEAD_SECONDS + delay
