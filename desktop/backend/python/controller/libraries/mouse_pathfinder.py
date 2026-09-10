from dataclasses import dataclass

import math
import random
import time

import pyautogui

pyautogui.PAUSE = 0
@dataclass
class PathProfile:
    """Tuning parameters for different 'personalities'"""
    min_duration: float = 0.0005
    max_duration: float = 0.0012
    overshoot_chance: float = 0.3  # 30% chance of overshoot
    noise_scale: float = 0.8  # Reduced noise for smoothness
    smoothing_factor: float = 0.578 # Very human-like smoothness and fluidity

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
        start_x, start_y = pyautogui.position()
        
        # Calculate distance for adaptive parameters
        distance = math.sqrt((target_x - start_x)**2 + (target_y - start_y)**2)
        
        # Adaptive duration based on distance
        if duration is None:
            duration = self.profile.min_duration + (distance / 1000) * 0.3
            duration = min(duration, self.profile.max_duration)
        
        # Adaptive steps: fewer steps = smoother, but distance-dependent
        #steps = max(15, int(distance / 10))  # About 10 pixels per step
        #steps = min(steps, 60)  # Cap at 60 for very long distances
        steps = 1
        # Create control points for cubic Bézier
        # Add randomness to curve direction
        angle_offset = random.uniform(-math.pi/4, math.pi/4)
        curve_distance = distance * random.uniform(0.2, 0.4)
        
        ctrl1_x = start_x + (target_x - start_x) * 0.33 + math.cos(angle_offset) * curve_distance
        ctrl1_y = start_y + (target_y - start_y) * 0.33 + math.sin(angle_offset) * curve_distance
        
        ctrl2_x = start_x + (target_x - start_x) * 0.66 - math.cos(angle_offset) * curve_distance * 0.5
        ctrl2_y = start_y + (target_y - start_y) * 0.66 - math.sin(angle_offset) * curve_distance * 0.5
        
        # Determine if we overshoot
        overshoot = random.random() < self.profile.overshoot_chance
        
        delay = duration / steps
        noise_scale = jitter if jitter is not None else self.profile.noise_scale
        
        for i in range(steps + 1):
            t = i / steps
            
            # Apply easing
            t_eased = self.ease_in_out_cubic(t)
            
            # Get position on Bézier curve
            x = self.bezier_cubic(start_x, ctrl1_x, ctrl2_x, target_x, t_eased)
            y = self.bezier_cubic(start_y, ctrl1_y, ctrl2_y, target_y, t_eased)
            
            # Add organic Perlin-like noise (much smoother than random jitter)
            noise_x = self.perlin_noise_1d(i * 0.3) * noise_scale
            noise_y = self.perlin_noise_1d(i * 0.3 + 100) * noise_scale
            
            x += noise_x
            y += noise_y

            smoothed_pos = self.lerp(pyautogui.position(), (x, y), self.profile.smoothing_factor)
            x = smoothed_pos[0]
            y = smoothed_pos[1]
            
            pyautogui.moveTo(int(x), int(y))
            
            # Variable delay with slight randomness
            actual_delay = delay * random.uniform(0.56, 0.75)
            time.sleep(actual_delay)
        
        # Overshoot and correction (human behavior)
        if overshoot and distance > 50:
            overshoot_x = target_x + random.randint(-3, 3)
            overshoot_y = target_y + random.randint(-3, 3)
            smoothed_pos = self.lerp(pyautogui.position(), (overshoot_x, overshoot_y), self.profile.smoothing_factor)
            overshoot_x = smoothed_pos[0]
            overshoot_y = smoothed_pos[1]
            pyautogui.moveTo(overshoot_x, overshoot_y)
            time.sleep(random.uniform(0.05, 0.07))
            pyautogui.moveTo(target_x, target_y)
        
        # Final micro-pause
        time.sleep(random.uniform(0.05, 0.07))

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
