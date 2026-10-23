"""Cross-platform "open this thing" helper.

Used by the ``LAUNCH`` action-script command, which the game layer uses to start
a game from its profile. Commands are handed to the platform opener rather than
a shell, so a profile cannot smuggle shell metacharacters into the executor.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
from typing import List, Optional


class LaunchError(RuntimeError):
    pass


def _opener_for_platform(platform: Optional[str] = None) -> List[str]:
    platform = platform or current_platform()

    if platform == "windows":
        # os.startfile handles both paths and protocol URLs (steam://...).
        return []
    if platform == "macos":
        return ["open"]
    return ["xdg-open"]


def current_platform() -> str:
    if sys.platform.startswith("win"):
        return "windows"
    if sys.platform == "darwin":
        return "macos"
    return "linux"


def launch_target(target: str, platform: Optional[str] = None, dry_run: bool = False) -> str:
    """Open a path or protocol URL with the OS opener.

    Returns the command that was used (useful for tests and logs).
    """
    target = (target or "").strip()
    if not target:
        raise LaunchError("LAUNCH requires a path or URL")

    platform = platform or current_platform()

    if dry_run:
        return target

    if platform == "windows":
        os.startfile(target)  # type: ignore[attr-defined]  # Windows-only
        return target

    opener = _opener_for_platform(platform)
    if not opener:
        raise LaunchError(f"no opener available on {platform}")

    executable = shutil.which(opener[0])
    if executable is None:
        raise LaunchError(
            f"'{opener[0]}' is not installed, so LAUNCH cannot open {target!r} "
            f"(install xdg-utils, or open it manually)"
        )

    try:
        subprocess.Popen(  # noqa: S603 - fixed argv, no shell
            [executable, target],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
    except OSError as exc:  # pragma: no cover - environment dependent
        raise LaunchError(f"failed to launch {target!r}: {exc}") from exc

    return f"{opener[0]} {target}"
