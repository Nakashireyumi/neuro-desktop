"""The pointer path must end exactly on its requested target.

A human-like intermediate path is fine; a few pixels short is not. The old
implementation smoothed against the last emitted position inside the loop, so
its own offset compounded and a 1000,600 move ended near 578,346 whenever
noise did not happen to hide the error.
"""

import pathlib
import sys
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller.libraries import mouse_pathfinder  # noqa: E402


class FakeDriver:
    def __init__(self, start=(0, 0)):
        self.position_value = start
        self.moves = []

    def position(self):
        return self.position_value

    def moveTo(self, x, y):
        self.position_value = (x, y)
        self.moves.append((x, y))


class AlgorithmicPathTest(unittest.TestCase):
    def run_path(self, target, profile, jitter=None, duration=0.0):
        driver = FakeDriver()
        with mock.patch.object(mouse_pathfinder, "pyautogui", driver):
            mouse_pathfinder.AlgorithmicPath(profile).move_to(
                target[0], target[1], duration=duration, jitter=jitter
            )
        return driver

    def test_default_path_ends_exactly_on_target(self):
        driver = self.run_path((1000, 600), mouse_pathfinder.PathProfile())
        self.assertEqual(driver.moves[-1], (1000, 600))
        self.assertEqual(driver.position_value, (1000, 600))

    def test_smoothing_and_noise_cannot_shorten_the_path(self):
        profile = mouse_pathfinder.PathProfile(
            overshoot_chance=0.0,
            noise_scale=8.0,
            smoothing_factor=0.9,
        )
        for target in ((1000, 600), (5, 7), (1920, 1080), (301, 2)):
            with self.subTest(target=target):
                driver = self.run_path(target, profile)
                self.assertEqual(driver.moves[-1], target)

    def test_optional_overshoot_still_corrects_exactly(self):
        profile = mouse_pathfinder.PathProfile(
            overshoot_chance=1.0,
            noise_scale=0.0,
            smoothing_factor=0.0,
        )
        driver = self.run_path((500, 400), profile)
        self.assertEqual(driver.moves[-1], (500, 400))

    def test_intermediate_moves_stay_near_the_line(self):
        profile = mouse_pathfinder.PathProfile(
            overshoot_chance=0.0, noise_scale=0.0, smoothing_factor=0.0
        )
        driver = self.run_path((1000, 600), profile)
        for x, y in driver.moves:
            self.assertLessEqual(abs(y - x * 0.6), 1)
        self.assertGreater(len(driver.moves), 1)


if __name__ == "__main__":
    unittest.main()
