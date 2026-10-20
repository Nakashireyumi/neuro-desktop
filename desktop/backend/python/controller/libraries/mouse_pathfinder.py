from dataclasses import dataclass

import math
import random
import time

from ..gui_stub import load_pyautogui

# Real pyautogui on a desktop session, a self-explaining stub without one: this
# module is imported by the controller everywhere, including on headless boxes.
pyautogui = load_pyautogui()
pyautogui.PAUSE = 0
@dataclass
class PathProfile:
    """Tuning parameters for different 'personalities'.

    The default is a short, predictable eased move. Organic noise and overshoot
    are opt-in because a missed endpoint is a functional bug, not a quirk: a
    later click lands on the wrong control.
    """
    min_duration: float = 0.05
    max_duration: float = 0.35
    overshoot_chance: float = 0.0
    noise_scale: float = 0.0
    smoothing_factor: float = 0.0
    max_steps: int = 48

class AlgorithmicPath:
    """Human-like algorithmic mouse path generator"""

    def __init__(self, profile: PathProfile | None = None):
        self.profile = profile or PathProfile()

    @staticmethod
    def ease_in_out_cubic(t: float) -> float:
        """Smoother easing function"""
        if t < 0.5:
            return 4 * t * t * t
        else:
            return 1 - pow(-2 * t + 2, 3) / 2

    @staticmethod
    def bezier_cubic(p0, p1, p2, p3, t):
        """Cubic Bézier curve for smoother paths"""
        return (
            (1 - t)**3 * p0 +
            3 * (1 - t)**2 * t * p1 +
            3 * (1 - t) * t**2 * p2 +
            t**3 * p3
        )
    
    # Function to linear interpolate (lerp) between two points
    @staticmethod
    def lerp(current_pos, target_pos, weight):
        return (current_pos[0] + (target_pos[0] - current_pos[0]) * weight,
                current_pos[1] + (target_pos[1] - current_pos[1]) * weight)

    @staticmethod
    def perlin_noise_1d(x: float) -> float:
        """Simple 1D Perlin-like noise for organic variation"""
        xi = int(x)
        xf = x - xi
        
        # Fade curve
        fade = xf * xf * (3 - 2 * xf)
        
        # Hash function for pseudo-randomness
        random.seed(xi)
        a = random.random() * 2 - 1
        random.seed(xi + 1)
        b = random.random() * 2 - 1
        
        return a + fade * (b - a)

    def move_to(
        self,
        target_x: int,
        target_y: int,
        duration: float = None,
        jitter: float = None
    ):
        """Move to exactly ``(target_x, target_y)``.

        Intermediate points may be eased, noisy or smoothed, but the last move is
        always the unmodified target. Smoothing against ``pyautogui.position()``
        inside the loop used to compound its own offset and stop short of the
        target whenever noise happened not to mask it.
        """
        start_x, start_y = pyautogui.position()
        distance = math.sqrt((target_x - start_x) ** 2 + (target_y - start_y) ** 2)

        if duration is None:
            duration = self.profile.min_duration + (distance / 1000.0) * 0.3
            duration = min(duration, self.profile.max_duration)
        duration = max(0.0, min(float(duration), 30.0))

        steps = max(1, min(self.profile.max_steps, int(distance / 12) + 1))
        delay = duration / steps
        noise_scale = jitter if jitter is not None else self.profile.noise_scale
        smoothing = max(0.0, min(float(self.profile.smoothing_factor), 1.0))

        for step in range(1, steps + 1):
            t_eased = self.ease_in_out_cubic(step / steps)
            x = start_x + (target_x - start_x) * t_eased
            y = start_y + (target_y - start_y) * t_eased

            if noise_scale:
                # Fade the organic noise at both ends so it cannot move the
                # final point away from the target.
                envelope = math.sin(math.pi * (step / steps))
                x += self.perlin_noise_1d(step * 0.3) * noise_scale * envelope
                y += self.perlin_noise_1d(step * 0.3 + 100) * noise_scale * envelope

            if smoothing:
                # One fixed smoothing pass around the path, not repeated against
                # the last emitted position (which compounded the shortfall).
                x = start_x + (x - start_x) * (1.0 - smoothing * 0.5)
                y = start_y + (y - start_y) * (1.0 - smoothing * 0.5)

            pyautogui.moveTo(int(round(x)), int(round(y)))
            if delay:
                time.sleep(delay)

        # Deliberate overshoot is off by default; if a profile enables it, the
        # correction and the unconditional final move still end exactly here.
        if (
            self.profile.overshoot_chance > 0
            and distance > 50
            and random.random() < self.profile.overshoot_chance
        ):
            pyautogui.moveTo(
                target_x + random.randint(-3, 3),
                target_y + random.randint(-3, 3),
            )
            time.sleep(min(0.05, duration) if duration else 0.0)

        pyautogui.moveTo(int(target_x), int(target_y))
        if duration:
            time.sleep(min(0.02, duration))

    def idle_wander(self, radius: int = 80, duration: float = 2.5):
        """Natural idle movement"""
        start_x, start_y = pyautogui.position()
        
        # Use circular motion for more natural idle
        angle = random.uniform(0, 2 * math.pi)
        distance = random.uniform(radius * 0.3, radius)
        
        end_x = start_x + int(math.cos(angle) * distance)
        end_y = start_y + int(math.sin(angle) * distance)
        
        self.move_to(end_x, end_y, duration=duration)

# Test the algrothim
if __name__ == "__main__":
    path = AlgorithmicPath()
    
    print("Moving to test position in 1 seconds...")
    time.sleep(1)
    
    # Test movement
    path.move_to(800, 400)
    time.sleep(0.5)
    path.move_to(1200, 600)
    time.sleep(0.5)
    path.move_to(600, 300)
