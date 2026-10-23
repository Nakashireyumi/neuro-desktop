"""A screenshot leaves the client as bytes, never as a path.

The server may run on another PC, so it cannot open a file that the client names.
These tests pin the status reply to base64 PNG, and check the size limits.
"""

import base64
import os
import pathlib
import subprocess
import sys
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller import agent as agent_module  # noqa: E402
from controller import desktop  # noqa: E402

FAKE_PNG = b"\x89PNG\r\n\x1a\nfake-image-bytes"


class ScreenshotIsBytesTest(unittest.TestCase):
    def test_capture_failure_is_explained_in_telemetry(self):
        agent = agent_module.Agent()
        agent.headless = False
        with mock.patch.object(agent.monitor, "capture_screen_png", side_effect=RuntimeError("compositor denied capture")):
            data = agent.status({"capture_screenshot": True})
        self.assertIsNone(data["screenshot_png_b64"])
        self.assertEqual(data["screenshot_error"], "compositor denied capture")

    def test_status_returns_base64_png_and_no_path(self):
        agent = agent_module.Agent()
        agent.headless = False
        agent.monitor.capture_screen_png = lambda *args, **kwargs: FAKE_PNG

        data = agent.status({"capture_screenshot": True})

        self.assertNotIn("screenshot_path", data)
        self.assertEqual(base64.b64decode(data["screenshot_png_b64"]), FAKE_PNG)

    def test_status_ignores_a_path_asked_for_by_the_server(self):
        agent = agent_module.Agent()
        agent.headless = False
        agent.monitor.capture_screen_png = lambda *args, **kwargs: FAKE_PNG

        data = agent.status({"capture_screenshot": True, "screenshot_path": "/etc/passwd"})

        self.assertNotIn("screenshot_path", data)
        self.assertEqual(base64.b64decode(data["screenshot_png_b64"]), FAKE_PNG)

    def test_no_capture_when_not_asked_or_headless(self):
        agent = agent_module.Agent()
        agent.headless = True
        called = []
        agent.monitor.capture_screen_png = lambda *args, **kwargs: called.append(1) or FAKE_PNG

        self.assertIsNone(agent.status({"capture_screenshot": True})["screenshot_png_b64"])
        agent.headless = False
        self.assertIsNone(agent.status({})["screenshot_png_b64"])
        self.assertEqual(called, [])


@unittest.skipUnless(desktop.Image is not None, "Pillow is not installed")
class ScreenshotSizeTest(unittest.TestCase):
    def test_wayland_capture_uses_compositor_even_without_mss(self):
        image = desktop.Image.new("RGB", (80, 60), (1, 2, 3))
        png = desktop._encode_png(image)
        monitor = desktop.DesktopMonitor(track_mouse=False)
        with mock.patch.dict(os.environ, {"WAYLAND_DISPLAY": "wayland-test"}), mock.patch.object(desktop.sys, "platform", "linux"), mock.patch.object(desktop, "mss", None), mock.patch.object(desktop.shutil, "which", return_value="/usr/bin/grim"), mock.patch.object(desktop.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, png, b"")):
            captured = monitor.capture_screen_png()
        self.assertTrue(captured.startswith(b"\x89PNG"))

    def test_wayland_capture_timeout_is_actionable(self):
        monitor = desktop.DesktopMonitor(track_mouse=False)
        with mock.patch.dict(os.environ, {"WAYLAND_DISPLAY": "wayland-test"}), mock.patch.object(desktop.sys, "platform", "linux"), mock.patch.object(desktop.shutil, "which", return_value="/usr/bin/grim"), mock.patch.object(desktop.subprocess, "run", side_effect=subprocess.TimeoutExpired("grim", 5)):
            with self.assertRaisesRegex(desktop.NoDisplayError, "Wayland screenshot failed"):
                monitor.capture_screen_png()

    def test_a_large_screen_is_scaled_to_the_side_limit(self):
        image = desktop.Image.new("RGB", (3840, 2160), (10, 20, 30))
        small = desktop._fit_within(image, desktop.SCREENSHOT_MAX_SIDE)
        self.assertLessEqual(max(small.size), desktop.SCREENSHOT_MAX_SIDE)

    def test_a_small_screen_is_not_scaled(self):
        image = desktop.Image.new("RGB", (800, 600), (10, 20, 30))
        self.assertEqual(desktop._fit_within(image, desktop.SCREENSHOT_MAX_SIDE).size, (800, 600))

    def test_capture_geometry_keeps_logical_origin_pixel_scale_and_output_scale(self):
        geometry = desktop._capture_geometry(
            "mss", -100, 50, 2880, 1800, 5760, 3600
        )
        self.assertEqual(geometry["origin"], {"x": -100, "y": 50})
        self.assertEqual(geometry["logical"], {"width": 2880, "height": 1800})
        self.assertEqual(geometry["pixel"], {"width": 5760, "height": 3600})
        self.assertEqual(geometry["pixel_scale"], {"x": 2.0, "y": 2.0})

        output = desktop._with_output_geometry(geometry, (1600, 1000))
        self.assertEqual(output["output"], {"width": 1600, "height": 1000})
        self.assertAlmostEqual(output["output_scale"]["x"], 1600 / 2880, places=5)
        self.assertAlmostEqual(output["output_scale"]["y"], 1000 / 1800, places=5)

    def test_wayland_without_grim_does_not_fall_back_to_mss(self):
        class ForbiddenMSS:
            def __enter__(self):
                raise AssertionError("MSS must not be used on Wayland without grim")

            def __exit__(self, *exc):
                return False

        monitor = desktop.DesktopMonitor(track_mouse=False)
        with mock.patch.dict(os.environ, {"WAYLAND_DISPLAY": "wayland-test", "XDG_SESSION_TYPE": "wayland"}), mock.patch.object(desktop.sys, "platform", "linux"), mock.patch.object(desktop, "mss", ForbiddenMSS()), mock.patch.object(desktop.shutil, "which", return_value=None):
            with self.assertRaises(desktop.NoDisplayError) as raised:
                monitor.capture_screen_png()
        self.assertIn("grim", str(raised.exception))
        self.assertIn("XWayland", str(raised.exception))

    def test_encoded_png_is_a_png_under_the_byte_limit(self):
        image = desktop.Image.new("RGB", (1600, 900), (1, 2, 3))
        data = desktop._encode_png(image)
        self.assertTrue(data.startswith(b"\x89PNG"))
        self.assertLess(len(data), desktop.SCREENSHOT_MAX_BYTES)


if __name__ == "__main__":
    unittest.main()
