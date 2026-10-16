"""Small, bounded HTTP client for the native manager; credentials stay in memory."""

from __future__ import annotations

import copy
import json
import urllib.error
import urllib.parse
import urllib.request

MAX_RESPONSE_BYTES = 4 * 1024 * 1024
SCOPES = ("input", "game", "vision", "shell", "filesystem", "process", "network", "system", "extensions")


class ApiError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


def server_url(value: str) -> str:
    url = urllib.parse.urlsplit(value.strip())
    if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password or url.query or url.fragment:
        raise ValueError("Use an http:// or https:// server address without credentials or query parameters.")
    try:
        url.port
    except ValueError as error:
        raise ValueError("The server port must be a number between 1 and 65535.") from error
    return urllib.parse.urlunsplit(url).rstrip("/")


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        # A redirect must never send the operator's dashboard token elsewhere.
        raise ApiError(code, "The server redirected this request. Enter its final address explicitly.")


class Client:
    def __init__(self, server: str, token: str = ""):
        self.server = server_url(server)
        self.token = token
        self.requests = 0
        self._opener = urllib.request.build_opener(_NoRedirect())

    def request(self, path: str, method: str = "GET", body=None, *, operator_token=""):
        if not (path.startswith("/api/") or path == "/health"):
            raise ValueError("Only Neuro Desktop API routes are supported.")
        headers = {"Accept": "application/json"}
        if self.token:
            headers["X-ND-Token"] = self.token
        if operator_token:
            headers["X-ND-Operator-Token"] = operator_token
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.server + path, data=data, headers=headers, method=method)
        self.requests += 1
        try:
            timeout = 125 if method == "POST" and path.endswith(("/stage", "/install")) else 8
            with self._opener.open(request, timeout=timeout) as response:
                return self._decode(response)
        except urllib.error.HTTPError as error:
            try:
                message = self._decode(error).get("error", f"HTTP {error.code}")
            except (ValueError, ApiError):
                message = f"Request failed (HTTP {error.code})."
            raise ApiError(error.code, str(message)) from error
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            raise ApiError(0, "Cannot reach the server. Check its address and connection. Refresh after a timed-out change to see whether it completed.") from error

    @staticmethod
    def _decode(response):
        data = response.read(MAX_RESPONSE_BYTES + 1)
        if len(data) > MAX_RESPONSE_BYTES:
            raise ApiError(0, "The server response exceeds the manager's size limit.")
        try:
            payload = json.loads(data)
        except (ValueError, UnicodeError) as error:
            raise ApiError(0, "The server returned invalid JSON.") from error
        if not isinstance(payload, dict):
            raise ApiError(0, "The server returned an unexpected response.")
        return payload


def normalise_policy(raw: dict) -> dict:
    policy = copy.deepcopy(raw)
    scopes = policy.get("scopes") or {}
    if not isinstance(scopes, dict):
        raise ApiError(0, "The server returned invalid permission scopes.")
    policy["scopes"] = scopes
    for name in SCOPES:
        value = scopes.get(name, False)
        if isinstance(value, bool):
            value = {"allowed": value}
        if value is None:
            value = {}
        if not isinstance(value, dict):
            raise ApiError(0, f"The server returned an invalid {name} scope.")
        value = copy.deepcopy(value)
        value["allowed"] = _policy_bool(value.get("allowed", False))
        value["requestable"] = _policy_bool(value.get("requestable", False))
        limits = value.get("limits") or {}
        if not isinstance(limits, dict):
            raise ApiError(0, f"The server returned invalid {name} limits.")
        limits.setdefault("max_actions_per_minute", 0)
        value["limits"] = limits
        scopes[name] = value
    for key in ("allowed_actions", "denied_actions"):
        actions = policy.get(key) or []
        if not isinstance(actions, list) or any(not isinstance(action, str) for action in actions):
            raise ApiError(0, "The server returned invalid permission action lists.")
        policy[key] = actions
    policy["default_allow"] = _policy_bool(policy.get("default_allow", False))
    return policy


def _policy_bool(value):
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        if value.lower().strip() in {"true", "yes", "1", "on", "allow", "allowed"}:
            return True
        if value.lower().strip() in {"false", "no", "0", "off", "deny", "denied"}:
            return False
    raise ApiError(0, "The server returned an invalid permission switch.")


def poll_delay(active: bool, failures: int = 0) -> int:
    """One refresh at a time, slower while unfocused, bounded offline backoff."""
    return min(60, (5 if active else 30) * 2 ** min(failures, 4))
