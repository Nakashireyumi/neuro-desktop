"""Command-line capability: the one thing a headless machine can still do.

This is the only place Neuro Desktop runs a program the model asked for, so the
rules are deliberately conservative and are enforced in three places:

* the Go bridge refuses a command before it reaches the executor (fast, clear
  message to Neuro, and the policy scope is checked there too),
* this module refuses it again when the script is executed,
* the permission policy scope (``shell``) is denied in the shipped example policy.

Defaults that matter:

* ``NEURO_SHELL_ALLOWLIST`` — comma-separated program names that may run. Empty
  means "run nothing"; ``*`` allows any program and logs a warning.
* ``NEURO_SHELL_DENYLIST`` — extra regular expressions, on top of the built-in
  patterns below.
* ``NEURO_SHELL_TIMEOUT`` — seconds (default 20, max 120).
* ``NEURO_SHELL_MAX_OUTPUT`` — characters of stdout/stderr kept (default 4000).

There is deliberately no PTY and no interactive mode: every command runs with a
timeout, in a fresh process, and long output is truncated.
"""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import sys
from typing import Any, List, Mapping, Optional, Tuple

DEFAULT_TIMEOUT = 20.0
MAX_TIMEOUT = 120.0
DEFAULT_MAX_OUTPUT = 4000
MAX_MAX_OUTPUT = 200_000

#: Patterns that are refused even when a program is on the allowlist. They are
#: "unrecoverable or sideways" rather than merely powerful: wiping disks,
#: killing the machine, editing /etc, piping the internet into a shell.
BUILT_IN_DENY_PATTERNS: Tuple[str, ...] = (
    r"\brm\s+(-{1,2}[a-zA-Z-]+\s+)*/(\*)?(\s|$)",
    r"\bmkfs(\.\w+)?\b",
    r"\bdd\b[^\n]*\bof\s*=\s*/dev/",
    r">\s*/dev/(sd|nvme|hd|disk)",
    r"\b(shutdown|reboot|poweroff|halt|init\s+0)\b",
    r"\bmkinitrd\b",
    r"\bchmod\s+(-[a-zA-Z]+\s+)*777\s+/\s*$",
    r"\bchown\s+(-[a-zA-Z]+\s+)*[^\s]+\s+/\s*$",
    r":\(\)\s*\{.*\};\s*:",  # fork bomb
    r"\bcurl\b[^\n]*\|\s*(ba|z|d|k)?sh\b",
    r"\bwget\b[^\n]*\|\s*(ba|z|d|k)?sh\b",
    r"\bhistory\s+-c\b",
    r"\bsudo\b",
    r"\bdoas\b",
    r"\bsu\s+-?\s*$",
    r"\bpasswd\b",
    r"\bvisudo\b",
    r"\bcrontab\s+-r\b",
    r"\breg\s+delete\b",
    r"\bdiskpart\b",
    r"\bformat\s+[a-zA-Z]:",
)


class ShellDeniedError(RuntimeError):
    """The command was refused by the firewall before running."""


class ShellTimeoutError(RuntimeError):
    """The command did not finish inside the timeout."""


def _float_env(name: str, default: float, maximum: float) -> float:
    try:
        value = float(os.environ.get(name, "") or default)
    except ValueError:
        return default
    if value <= 0:
        return default
    return min(value, maximum)


def _int_env(name: str, default: int, maximum: int) -> int:
    try:
        value = int(os.environ.get(name, "") or default)
    except ValueError:
        return default
    if value <= 0:
        return default
    return min(value, maximum)


def shell_timeout() -> float:
    return _float_env("NEURO_SHELL_TIMEOUT", DEFAULT_TIMEOUT, MAX_TIMEOUT)


def max_output() -> int:
    return _int_env("NEURO_SHELL_MAX_OUTPUT", DEFAULT_MAX_OUTPUT, MAX_MAX_OUTPUT)


def allowlist() -> List[str]:
    raw = os.environ.get("NEURO_SHELL_ALLOWLIST", "")
    return [item.strip() for item in raw.split(",") if item.strip()]


def deny_patterns() -> List[str]:
    raw = os.environ.get("NEURO_SHELL_DENYLIST", "")
    extra = [item.strip() for item in raw.split(",") if item.strip()]
    return list(BUILT_IN_DENY_PATTERNS) + extra


def allowlist_is_open() -> bool:
    return "*" in allowlist()


def first_token(command: str) -> str:
    """The program name the command starts with, without a path or extension."""
    stripped = command.strip()
    if not stripped:
        return ""
    try:
        parts = shlex.split(stripped, posix=not sys.platform.startswith("win"))
    except ValueError:
        parts = stripped.split()
    if not parts:
        return ""
    program = parts[0]
    # "C:\Windows\notepad.exe" and "/usr/bin/ls" both reduce to a name.
    program = program.replace("\\", "/").rsplit("/", 1)[-1]
    for suffix in (".exe", ".cmd", ".bat", ".ps1", ".sh"):
        if program.lower().endswith(suffix):
            program = program[: -len(suffix)]
    return program.lower()


_CHAINING = re.compile(r"[;&|`<>\n\r]|\$\(|\$\{")
_MAX_COMMAND_CHARS = 4000

NOT_CONFIGURED_LOCAL = (
    "Shell access is not configured: NEURO_SHELL_ALLOWLIST is empty, so no command may run. "
    "Vedal can allow specific programs, e.g. NEURO_SHELL_ALLOWLIST=ls,cat,python3."
)
NOT_CONFIGURED_SERVER = (
    "Shell access is not configured on the server: its NEURO_SHELL_ALLOWLIST is empty, so no command "
    "may run. Vedal can allow specific programs on the server, e.g. NEURO_SHELL_ALLOWLIST=ls,cat,python3."
)
CHAINING_MESSAGE = (
    "The shell firewall refused the command: it contains a shell operator (; & | < > ` $( or a newline). "
    "Only one program per command "
    "is allowed, with plain arguments. Split it into separate shell_command calls, one step each."
)


def _names(items: Any) -> List[str]:
    if not isinstance(items, (list, tuple)):
        return []
    return [str(item).strip() for item in items if str(item).strip()]


def check_command(command: str, policy: Optional[Mapping[str, Any]] = None) -> str:
    """Return the normalised command, or raise ShellDeniedError explaining why not.

    ``policy`` is what the server sent with the command: ``allowlist`` and
    ``denylist``. The server owns the shell policy and is the authority, so a
    command that reaches this machine has already been approved there. This
    machine's own ``NEURO_SHELL_ALLOWLIST`` can only narrow that list, never
    widen it, and an empty local list narrows nothing. Without a server policy
    (an older server) the local environment decides, as before.
    """
    command = command.strip()
    if not command:
        raise ShellDeniedError("The command is empty. Pass the command line as the first argument.")
    if len(command) > _MAX_COMMAND_CHARS:
        raise ShellDeniedError(
            f"The command is too long ({len(command)} characters, limit {_MAX_COMMAND_CHARS}). Split it into steps."
        )
    if _CHAINING.search(command):
        raise ShellDeniedError(CHAINING_MESSAGE)

    local = allowlist()
    server: Optional[List[str]] = None
    server_denies: List[str] = []
    if policy is not None and policy.get("allowlist") is not None:
        server = _names(policy.get("allowlist"))
        server_denies = _names(policy.get("denylist"))
        if not server:
            raise ShellDeniedError(NOT_CONFIGURED_SERVER)
    elif not local:
        raise ShellDeniedError(NOT_CONFIGURED_LOCAL)

    program = first_token(command)
    checks: List[Tuple[str, List[str]]] = []
    if server is not None:
        checks.append(("the server's shell allowlist", server))
    if local:
        checks.append(("this machine's NEURO_SHELL_ALLOWLIST", local))
    for label, names in checks:
        lowered = {name.lower() for name in names}
        if "*" in lowered:
            continue
        if program not in lowered:
            raise ShellDeniedError(
                f"Program {program!r} is not on {label} ({', '.join(sorted(names))}). "
                "Vedal can add it with NEURO_SHELL_ALLOWLIST on the server."
            )

    for pattern in deny_patterns() + server_denies:
        if re.search(pattern, command, flags=re.IGNORECASE):
            raise ShellDeniedError(
                "The command matches a blocked pattern and will not be run "
                f"(pattern: {pattern}). Split it into smaller, non-destructive steps."
            )

    return command


def run(
    command: str,
    cwd: Optional[str] = None,
    timeout: Optional[float] = None,
    policy: Optional[Mapping[str, Any]] = None,
) -> str:
    """Run one command line and return a summary for Neuro.

    The exit code and a truncated transcript are included, because the model
    needs the exit status to know whether it worked.
    """
    command = check_command(command, policy)
    cwd = cwd or os.environ.get("NEURO_SHELL_CWD") or None
    timeout = timeout or shell_timeout()
    limit = max_output()

    try:
        completed = subprocess.run(
            command,
            shell=True,  # the whole point: operators allowlist a program, neuro writes arguments
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=timeout,
            errors="replace",
        )
    except subprocess.TimeoutExpired:
        raise ShellTimeoutError(
            f"The command did not finish within {timeout:.0f}s and was killed."
        ) from None
    except FileNotFoundError as exc:
        raise ShellDeniedError(f"The working directory does not exist: {exc}") from exc
    except OSError as exc:
        raise ShellDeniedError(f"Could not start the command: {exc}") from exc

    def clip(text: str) -> str:
        text = (text or "").strip()
        if len(text) <= limit:
            return text
        return text[:limit] + f"\n… [{len(text) - limit} more characters truncated]"

    stdout = clip(completed.stdout)
    stderr = clip(completed.stderr)

    lines = [f"exit code: {completed.returncode}"]
    if stdout:
        lines.append(f"stdout:\n{stdout}")
    if stderr:
        lines.append(f"stderr:\n{stderr}")
    if not stdout and not stderr:
        lines.append("(no output)")

    return "\n".join(lines)
