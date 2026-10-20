"""Separate input/capture/window capabilities and the refusals they drive.

These tests do not need a real desktop session. They pin the states that used
to be flattened into ``input_available``: Wayland/XWayland, denied macOS
Accessibility or Screen Recording permission, a non-interactive Windows input
desktop, missing grim, and unsupported window enumeration.
"""

import os
import pathlib
import sys
import types
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from controller import capabilities  # noqa: E402
from controller.gui_stub import StubPyAutoGUI  # noqa: E402


def fake_pyautogui():
    return types.SimpleNamespace(__name__="pyautogui")


class CapabilityEnvTest(unittest.TestCase):
    ENV_KEYS = (
        "DISPLAY",
        "WAYLAND_DISPLAY",
        "XDG_SESSION_TYPE",
        "NEURO_HEADLESS",
        "NEURO_ALLOW_XWAYLAND_INPUT",
    )

    def setUp(self):
        self.saved = {key: os.environ.get(key) for key in self.ENV_KEYS}

    def tearDown(self):
        for key, value in self.saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def force_wayland(self):
        os.environ.pop("NEURO_HEADLESS", None)
        os.environ.pop("NEURO_ALLOW_XWAYLAND_INPUT", None)
        os.environ["WAYLAND_DISPLAY"] = "wayland-test"
        os.environ["DISPLAY"] = ":0"
        os.environ["XDG_SESSION_TYPE"] = "wayland"
        # Test the branch on every CI OS, not only Linux runners.
        patcher = mock.patch.object(capabilities, "wayland_session", return_value=True)
        patcher.start()
        self.addCleanup(patcher.stop)


class InputCapabilityTest(CapabilityEnvTest):
    def test_wayland_refuses_pyautogui_as_full_desktop_input(self):
        self.force_wayland()
        state = capabilities.input_capability(
            headless=False, backend=fake_pyautogui(), platform="linux"
        )
        self.assertFalse(state["available"])
        self.assertIn("Wayland", state["reason"])
        self.assertIn("NEURO_ALLOW_XWAYLAND_INPUT", state["reason"])
        self.assertEqual(state["session"], "wayland")

    def test_xwayland_opt_in_is_explicit_and_limited(self):
        self.force_wayland()
        os.environ["NEURO_ALLOW_XWAYLAND_INPUT"] = "1"
        state = capabilities.input_capability(
            headless=False, backend=fake_pyautogui(), platform="linux"
        )
        self.assertTrue(state["available"])
        self.assertEqual(state["backend"], "pyautogui-xwayland")
        self.assertIn("XWayland", state["warning"])

    def test_wayland_stub_refuses_before_a_mixed_script_runs(self):
        self.force_wayland()
        stub = StubPyAutoGUI("Wayland session detected")
        refusal = capabilities.input_execution_refusal(
            headless=False, backend=stub, platform="linux"
        )
        self.assertIn("Wayland", refusal)

    def test_macos_accessibility_denial_is_actionable(self):
        with mock.patch.object(
            capabilities, "probe_macos_accessibility", return_value=False
        ):
            state = capabilities.input_capability(
                headless=False, backend=fake_pyautogui(), platform="macos"
            )
        self.assertFalse(state["available"])
        self.assertIn("Accessibility", state["reason"])
        self.assertIn("System Settings", state["reason"])

    def test_macos_unknown_permission_does_not_claim_a_denial(self):
        with mock.patch.object(
            capabilities, "probe_macos_accessibility", return_value=None
        ):
            state = capabilities.input_capability(
                headless=False, backend=fake_pyautogui(), platform="macos"
            )
        self.assertTrue(state["available"])
        self.assertIn("could not be probed", state["warning"])

    def test_windows_interactive_desktop_is_required(self):
        probe = {
            "available": False,
            "reason": "Windows has no interactive input desktop (locked or disconnected)",
        }
        with mock.patch.object(
            capabilities, "probe_windows_input_desktop", return_value=probe
        ):
            state = capabilities.input_capability(
                headless=False, backend=fake_pyautogui(), platform="windows"
            )
        self.assertFalse(state["available"])
        self.assertIn("interactive input desktop", state["reason"])

    def test_custom_backend_does_not_trigger_os_permission_probes(self):
        with mock.patch.object(
            capabilities, "probe_macos_accessibility", return_value=False
        ) as probe:
            state = capabilities.input_capability(
                headless=False, backend=object(), platform="macos"
            )
        self.assertTrue(state["available"])
        probe.assert_not_called()


class CaptureCapabilityTest(CapabilityEnvTest):
    def test_wayland_without_grim_refuses_the_mss_fallback(self):
        self.force_wayland()
        state = capabilities.capture_capability(
            headless=False, mss_available=True, grim_path=None, platform="linux"
        )
        self.assertFalse(state["available"])
        self.assertEqual(state["backend"], "none")
        self.assertIn("grim", state["reason"])
        self.assertIn("XWayland", state["reason"])

    def test_wayland_with_grim_uses_the_compositor(self):
        self.force_wayland()
        state = capabilities.capture_capability(
            headless=False,
            mss_available=True,
            grim_path="/usr/bin/grim",
            platform="linux",
        )
        self.assertTrue(state["available"])
        self.assertEqual(state["backend"], "grim")

    def test_macos_screen_recording_denial_is_actionable(self):
        with mock.patch.object(
            capabilities, "probe_macos_screen_capture", return_value=False
        ):
            state = capabilities.capture_capability(
                headless=False, mss_available=True, platform="macos"
            )
        self.assertFalse(state["available"])
        self.assertIn("Screen Recording", state["reason"])
        self.assertIn("System Settings", state["reason"])

    def test_missing_mss_is_named(self):
        state = capabilities.capture_capability(
            headless=False, mss_available=False, platform="windows"
        )
        self.assertFalse(state["available"])
        self.assertIn("mss", state["reason"])

    def test_window_enumeration_on_wayland_is_unsupported(self):
        self.force_wayland()
        state = capabilities.window_capability(
            headless=False, backend_available=True, platform="linux"
        )
        self.assertFalse(state["available"])
        self.assertIn("Wayland", state["reason"])

    def test_missing_pygetwindow_is_named(self):
        state = capabilities.window_capability(
            headless=False, backend_available=False, platform="windows"
        )
        self.assertFalse(state["available"])
        self.assertIn("PyGetWindow", state["reason"])

    def test_headless_reason_is_reused(self):
        state = capabilities.input_capability(
            headless=True, backend=fake_pyautogui(), platform="linux"
        )
        self.assertFalse(state["available"])
        self.assertTrue(state["reason"])


if __name__ == "__main__":
    unittest.main()
