"""Tests for nd-vision-server. They run the real HTTP server on an ephemeral port.

Run from desktop/apps/nd-vision-server:  python3 -m unittest discover -s tests -t .
"""

from __future__ import annotations

import base64
import io
import json
import os
import struct
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from nd_vision import server as vision_server
from nd_vision.analysis import AnalysisError, Analyzer, sniff_image

try:
    import PIL  # noqa: F401

    HAVE_PILLOW = True
except ImportError:  # pragma: no cover - depends on the machine
    HAVE_PILLOW = False


def png_bytes(width: int, height: int, rgb=(255, 0, 0)) -> bytes:
    """A solid-colour RGB PNG built by hand, so the tests do not need Pillow."""
    raw = b"".join(b"\x00" + bytes(rgb) * width for _ in range(height))

    def chunk(kind: bytes, data: bytes) -> bytes:
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    header = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


class Running:
    """Starts a vision server on an ephemeral port for the duration of a test."""

    def __init__(self, analyzer=None, token="", image_root=None):
        self.server = vision_server.make_server(
            "127.0.0.1",
            0,
            analyzer or Analyzer(backend="size"),
            token=token,
            image_root=image_root,
        )
        self.base = "http://127.0.0.1:%d" % self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.server.shutdown()
        self.server.server_close()

    def call(self, method, path, body=None, headers=None):
        data = None
        if body is not None:
            data = body if isinstance(body, bytes) else json.dumps(body).encode("utf-8")
        request = urllib.request.Request(self.base + path, data=data, method=method)
        request.add_header("Content-Type", "application/json")
        for key, value in (headers or {}).items():
            request.add_header(key, value)
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                return response.status, json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            return exc.code, json.loads(exc.read().decode("utf-8") or "{}")


class FakeUpstream:
    """A stand-in for Ollama or an OpenAI-compatible server. It records requests."""

    def __init__(self, reply: dict, status: int = 200):
        self.requests = []
        upstream = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):  # keep test output quiet
                pass

            def do_POST(self):  # noqa: N802
                length = int(self.headers.get("Content-Length") or 0)
                upstream.requests.append((self.path, json.loads(self.rfile.read(length) or b"{}"), dict(self.headers)))
                body = json.dumps(reply).encode("utf-8")
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = "http://127.0.0.1:%d" % self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.server.shutdown()
        self.server.server_close()


class HealthAndDescribeTests(unittest.TestCase):
    def test_health_reports_backend_and_that_paths_are_not_read(self):
        with Running() as running:
            status, body = running.call("GET", "/health")
        self.assertEqual(status, 200)
        self.assertTrue(body["ok"])
        self.assertEqual(body["backend"], "size")
        self.assertFalse(body["reads_paths"], "no NEURO_VISION_ROOT means the server reads no paths")

    def test_describe_base64_png_reports_size(self):
        image = base64.b64encode(png_bytes(16, 8)).decode("ascii")
        with Running() as running:
            status, body = running.call("POST", "/describe", {"image_base64": image, "prompt": "what is this"})
        self.assertEqual(status, 200)
        self.assertIn("16x8", body["summary"])
        self.assertEqual(body["format"], "PNG")
        self.assertEqual((body["width"], body["height"]), (16, 8))

    @unittest.skipUnless(HAVE_PILLOW, "Pillow is needed for colour statistics")
    def test_stats_backend_describes_colour(self):
        image = base64.b64encode(png_bytes(32, 32, (255, 0, 0))).decode("ascii")
        analyzer = Analyzer(backend="stats")
        with Running(analyzer=analyzer) as running:
            status, body = running.call("POST", "/describe", {"image_base64": image})
        self.assertEqual(status, 200)
        self.assertEqual(body["backend"], "stats")
        self.assertIn("#f00000", body["summary"], "a pure red image must report the red bucket")
        self.assertIn("fairly dark", body["summary"], "pure red has mean brightness 76")

    def test_invalid_base64_is_400(self):
        with Running() as running:
            status, body = running.call("POST", "/describe", {"image_base64": "%%%not base64%%%"})
        self.assertEqual(status, 400)
        self.assertIn("base64", body["error"])

    def test_non_image_bytes_are_422(self):
        junk = base64.b64encode(b"this is not an image at all, sorry").decode("ascii")
        with Running() as running:
            status, body = running.call("POST", "/describe", {"image_base64": junk})
        self.assertEqual(status, 422)
        self.assertIn("unsupported", body["error"])

    def test_bad_json_and_missing_image_are_400(self):
        with Running() as running:
            status, _ = running.call("POST", "/describe", b"{not json")
            self.assertEqual(status, 400)
            status, body = running.call("POST", "/describe", {"prompt": "no image here"})
            self.assertEqual(status, 400)
            self.assertIn("image_base64", body["error"])

    def test_unknown_route_is_404(self):
        with Running() as running:
            status, _ = running.call("GET", "/nope")
        self.assertEqual(status, 404)

    def test_body_size_limit(self):
        original = vision_server.MAX_BODY_BYTES
        vision_server.MAX_BODY_BYTES = 500
        try:
            with Running() as running:
                status, body = running.call("POST", "/describe", {"image_base64": "A" * 2000})
        finally:
            vision_server.MAX_BODY_BYTES = original
        self.assertEqual(status, 413)
        self.assertIn("larger", body["error"])


class PathReadingTests(unittest.TestCase):
    def test_paths_are_refused_without_a_root(self):
        with tempfile.TemporaryDirectory() as tmp:
            image = Path(tmp) / "shot.png"
            image.write_bytes(png_bytes(4, 4))
            with Running() as running:
                status, body = running.call("POST", "/describe", {"image_path": str(image)})
        self.assertEqual(status, 403)
        self.assertIn("image_base64", body["error"])

    def test_paths_inside_the_root_are_read_and_others_refused(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as elsewhere:
            inside = Path(root) / "shot.png"
            inside.write_bytes(png_bytes(5, 3))
            outside = Path(elsewhere) / "secret.png"
            outside.write_bytes(png_bytes(9, 9))
            with Running(image_root=Path(root)) as running:
                status, body = running.call("POST", "/describe", {"image_path": str(inside)})
                self.assertEqual(status, 200)
                self.assertIn("5x3", body["summary"])

                status, body = running.call("POST", "/describe", {"image_path": str(outside)})
                self.assertEqual(status, 403)

                traversal = str(Path(root) / ".." / Path(elsewhere).name / "secret.png")
                status, _ = running.call("POST", "/describe", {"image_path": traversal})
                self.assertEqual(status, 403)

    def test_symlink_out_of_the_root_is_refused(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as elsewhere:
            target = Path(elsewhere) / "secret.png"
            target.write_bytes(png_bytes(2, 2))
            link = Path(root) / "link.png"
            try:
                os.symlink(target, link)
            except (OSError, NotImplementedError):
                self.skipTest("symlinks are not available on this machine")
            with Running(image_root=Path(root)) as running:
                status, _ = running.call("POST", "/describe", {"image_path": str(link)})
        self.assertEqual(status, 403)


class AuthTests(unittest.TestCase):
    def test_token_is_required_when_set(self):
        image = base64.b64encode(png_bytes(2, 2)).decode("ascii")
        with Running(token="s3cret") as running:
            self.assertEqual(running.call("GET", "/health")[0], 401)
            self.assertEqual(running.call("GET", "/health", headers={"Authorization": "Bearer nope"})[0], 401)
            self.assertEqual(running.call("GET", "/health", headers={"Authorization": "Bearer s3cret"})[0], 200)
            self.assertEqual(running.call("POST", "/describe", {"image_base64": image})[0], 401)
            self.assertEqual(
                running.call("POST", "/describe", {"image_base64": image}, headers={"Authorization": "Bearer s3cret"})[0],
                200,
            )

    def test_network_bind_needs_a_token(self):
        with self.assertRaises(ValueError):
            vision_server.check_bind("0.0.0.0", "")
        vision_server.check_bind("0.0.0.0", "a-token")
        vision_server.check_bind("127.0.0.1", "")
        vision_server.check_bind("localhost", "")


class ModelBackendTests(unittest.TestCase):
    def test_ollama_backend_sends_the_image_and_returns_its_answer(self):
        with FakeUpstream({"response": "a cat asleep on a keyboard"}) as upstream:
            analyzer = Analyzer(backend="ollama", ollama_url=upstream.url, ollama_model="llava")
            image = png_bytes(3, 3)
            analysis = analyzer.describe(image, "what is this")
        self.assertEqual(analysis.summary, "a cat asleep on a keyboard")
        path, sent, _ = upstream.requests[0]
        self.assertEqual(path, "/api/generate")
        self.assertEqual(sent["model"], "llava")
        self.assertEqual(base64.b64decode(sent["images"][0]), image)

    def test_openai_backend_uses_the_chat_endpoint_and_the_key(self):
        reply = {"choices": [{"message": {"content": "a terminal with green text"}}]}
        with FakeUpstream(reply) as upstream:
            analyzer = Analyzer(backend="openai", openai_url=upstream.url, openai_model="qwen-vl", openai_key="k-1")
            analysis = analyzer.describe(png_bytes(3, 3), "what is this")
        self.assertEqual(analysis.summary, "a terminal with green text")
        path, sent, headers = upstream.requests[0]
        self.assertEqual(path, "/v1/chat/completions")
        self.assertTrue(sent["messages"][0]["content"][1]["image_url"]["url"].startswith("data:image/png;base64,"))
        self.assertEqual(headers.get("Authorization"), "Bearer k-1")

    def test_unreachable_model_is_502_not_a_fake_answer(self):
        analyzer = Analyzer(backend="ollama", ollama_url="http://127.0.0.1:9", ollama_model="llava")
        with Running(analyzer=analyzer) as running:
            status, body = running.call("POST", "/describe", {"image_base64": base64.b64encode(png_bytes(2, 2)).decode()})
        self.assertEqual(status, 502)
        self.assertIn("not reachable", body["error"])

    def test_ocr_missing_degrades_with_a_warning(self):
        def no_tesseract(_data):
            raise RuntimeError("tesseract is not installed")

        analyzer = Analyzer(backend="ocr", ocr=no_tesseract)
        analysis = analyzer.describe(png_bytes(6, 6))
        self.assertTrue(analysis.summary)
        expected = "OCR unavailable" if HAVE_PILLOW else "Pillow"
        self.assertTrue(any(expected in w for w in analysis.warnings), analysis.warnings)

    def test_unknown_backend_is_rejected(self):
        with self.assertRaises(ValueError):
            Analyzer(backend="magic")


class SniffTests(unittest.TestCase):
    def test_png_header(self):
        info = sniff_image(png_bytes(640, 480))
        self.assertEqual((info.format, info.width, info.height), ("PNG", 640, 480))

    @unittest.skipUnless(HAVE_PILLOW, "Pillow is needed to make JPEG and WEBP samples")
    def test_jpeg_and_webp_headers(self):
        from PIL import Image

        for fmt, name in (("JPEG", "jpeg"), ("WEBP", "webp")):
            buffer = io.BytesIO()
            Image.new("RGB", (37, 21), (10, 200, 30)).save(buffer, format=fmt)
            info = sniff_image(buffer.getvalue())
            self.assertEqual((info.format, info.width, info.height), (fmt, 37, 21), name)

    def test_corrupt_data_raises(self):
        with self.assertRaises(AnalysisError):
            sniff_image(b"\x89PNG\r\n\x1a\nshort")


if __name__ == "__main__":
    unittest.main()
