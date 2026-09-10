import shlex
from typing import TYPE_CHECKING, List, Optional, Tuple

from .platform_intents import apply_intent, supported_intents

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

    # ------------------------
    # Public API
    # ------------------------

    def parse(self, script: str):
        lines = script.strip().splitlines()

        for line_no, raw_line in enumerate(lines, start=1):
            line = raw_line.strip()

            if not line or line.startswith("#"):
                continue

            try:
                self._parse_line(line)
            except Exception as e:
                raise ActionParseError(
                    f"Line {line_no}: {line}\n-> {e}"
                ) from e

    # ------------------------
    # Line parser
    # ------------------------

    def _parse_line(self, line: str):
        tokens = shlex.split(line)
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

        # -------- High-level desktop intents (cross-platform) --------
        elif cmd in _DESKTOP_INTENTS or cmd == "OPEN_SETTINGS":
            try:
                apply_intent(self.kbd, cmd, platform=self.platform)
            except (KeyError, ValueError) as exc:
                raise ActionParseError(str(exc)) from exc

        # -------- Mouse --------
        elif cmd == "MOVE":
            self._mouse_move(tokens)

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

        if "STEPS" in tokens:
            idx = tokens.index("STEPS")
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

        path = self.mouse.draw_polyline(points)
        self.mouse.queue_path(path)

    # ========================
    # Shared
    # ========================

    def _wait(self, tokens: List[str]):
        if len(tokens) != 2:
            raise ActionParseError("WAIT seconds")
        seconds = float(tokens[1])
        self.kbd.wait(seconds)
        self.mouse.queue_wait(seconds)
