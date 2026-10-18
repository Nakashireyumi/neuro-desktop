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
* Bodies and images have hard size limits. Base64 must decode strictly.
* Logs record the method, path, and status. They never record bodies or tokens.
"""

from __future__ import annotations

import base64
import binascii
import hmac
import json
import logging
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Optional

from . import __version__
from .analysis import AnalysisError, Analyzer

MAX_BODY_BYTES = 12 * 1024 * 1024
MAX_IMAGE_BYTES = 8 * 1024 * 1024
MAX_PROMPT_CHARS = 2000
LOOPBACK_HOSTS = {"127.0.0.1", "::1", "localhost"}

log = logging.getLogger("nd-vision")


class ImageProblem(Exception):
    def __init__(self, status: int, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.message = message


def check_bind(host: str, token: str) -> None:
    """Refuses a network bind that has no token. Raises ValueError."""
    if host not in LOOPBACK_HOSTS and not token:
        raise ValueError(
            f"refusing to listen on {host} without NEURO_VISION_TOKEN: "
            "anyone on the network could use this server. Set a token, or bind to 127.0.0.1."
        )


class VisionHandler(BaseHTTPRequestHandler):
    server_version = f"nd-vision/{__version__}"
    protocol_version = "HTTP/1.1"

    # The stdlib logs every request to stderr with headers; route it through our logger.
    def log_message(self, format: str, *args) -> None:  # noqa: A002 - stdlib signature
        log.info("%s %s", self.command, self.path.split("?")[0])

    def _send(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _authorised(self) -> bool:
        token = self.server.token  # type: ignore[attr-defined]
        if not token:
            return True
        header = self.headers.get("Authorization", "")
        if not header.lower().startswith("bearer "):
            return False
        return hmac.compare_digest(header[7:].strip().encode("utf-8"), token.encode("utf-8"))

    def do_GET(self) -> None:  # noqa: N802 - stdlib naming
        path = self.path.split("?")[0]
        if path != "/health":
            self._send(404, {"ok": False, "error": "not found: use GET /health or POST /describe"})
            return
        if not self._authorised():
            self._send(401, {"ok": False, "error": "missing or wrong bearer token"})
            return
        self._send(
            200,
            {
                "ok": True,
                "version": __version__,
                "backend": self.server.analyzer.backend,  # type: ignore[attr-defined]
                "max_image_bytes": MAX_IMAGE_BYTES,
                "reads_paths": self.server.image_root is not None,  # type: ignore[attr-defined]
            },
        )

    def do_POST(self) -> None:  # noqa: N802 - stdlib naming
        path = self.path.split("?")[0]
        if path not in ("/describe", "/"):
            self._send(404, {"ok": False, "error": "not found: use POST /describe"})
            return
        if not self._authorised():
            self._send(401, {"ok": False, "error": "missing or wrong bearer token"})
            return

        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            self._send(400, {"ok": False, "error": "Content-Length must be a number"})
            return
        if length <= 0:
            self._send(400, {"ok": False, "error": "send a JSON body with image_base64 or image_path"})
            return
        if length > MAX_BODY_BYTES:
            self.close_connection = True
            self._send(413, {"ok": False, "error": f"request body is larger than {MAX_BODY_BYTES} bytes"})
            return

        raw = self.rfile.read(length)
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
        except ImageProblem as problem:
            self._send(problem.status, {"ok": False, "error": problem.message})
            return

        try:
            analysis = self.server.analyzer.describe(data, prompt)  # type: ignore[attr-defined]
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
        if candidate.stat().st_size > MAX_IMAGE_BYTES:
            raise ImageProblem(413, f"the image is larger than {MAX_IMAGE_BYTES} bytes")
        return candidate.read_bytes()


def make_server(
    host: str,
    port: int,
    analyzer: Analyzer,
    token: str = "",
    image_root: Optional[Path] = None,
) -> ThreadingHTTPServer:
    check_bind(host, token)
    server = ThreadingHTTPServer((host, port), VisionHandler)
    server.analyzer = analyzer  # type: ignore[attr-defined]
    server.token = token  # type: ignore[attr-defined]
    server.image_root = image_root.resolve() if image_root else None  # type: ignore[attr-defined]
    return server


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
    }


def main(argv=None) -> int:
    import argparse

    settings = settings_from_env()
    parser = argparse.ArgumentParser(prog="nd-vision", description="Vision endpoint for Neuro Desktop (NEURO_VISION_URL).")
    parser.add_argument("--host", default=os.environ.get("NEURO_VISION_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("NEURO_VISION_PORT", "8610")))
    parser.add_argument("--backend", default=settings["backend"], help="size | stats | ocr | ollama | openai")
    parser.add_argument("--root", default=str(settings["image_root"] or ""), help="directory the server may read image_path from")
    args = parser.parse_args(argv)

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s", stream=sys.stderr)

    try:
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
        )
    except ValueError as exc:
        print(f"nd-vision: {exc}", file=sys.stderr)
        return 2

    host, port = server.server_address[:2]
    log.info("nd-vision %s listening on http://%s:%s (backend=%s, token=%s, reads_paths=%s)",
             __version__, host, port, analyzer.backend, "yes" if settings["token"] else "no",
             server.image_root is not None)  # type: ignore[attr-defined]
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
