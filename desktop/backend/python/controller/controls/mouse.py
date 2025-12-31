import pyautogui
import time
from typing import List, Tuple, Union
from ..desktop import DesktopMonitor

Point = Tuple[int, int]


class MouseInstruction:
    """Base class for mouse instructions."""
    def execute(self):
        raise NotImplementedError


class MoveInstruction(MouseInstruction):
    def __init__(self, x: int, y: int, duration: float = 0.1):
        self.x = x
        self.y = y
        self.duration = duration

    def execute(self):
        pyautogui.moveTo(self.x, self.y, duration=self.duration)


class ClickInstruction(MouseInstruction):
    def __init__(self, x: int, y: int, button: str = "left"):
        self.x = x
        self.y = y
        self.button = button

    def execute(self):
        pyautogui.click(self.x, self.y, button=self.button)


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

    def __init__(self, monitor: DesktopMonitor):
        self.screen_width, self.screen_height = pyautogui.size()
        self.instruction_queue: List[MouseInstruction] = []
        self.monitor = monitor

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

    def queue_click(self, x: int, y: int, button: str = "left"):
        self.monitor.record_action(
            source="mouse",
            action_type="CLICK",
            data={"x": x, "y": y, "button": button}
        )
        x, y = self.clamp_point(x, y)
        self.instruction_queue.append(ClickInstruction(x, y, button))

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

    def execute(self, clear_queue: bool = True):
        """
        Executes all queued instructions sequentially.
        """
        for instr in self.instruction_queue:
            instr.execute()

        if clear_queue:
            self.instruction_queue.clear()

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