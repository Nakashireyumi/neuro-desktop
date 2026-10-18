import time
from typing import List, Optional, Tuple, Union
from ..desktop import DesktopMonitor
from ..gui_stub import load_pyautogui

# Real pyautogui when there is a display, a stub that explains itself when not.
pyautogui = load_pyautogui()

Point = Tuple[int, int]

from ..libraries.mouse_pathfinder import AlgorithmicPath

class MouseInstruction:
    """Base class for mouse instructions."""
    pathfinder = AlgorithmicPath()
    def execute(self):
        raise NotImplementedError


class MoveInstruction(MouseInstruction):
    def __init__(self, x: int, y: int, duration: float = 0.1):
        self.x = x
        self.y = y
        self.duration = duration

    def execute(self):
        self.pathfinder.move_to(self.x, self.y, duration=self.duration)


class ClickInstruction(MouseInstruction):
    def __init__(self, button: str = "left"):
        self.button = button

    def execute(self):
        pyautogui.click(button=self.button)


class MoveRelativeInstruction(MouseInstruction):
    """Relative pointer movement.

    This is what in-game mouse-look needs: the absolute target of a first-person
    camera is unknown, only "how far did the player turn" matters. The move is
    applied directly instead of through the human-like pathfinder so the camera
    does not drift while the path is being generated.
    """

    def __init__(self, dx: int, dy: int, duration: float = 0.0):
        self.dx = dx
        self.dy = dy
        self.duration = duration

    def execute(self):
        pyautogui.moveRel(self.dx, self.dy, duration=self.duration, _pause=False)


class HoldClickInstruction(MouseInstruction):
    """Mouse button held down for a fixed time (sustained fire / aim).

    Bounded on purpose: an unbounded button-down is the mouse equivalent of a
    stuck key, and nothing in the protocol can rescue it.
    """

    def __init__(self, button: str = "left", seconds: float = 0.2, tracker: Optional[set] = None):
        self.button = button
        self.seconds = max(0.0, min(float(seconds), 30.0))
        self.tracker = tracker

    def execute(self):
        pyautogui.mouseDown(button=self.button, _pause=False)
        if self.tracker is not None:
            self.tracker.add(self.button)
        try:
            time.sleep(self.seconds)
        finally:
            pyautogui.mouseUp(button=self.button, _pause=False)
            if self.tracker is not None:
                self.tracker.discard(self.button)


class WaitInstruction(MouseInstruction):
    def __init__(self, duration: float):
        self.duration = duration

    def execute(self):
        time.sleep(self.duration)


class PathInstruction(MouseInstruction):
    """
    Moves mouse through a sequence of points (a drawn line/path).
    """
    def __init__(self, points: List[Point], step_duration: float = 0.02):
        self.points = points
        self.step_duration = step_duration

    def execute(self):
        for x, y in self.points:
            pyautogui.moveTo(x, y, duration=self.step_duration)


# -------------------------------------------------
# High-level Mouse Controller
# -------------------------------------------------

class MouseController:
    """
    High-level, AI-friendly mouse control abstraction.
    """

    def __init__(self, monitor: DesktopMonitor, headless: bool = False):
        self.monitor = monitor
        self.headless = headless
        self.instruction_queue: List[MouseInstruction] = []
        # Buttons currently held down, so a stuck button can always be released.
        self.held_buttons: set = set()
        self._screen_size: Optional[Point] = None
        if not headless:
            try:
                self._screen_size = pyautogui.size()
            except Exception:
                # Display may be unavailable at import time (Wayland auth, SSH, CI).
                self._screen_size = None

    @property
    def screen_width(self) -> int:
        w, _ = self._ensure_screen_size()
        return w

    @property
    def screen_height(self) -> int:
        _, h = self._ensure_screen_size()
        return h

    def _ensure_screen_size(self) -> Point:
        if self._screen_size is not None:
            return self._screen_size
        if self.headless:
            self._screen_size = (1920, 1080)
            return self._screen_size
        try:
            self._screen_size = pyautogui.size()
        except Exception as exc:
            raise RuntimeError(
                "Cannot query display size. On Wayland/Omarchy ensure the app "
                "runs in your graphical session (not via sudo/SSH without XAUTHORITY). "
                f"Underlying error: {exc}"
            ) from exc
        return self._screen_size

    # ------------------------
    # Coordinate mapping
    # ------------------------

    def map_normalized(self, nx: float, ny: float) -> Point:
        """
        Maps normalized coordinates (0.0–1.0) to screen pixels.
        """
        x = int(nx * self.screen_width)
        y = int(ny * self.screen_height)
        return x, y

    def clamp_point(self, x: int, y: int) -> Point:
        x = max(0, min(self.screen_width - 1, x))
        y = max(0, min(self.screen_height - 1, y))
        return x, y

    # ------------------------
    # Instruction builders
    # ------------------------

    def queue_move(self, x: int, y: int, duration: float = 0.1):
        self.monitor.record_action(
            source="mouse",
            action_type="MOVE",
            data={"x": x, "y": y, "duration": duration}
        )
        x, y = self.clamp_point(x, y)
        self.instruction_queue.append(MoveInstruction(x, y, duration))

    def queue_click(self, button: str = "left"):
        self.monitor.record_action(
            source="mouse",
            action_type="CLICK",
            data={"button": button}
        )
        self.instruction_queue.append(ClickInstruction(button))

    def queue_move_rel(self, dx: int, dy: int, duration: float = 0.0):
        """Queue a relative move (mouse-look)."""
        self.monitor.record_action(
            source="mouse",
            action_type="MOVE_REL",
            data={"dx": dx, "dy": dy, "duration": duration},
        )
        self.instruction_queue.append(MoveRelativeInstruction(dx, dy, duration))

    def queue_hold(self, button: str = "left", seconds: float = 0.2):
        """Queue a bounded press-and-hold of a mouse button."""
        self.monitor.record_action(
            source="mouse",
            action_type="HOLD_CLICK",
            data={"button": button, "seconds": seconds},
        )
        self.instruction_queue.append(HoldClickInstruction(button, seconds, self.held_buttons))

    def release_all(self):
        """Release every mouse button we believe is held down."""
        for button in list(self.held_buttons):
            try:
                pyautogui.mouseUp(button=button, _pause=False)
            except Exception:
                pass
            self.held_buttons.discard(button)

    def queue_wait(self, duration: float):
        self.monitor.record_action(
            source="mouse",
            action_type="WAIT",
            data={"duration": duration}
        )
        self.instruction_queue.append(WaitInstruction(duration))

    def queue_path(self, points: List[Point], step_duration: float = 0.02):
        self.monitor.record_action(
            source="mouse",
            action_type="PATH",
            data={"points": points, "step_duration": step_duration}
        )
        clamped = [self.clamp_point(x, y) for x, y in points]
        self.instruction_queue.append(PathInstruction(clamped, step_duration))

    # ------------------------
    # Drawing helpers (AI-friendly)
    # ------------------------

    def draw_line(self, start: Point, end: Point, steps: int = 50) -> List[Point]:
        """
        Generates a straight-line path between two points.
        """
        x1, y1 = start
        x2, y2 = end

        points = []
        for i in range(steps + 1):
            t = i / steps
            x = int(x1 + (x2 - x1) * t)
            y = int(y1 + (y2 - y1) * t)
            points.append(self.clamp_point(x, y))
        return points

    def draw_polyline(self, points: List[Point], steps_per_segment: int = 30) -> List[Point]:
        """
        Draws connected line segments through multiple points.
        """
        path = []
        for i in range(len(points) - 1):
            segment = self.draw_line(points[i], points[i + 1], steps_per_segment)
            path.extend(segment)
        return path

    # ------------------------
    # Execution
    # ------------------------

    def execute(self):
        """
        Executes all queued instructions sequentially.
        """
        for instr in self.instruction_queue:
            instr.execute()

    def clear(self):
        self.instruction_queue.clear()

    # ------------------------
    # Debug / inspection
    # ------------------------

    def dump_queue(self):
        for i, instr in enumerate(self.instruction_queue):
            print(f"{i:02d}: {instr.__class__.__name__}")

# # Draw a line across the screen example.
# mouse = MouseController()
#
# start = mouse.map_normalized(0.2, 0.3)
# end = mouse.map_normalized(0.8, 0.6)
#
# path = mouse.draw_line(start, end, steps=100)
# mouse.queue_path(path)
#
# mouse.execute()

# # ASM line instruction queue.
# mouse.queue_move(500, 500)
# mouse.queue_wait(0.2)
# mouse.queue_click(500, 500)
# mouse.queue_wait(0.5)
#
# path = mouse.draw_polyline([
#     (500, 500),
#     (600, 600),
#     (700, 550),
# ])
#
# mouse.queue_path(path)
# mouse.execute()