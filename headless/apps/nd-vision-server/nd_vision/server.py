"""HTTP front end for nd-vision-server.

Endpoints
---------
GET  /health    {"ok": true, "backend": ..., "version": ...}
POST /describe  {"image_base64": "...", "prompt": "...", "metadata": {...}}
                or {"image_path": "...", ...} when NEURO_VISION_ROOT allows it
                -> {"summary": "...", "backend": ..., "format": ..., "width": ..., "height": ...,
                    "text": "...", "warnings": [...]}

Safety
------
* Binds to 127.0.0.1 unless told otherwise. Binding to any other address refuses
  to start without NEURO_VISION_TOKEN.
* Reads files only from inside NEURO_VISION_ROOT. With no root set, the server
  never opens a path, so a caller cannot use it to read arbitrary files.
* Bodies and images have hard size and pixel limits. Base64 must decode strictly.
* Connections and analyses are bounded. Overload gets a 503 rather than another
  thread, and a request deadline stops upstream reads and OCR before it expires.
* Logs record the method, path, and status. They never record bodies or tokens.
"""

from __future__ import annotations

import base64
import binascii
import hmac
import json
import logging
import os
import socket
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Optional

from . import __version__
from .analysis import (
    MAX_IMAGE_PIXELS,
    AnalysisError,
    AnalysisTimeout,
    Analyzer,
    sniff_image,
)
from .cluster import ClusterAnalyzer, parse_workers
from .deadline import SocketDeadline

MAX_BODY_BYTES = 12 * 1024 * 1024
MAX_IMAGE_BYTES = 8 * 1024 * 1024
MAX_PROMPT_CHARS = 2000
LOOPBACK_HOSTS = {"127.0.0.1", "::1", "localhost"}

DEFAULT_MAX_CONNECTIONS = 8
DEFAULT_MAX_INFERENCE = 2
# Keep this below the bridge's 45 s client timeout and the native manager's
# 50 s observation timeout, so this server gives up first and nothing keeps
# analysing after its caller has stopped waiting.
DEFAULT_DEADLINE_SECONDS = 40.0
MAX_DEADLINE_SECONDS = 40.0
# A slow request body must not hold one of the connection slots forever.
REQUEST_READ_TIMEOUT_SECONDS = 15

log = logging.getLogger("nd-vision")


class RequestProblem(Exception):
    def __init__(self, status: int, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.message = message


class ImageProblem(RequestProblem):
    pass


def check_bind(host: str, token: str) -> None:
    """Refuses a network bind that has no token. Raises ValueError."""
    if host not in LOOPBACK_HOSTS and not token:
        raise ValueError(
            f"refusing to listen on {host} without NEURO_VISION_TOKEN: "
            "anyone on the network could use this server. Set a token, or bind to 127.0.0.1."
        )


class BoundedThreadingHTTPServer(ThreadingHTTPServer):
    """A thread-per-connection server with fixed connection and inference slots.

    The standard ThreadingHTTPServer starts a thread for every accepted socket.
    Here, extra connections get an immediate 503 instead of another thread, and
    only a small number of requests may decode images or call a model at once.
    """

    daemon_threads = True
    block_on_close = False

    def __init__(
        self,
        address,
        handler,
        *,
        max_connections: int = DEFAULT_MAX_CONNECTIONS,
        max_inference: int = DEFAULT_MAX_INFERENCE,
        deadline_seconds: float = DEFAULT_DEADLINE_SECONDS,
    ) -> None:
        if max_connections < 1 or max_inference < 1:
            raise ValueError("connection and inference limits must be at least one")
        if max_inference > max_connections:
            raise ValueError("the inference limit cannot exceed the connection limit")
        super().__init__(address, handler)
        self.connection_slots = threading.BoundedSemaphore(max_connections)
        self.inference_slots = threading.BoundedSemaphore(max_inference)
        self.max_connections = max_connections
        self.max_inference = max_inference
        self.deadline_seconds = max(1.0, min(float(deadline_seconds), MAX_DEADLINE_SECONDS))

    def process_request(self, request, client_address):
        if not self.connection_slots.acquire(blocking=False):
            self._refuse(request)
            return
        try:
            super().process_request(request, client_address)
        except BaseException:
            self.connection_slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.connection_slots.release()

    @staticmethod
    def _refuse(request) -> None:
        body = json.dumps(
            {"ok": False, "error": "the server is at its connection limit; retry shortly"}
        ).encode("utf-8")
        header = (
            b"HTTP/1.1 503 Service Unavailable\r\n"
            b"Content-Type: application/json; charset=utf-8\r\n"
            b"Cache-Control: no-store\r\n"
            b"Connection: close\r\n"
            b"Retry-After: 1\r\n"
            b"Content-Length: " + str(len(body)).encode("ascii") + b"\r\n\r\n"
        )
        try:
            request.sendall(header + body)
        except OSError:
            pass
        finally:
            try:
                request.close()
            except OSError:
                pass


class VisionHandler(BaseHTTPRequestHandler):
    server_version = f"nd-vision/{__version__}"
    protocol_version = "HTTP/1.1"
    # StreamRequestHandler applies this to the accepted socket, bounding a slow
    # or silent request body as well as an idle keep-alive connection.
    timeout = REQUEST_READ_TIMEOUT_SECONDS

    def handle_one_request(self):
        with SocketDeadline(self.connection, time.monotonic() + REQUEST_READ_TIMEOUT_SECONDS) as timer:
            self._header_deadline = timer
            super().handle_one_request()

    def log_request(self, code="-", size="-") -> None:
        log.info("%s %s %s", self.command, self.path.split("?")[0], code)

    def log_message(self, format: str, *args) -> None:  # noqa: A002 - stdlib signature
        log.debug("%s %s", self.command, self.path.split("?")[0])

    def _send(self, status: int, payload: dict, *, retry_after: Optional[str] = None) -> None:
        body = json.dumps(payload).encode("utf-8")
        try:
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            if retry_after is not None:
                self.send_header("Retry-After", retry_after)
            self.end_headers()
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True

    def _authorised(self) -> bool:
        token = self.server.token  # type: ignore[attr-defined]
        if not token:
            return True
        header = self.headers.get("Authorization", "")
        if not header.lower().startswith("bearer "):
            return False
        return hmac.compare_digest(header[7:].strip().encode("utf-8"), token.encode("utf-8"))

    def do_GET(self) -> None:  # noqa: N802 - stdlib naming
        self._header_deadline.cancel()
        path = self.path.split("?")[0]
        if path != "/health":
            self._send(404, {"ok": False, "error": "not found: use GET /health or POST /describe"})
            return
        if not self._authorised():
            self._send(401, {"ok": False, "error": "missing or wrong bearer token"})
            return
        payload = {
            "ok": True,
            "version": __version__,
            "backend": self.server.analyzer.backend,  # type: ignore[attr-defined]
            "max_image_bytes": MAX_IMAGE_BYTES,
            "max_image_pixels": MAX_IMAGE_PIXELS,
            "max_connections": self.server.max_connections,  # type: ignore[attr-defined]
            "max_inference": self.server.max_inference,  # type: ignore[attr-defined]
            "deadline_seconds": self.server.deadline_seconds,  # type: ignore[attr-defined]
            "reads_paths": self.server.image_root is not None,  # type: ignore[attr-defined]
        }
        worker_status = getattr(self.server.analyzer, "worker_status", None)  # type: ignore[attr-defined]
        if callable(worker_status):
            payload["workers"] = worker_status()
        self._send(200, payload)

    def do_POST(self) -> None:  # noqa: N802 - stdlib naming
        self._header_deadline.cancel()
        path = self.path.split("?")[0]
        if path not in ("/describe", "/"):
            self.close_connection = True
            self._send(404, {"ok": False, "error": "not found: use POST /describe"})
            return
        if not self._authorised():
            # The body is still unread; do not parse it as another request.
            self.close_connection = True
            self._send(401, {"ok": False, "error": "missing or wrong bearer token"})
            return

        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            self.close_connection = True
            self._send(400, {"ok": False, "error": "Content-Length must be a number"})
            return
        if length <= 0:
            self.close_connection = True
            self._send(400, {"ok": False, "error": "send a JSON body with image_base64 or image_path"})
            return
        if length > MAX_BODY_BYTES:
            self.close_connection = True
            self._send(413, {"ok": False, "error": f"request body is larger than {MAX_BODY_BYTES} bytes"})
            return

        started = time.monotonic()
        if not self.server.inference_slots.acquire(blocking=False):  # type: ignore[attr-defined]
            self.close_connection = True
            self._send(
                503,
                {"ok": False, "error": "the server is busy analysing other images; retry shortly"},
                retry_after="1",
            )
            return
        try:
            self._describe(started)
        finally:
            self.server.inference_slots.release()  # type: ignore[attr-defined]

    def _describe(self, started: float) -> None:
        try:
            deadline = self._deadline(started)
        except RequestProblem as problem:
            self.close_connection = True
            self._send(problem.status, {"ok": False, "error": problem.message})
            return
        length = int(self.headers.get("Content-Length") or 0)
        with SocketDeadline(self.connection, min(deadline, started + REQUEST_READ_TIMEOUT_SECONDS), socket.SHUT_RD) as timer:
            try:
                raw = self.rfile.read(length)
            except OSError:
                raw = b""
            expired = timer.expired.is_set()
        if expired or time.monotonic() >= deadline:
            self.close_connection = True
            self._send(504, {"ok": False, "error": "the request body deadline was reached"})
            return
        if len(raw) != length:
            self.close_connection = True
            self._send(400, {"ok": False, "error": "the request body is incomplete"})
            return
        try:
            payload = json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            self._send(400, {"ok": False, "error": "the body must be a JSON object"})
            return
        if not isinstance(payload, dict):
            self._send(400, {"ok": False, "error": "the body must be a JSON object"})
            return

        prompt = str(payload.get("prompt") or "")[:MAX_PROMPT_CHARS]

        try:
            data = self._image_bytes(payload)
        except RequestProblem as problem:
            self._send(problem.status, {"ok": False, "error": problem.message})
            return

        try:
            info = sniff_image(data)
        except AnalysisError:
            info = None
        if info is not None and info.width * info.height > MAX_IMAGE_PIXELS:
            self._send(
                413,
                {
                    "ok": False,
                    "error": f"the image is {info.width}x{info.height}, over the {MAX_IMAGE_PIXELS} pixel limit",
                },
            )
            return

        try:
            analysis = self.server.analyzer.describe(data, prompt, deadline=deadline)  # type: ignore[attr-defined]
        except AnalysisTimeout as exc:
            self._send(504, {"ok": False, "error": str(exc)})
            return
        except AnalysisError as exc:
            self._send(422, {"ok": False, "error": str(exc)})
            return
        except RuntimeError as exc:
            log.warning("analysis failed: %s", exc)
            self._send(502, {"ok": False, "error": str(exc)})
            return
        except Exception:  # pragma: no cover - last resort, never leak a traceback to the caller
            log.exception("unexpected analysis failure")
            self._send(500, {"ok": False, "error": "internal error; see the server log"})
            return

        self._send(
            200,
            {
                "summary": analysis.summary,
                "backend": analysis.backend,
                "format": analysis.info.format,
                "width": analysis.info.width,
                "height": analysis.info.height,
                "text": analysis.text,
                "warnings": analysis.warnings,
            },
        )

    def _deadline(self, started: float) -> float:
        limit = float(self.server.deadline_seconds)  # type: ignore[attr-defined]
        raw = self.headers.get("X-Neuro-Deadline-Ms")
        if raw is not None and raw.strip():
            try:
                requested = int(raw.strip())
            except ValueError as exc:
                raise RequestProblem(400, "X-Neuro-Deadline-Ms must be an integer number of milliseconds") from exc
            if requested <= 0:
                return started
            limit = min(limit, requested / 1000.0)
        return started + limit

    def _image_bytes(self, payload: dict) -> bytes:
        if payload.get("image_base64"):
            try:
                data = base64.b64decode(str(payload["image_base64"]), validate=True)
            except (binascii.Error, ValueError) as exc:
                raise ImageProblem(400, f"image_base64 is not valid base64: {exc}") from exc
        elif payload.get("image_path"):
            data = self._read_allowed_path(str(payload["image_path"]))
        else:
            raise ImageProblem(400, "send image_base64 (preferred) or image_path")

        if not data:
            raise ImageProblem(400, "the image is empty")
        if len(data) > MAX_IMAGE_BYTES:
            raise ImageProblem(413, f"the image is larger than {MAX_IMAGE_BYTES} bytes")
        return data

    def _read_allowed_path(self, raw_path: str) -> bytes:
        root: Optional[Path] = self.server.image_root  # type: ignore[attr-defined]
        if root is None:
            raise ImageProblem(
                403,
                "this server does not read files. Send image_base64, or set NEURO_VISION_ROOT "
                "to a directory the server may read screenshots from.",
            )
        try:
            candidate = Path(raw_path).resolve()
        except (OSError, RuntimeError) as exc:
            raise ImageProblem(400, f"image_path could not be resolved: {exc}") from exc
        if not candidate.is_relative_to(root):
            raise ImageProblem(403, "image_path is outside NEURO_VISION_ROOT")
        if not candidate.is_file():
            raise ImageProblem(404, "image_path does not name a file")
        try:
            with candidate.open("rb") as handle:
                data = handle.read(MAX_IMAGE_BYTES + 1)
        except OSError as exc:
            raise ImageProblem(404, f"image_path could not be read: {exc}") from exc
        if len(data) > MAX_IMAGE_BYTES:
            raise ImageProblem(413, f"the image is larger than {MAX_IMAGE_BYTES} bytes")
        return data


def make_server(
    host: str,
    port: int,
    analyzer: Analyzer,
    token: str = "",
    image_root: Optional[Path] = None,
    *,
    max_connections: int = DEFAULT_MAX_CONNECTIONS,
    max_inference: int = DEFAULT_MAX_INFERENCE,
    deadline_seconds: float = DEFAULT_DEADLINE_SECONDS,
) -> BoundedThreadingHTTPServer:
    check_bind(host, token)
    server = BoundedThreadingHTTPServer(
        (host, port),
        VisionHandler,
        max_connections=max_connections,
        max_inference=max_inference,
        deadline_seconds=deadline_seconds,
    )
    server.analyzer = analyzer  # type: ignore[attr-defined]
    server.token = token  # type: ignore[attr-defined]
    server.image_root = image_root.resolve() if image_root else None  # type: ignore[attr-defined]
    return server


def _env_int(environ, name: str, default: int, low: int, high: int) -> int:
    raw = environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be an integer between {low} and {high}") from exc
    if not low <= value <= high:
        raise ValueError(f"{name} must be between {low} and {high}")
    return value


def _env_float(environ, name: str, default: float, low: float, high: float) -> float:
    raw = environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be a number between {low} and {high}") from exc
    if not low <= value <= high:
        raise ValueError(f"{name} must be between {low} and {high}")
    return value


def settings_from_env(environ=os.environ) -> dict:
    root = environ.get("NEURO_VISION_ROOT", "").strip()
    return {
        "backend": environ.get("NEURO_VISION_BACKEND", "stats").strip() or "stats",
        "ollama_url": environ.get("NEURO_VISION_OLLAMA_URL", "http://127.0.0.1:11434").strip(),
        "ollama_model": environ.get("NEURO_VISION_MODEL", "").strip(),
        "openai_url": environ.get("NEURO_VISION_OPENAI_URL", "").strip(),
        "openai_model": environ.get("NEURO_VISION_MODEL", "").strip(),
        "openai_key": environ.get("NEURO_VISION_OPENAI_KEY", "").strip(),
        "token": environ.get("NEURO_VISION_TOKEN", "").strip(),
        "image_root": Path(root) if root else None,
        "workers": environ.get("NEURO_VISION_WORKERS", "").strip(),
        "worker_token": environ.get("NEURO_VISION_WORKER_TOKEN", "").strip(),
        "max_connections": _env_int(environ, "NEURO_VISION_MAX_CONNECTIONS", DEFAULT_MAX_CONNECTIONS, 1, 64),
        "max_inference": _env_int(environ, "NEURO_VISION_MAX_INFERENCE", DEFAULT_MAX_INFERENCE, 1, 8),
        "deadline_seconds": _env_float(
            environ, "NEURO_VISION_DEADLINE_SECONDS", DEFAULT_DEADLINE_SECONDS, 1.0, MAX_DEADLINE_SECONDS
        ),
    }


def main(argv=None) -> int:
    import argparse

    try:
        settings = settings_from_env()
    except ValueError as exc:
        print(f"nd-vision: {exc}", file=sys.stderr)
        return 2
    parser = argparse.ArgumentParser(prog="nd-vision", description="Vision endpoint for Neuro Desktop (NEURO_VISION_URL).")
    parser.add_argument("--host", default=os.environ.get("NEURO_VISION_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("NEURO_VISION_PORT", "8610")))
    parser.add_argument(
        "--backend",
        default=settings["backend"],
        help="size | stats | ocr | ollama | openai | cluster",
    )
    parser.add_argument("--root", default=str(settings["image_root"] or ""), help="directory the server may read image_path from")
    args = parser.parse_args(argv)

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s", stream=sys.stderr)

    try:
        if args.backend == "cluster":
            if not settings["workers"]:
                raise ValueError(
                    "backend cluster needs NEURO_VISION_WORKERS: a comma-separated list such as "
                    "primary=http://127.0.0.1:8611,backup=https://vision.example"
                )
            analyzer = ClusterAnalyzer(parse_workers(settings["workers"]), token=settings["worker_token"])
        elif settings["workers"]:
            raise ValueError("NEURO_VISION_WORKERS is set; add --backend cluster to use the worker list")
        else:
            analyzer = Analyzer(
                backend=args.backend,
                ollama_url=settings["ollama_url"],
                ollama_model=settings["ollama_model"],
                openai_url=settings["openai_url"],
                openai_model=settings["openai_model"],
                openai_key=settings["openai_key"],
            )
        server = make_server(
            args.host,
            args.port,
            analyzer,
            token=settings["token"],
            image_root=Path(args.root) if args.root else None,
            max_connections=settings["max_connections"],
            max_inference=settings["max_inference"],
            deadline_seconds=settings["deadline_seconds"],
        )
    except ValueError as exc:
        print(f"nd-vision: {exc}", file=sys.stderr)
        return 2

    host, port = server.server_address[:2]
    log.info("nd-vision %s listening on http://%s:%s (backend=%s, token=%s, reads_paths=%s)",
             __version__, host, port, analyzer.backend, "yes" if settings["token"] else "no",
             server.image_root is not None)  # type: ignore[attr-defined]
    log.info("limits: connections=%d inference=%d deadline=%.1fs image_pixels=%d",
             server.max_connections, server.max_inference, server.deadline_seconds, MAX_IMAGE_PIXELS)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
