from .controls.mouse import MouseController
from .controls.keyboard import KeyboardController

from .actions import ActionParser
from .desktop import DesktopMonitor
from .gui_stub import describe_headless_reason, is_headless
from .shell import ShellDeniedError, ShellTimeoutError, run as run_shell_command


def _headless() -> bool:
    """Headless when asked, or when there is simply no display session.

    The second half is what makes Neuro Desktop work on a command-line-only OS
    without any configuration: pyautogui cannot drive a machine with no X/Wayland
    session, but the shell capability can, and Neuro should still be able to use
    the machine.
    """
    return is_headless()


def initialize_driver():
    """Create drivers. In headless/CI, skip live display listeners."""
    headless = _headless()
    monitor = DesktopMonitor(track_mouse=not headless, headless=headless)
    mouse = MouseController(monitor, headless=headless)
    keyboard = KeyboardController(monitor)
    parser = ActionParser(keyboard, mouse, monitor)
    if headless:
        print(f"[controller] headless mode: {describe_headless_reason()}; input actions will refuse to run")
    return monitor, mouse, keyboard, parser


def run_shell(command, cwd=None, timeout=None):
    """Run one command line for the shell_command action.

    Raises ShellDeniedError / ShellTimeoutError; the executor turns those into a
    failed IPC response so Neuro sees the firewall's explanation.
    """
    return run_shell_command(command, cwd=cwd, timeout=timeout)


__all__ = [
    "initialize_driver",
    "run_shell",
    "ShellDeniedError",
    "ShellTimeoutError",
]
