"""Image analysis backends for nd-vision-server.

Every backend turns one image (raw bytes plus a prompt) into a plain-text summary.
The bridge reads the ``summary`` field, so summaries are written to be read by a
language model that cannot see the picture: concrete, short, and honest about
what was and was not determined.

Backends
--------
size     Reads only the image header: format and pixel size. Needs no dependency.
stats    size plus colour statistics (mean brightness, dominant colours). Needs Pillow.
ocr      stats plus text recognised by the ``tesseract`` command, when it is installed.
ollama   Asks a local Ollama server (``/api/generate``) with the image attached.
openai   Asks an OpenAI-compatible chat endpoint (llama.cpp, LM Studio, vLLM, ...).

A backend that cannot run reports why in ``warnings`` and falls back to ``size``,
so a missing optional piece never produces a silent empty answer.
"""

from __future__ import annotations

import base64
import io
import json
import shutil
import struct
import subprocess
import time
import urllib.error
import urllib.request
import warnings
from dataclasses import dataclass, field
from typing import Callable, Dict, List, Optional

BACKENDS = ("size", "stats", "ocr", "ollama", "openai")

# A hard ceiling on how long any single analysis may run, in seconds. The HTTP
# server applies a shorter deadline so its clients stop before this ceiling.
ANALYSIS_TIMEOUT = 60

# A decoded image is three bytes per pixel in Pillow. This limit admits an 8K
# desktop (7680x4320) but refuses decompression bombs and multi-screen stitches
# that would exhaust memory before any useful summary is possible.
MAX_IMAGE_PIXELS = 40_000_000

# Upstream model answers are prose, not images. Bound them so a broken or
# hostile endpoint cannot make this process hold or return an arbitrary blob.
MAX_UPSTREAM_RESPONSE_BYTES = 1 << 20
MAX_SUMMARY_CHARS = 16_000


class AnalysisError(Exception):
    """The image could not be read at all (not a supported format, or empty)."""


class AnalysisTimeout(RuntimeError):
    """The caller's analysis deadline expired before a result was ready."""


@dataclass
class ImageInfo:
    format: str
    width: int
    height: int


@dataclass
class Analysis:
    summary: str
    backend: str
    info: ImageInfo
    text: str = ""
    warnings: List[str] = field(default_factory=list)


def sniff_image(data: bytes) -> ImageInfo:
    """Reads format and size from the header. It never decodes pixels."""
    if len(data) >= 24 and data[:8] == b"\x89PNG\r\n\x1a\n" and data[12:16] == b"IHDR":
        width, height = struct.unpack(">II", data[16:24])
        return ImageInfo("PNG", width, height)

    if len(data) >= 10 and data[:6] in (b"GIF87a", b"GIF89a"):
        width, height = struct.unpack("<HH", data[6:10])
        return ImageInfo("GIF", width, height)

    if len(data) >= 4 and data[:2] == b"\xff\xd8":
        index = 2
        while index + 9 < len(data):
            if data[index] != 0xFF:
                index += 1
                continue
            marker = data[index + 1]
            if marker in (0xC0, 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7, 0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF):
                height, width = struct.unpack(">HH", data[index + 5 : index + 9])
                return ImageInfo("JPEG", width, height)
            segment_length = struct.unpack(">H", data[index + 2 : index + 4])[0]
            index += 2 + segment_length
        raise AnalysisError("JPEG header has no frame size")

    if len(data) >= 30 and data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        chunk = data[12:16]
        if chunk == b"VP8X":
            width = 1 + int.from_bytes(data[24:27], "little")
            height = 1 + int.from_bytes(data[27:30], "little")
            return ImageInfo("WEBP", width, height)
        if chunk == b"VP8L" and data[20] == 0x2F:
            bits = int.from_bytes(data[21:25], "little")
            width = (bits & 0x3FFF) + 1
            height = ((bits >> 14) & 0x3FFF) + 1
            return ImageInfo("WEBP", width, height)
        if chunk == b"VP8 " and data[23:26] == b"\x9d\x01\x2a":
            width = int.from_bytes(data[26:28], "little") & 0x3FFF
            height = int.from_bytes(data[28:30], "little") & 0x3FFF
            return ImageInfo("WEBP", width, height)

    raise AnalysisError("unsupported or corrupt image (expected PNG, JPEG, GIF or WEBP)")


def check_image_limits(info: ImageInfo) -> None:
    """Refuses an empty or memory-hungry image before any pixel decode."""
    if info.width <= 0 or info.height <= 0:
        raise AnalysisError("the image header reports an empty image")
    pixels = info.width * info.height
    if pixels > MAX_IMAGE_PIXELS:
        raise AnalysisError(
            f"the image is {info.width}x{info.height} ({pixels} pixels), "
            f"over the {MAX_IMAGE_PIXELS} pixel limit"
        )


def _pillow_image(data: bytes):
    try:
        from PIL import Image  # type: ignore
    except ImportError:
        return None, "Pillow is not installed: colour statistics are unavailable (pip install pillow)"
    Image.MAX_IMAGE_PIXELS = MAX_IMAGE_PIXELS
    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            image = Image.open(io.BytesIO(data))
            image.load()
            return image.convert("RGB"), ""
    except Exception as exc:  # Pillow raises several types for corrupt files
        raise AnalysisError(f"the image could not be decoded: {exc}") from exc


def colour_summary(image) -> Dict[str, object]:
    """Mean brightness (0-255) and the most common colours, quantised to 4 bits."""
    small = image.copy()
    small.thumbnail((128, 128))
    accessor = getattr(small, "get_flattened_data", None) or small.getdata
    pixels = list(accessor())
    if not pixels:
        return {"brightness": 0, "dominant": []}

    brightness = sum((r * 299 + g * 587 + b * 114) // 1000 for r, g, b in pixels) // len(pixels)

    buckets: Dict[tuple, int] = {}
    for r, g, b in pixels:
        key = (r & 0xF0, g & 0xF0, b & 0xF0)
        buckets[key] = buckets.get(key, 0) + 1
    ranked = sorted(buckets.items(), key=lambda item: item[1], reverse=True)[:3]
    dominant = ["#%02x%02x%02x" % key for key, _ in ranked]
    return {"brightness": brightness, "dominant": dominant}


def brightness_word(value: int) -> str:
    if value < 64:
        return "mostly dark"
    if value < 128:
        return "fairly dark"
    if value < 192:
        return "fairly bright"
    return "mostly bright"


def run_tesseract(data: bytes, timeout: float = 20) -> str:
    """OCR through the tesseract CLI. Arguments are a list, never a shell string."""
    binary = shutil.which("tesseract")
    if binary is None:
        raise RuntimeError("tesseract is not installed (for example: apt install tesseract-ocr)")
    completed = subprocess.run(
        [binary, "stdin", "stdout", "--psm", "11"],
        input=data,
        capture_output=True,
        timeout=max(0.1, min(float(timeout), 20.0)),
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError("tesseract failed: " + completed.stderr.decode("utf-8", "replace").strip()[:200])
    return " ".join(completed.stdout.decode("utf-8", "replace").split())


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    """A redirected POST could forward a private image to another host."""

    def redirect_request(self, request, fp, code, message, headers, new_url):
        raise RuntimeError(f"upstream redirected the request ({code}); refusing to forward the image")


def _post_json(url: str, payload: dict, headers: Optional[Dict[str, str]] = None, timeout: float = ANALYSIS_TIMEOUT) -> dict:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(url, data=body, method="POST")
    request.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    opener = urllib.request.build_opener(_NoRedirect())
    try:
        with opener.open(request, timeout=max(0.1, min(float(timeout), ANALYSIS_TIMEOUT))) as response:
            raw = response.read(MAX_UPSTREAM_RESPONSE_BYTES + 1)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"upstream returned {exc.code}") from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise RuntimeError(f"upstream not reachable: {exc}") from exc
    if len(raw) > MAX_UPSTREAM_RESPONSE_BYTES:
        raise RuntimeError(f"upstream response exceeds {MAX_UPSTREAM_RESPONSE_BYTES} bytes")
    try:
        decoded = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise RuntimeError("upstream returned invalid JSON") from exc
    if not isinstance(decoded, dict):
        raise RuntimeError("upstream returned an unexpected response")
    return decoded


def ask_ollama(base_url: str, model: str, prompt: str, image_b64: str, timeout: float = ANALYSIS_TIMEOUT) -> str:
    result = _post_json(
        base_url.rstrip("/") + "/api/generate",
        {"model": model, "prompt": prompt, "images": [image_b64], "stream": False},
        timeout=timeout,
    )
    text = result.get("response")
    if not isinstance(text, str) or not text.strip():
        raise RuntimeError("ollama answered without a response")
    return text.strip()


def ask_openai_compatible(
    base_url: str,
    model: str,
    prompt: str,
    mime: str,
    image_b64: str,
    api_key: str = "",
    timeout: float = ANALYSIS_TIMEOUT,
) -> str:
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    result = _post_json(
        base_url.rstrip("/") + "/v1/chat/completions",
        {
            "model": model,
            "max_tokens": 300,
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {"type": "text", "text": prompt},
                        {"type": "image_url", "image_url": {"url": f"data:{mime};base64,{image_b64}"}},
                    ],
                }
            ],
        },
        headers=headers,
        timeout=timeout,
    )
    try:
        text = result["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError) as exc:
        raise RuntimeError("the chat endpoint answered without a message") from exc
    if not isinstance(text, str) or not text.strip():
        raise RuntimeError("the chat endpoint answered with an empty message")
    return text.strip()


class Analyzer:
    """Turns images into summaries with one configured backend.

    Configuration comes from the constructor, which the server fills from the
    environment (see server.py). Nothing here reads the environment directly, so
    tests can build an Analyzer with any settings.
    """

    def __init__(
        self,
        backend: str = "stats",
        ollama_url: str = "http://127.0.0.1:11434",
        ollama_model: str = "",
        openai_url: str = "",
        openai_model: str = "",
        openai_key: str = "",
        ocr: Optional[Callable[[bytes], str]] = None,
    ) -> None:
        if backend not in BACKENDS:
            raise ValueError(f"unknown backend {backend!r} (use one of: {', '.join(BACKENDS)})")
        self.backend = backend
        self.ollama_url = ollama_url
        self.ollama_model = ollama_model
        self.openai_url = openai_url
        self.openai_model = openai_model
        self.openai_key = openai_key
        self.ocr = ocr or run_tesseract

    def describe(self, data: bytes, prompt: str = "", deadline: Optional[float] = None) -> Analysis:
        """Analyse one image before ``deadline`` (a ``time.monotonic`` value).

        Network reads and the OCR subprocess are given only the time that is
        left. A caller that has already timed out gets AnalysisTimeout instead
        of a result that nobody is waiting for.
        """
        if deadline is None:
            deadline = time.monotonic() + ANALYSIS_TIMEOUT

        def remaining() -> float:
            left = deadline - time.monotonic()
            if left <= 0:
                raise AnalysisTimeout("the analysis deadline was reached")
            return min(left, float(ANALYSIS_TIMEOUT))

        remaining()
        info = sniff_image(data)
        check_image_limits(info)
        prompt = prompt.strip() or "Describe what is on this screen for an AI that cannot see it. Be concrete and brief."
        warnings: List[str] = []

        if self.backend in ("size",):
            return Analysis(self._size_summary(info), "size", info, warnings=warnings)

        if self.backend in ("stats", "ocr"):
            remaining()
            image, note = _pillow_image(data)
            remaining()
            if note:
                warnings.append(note)
                return Analysis(self._size_summary(info), "size", info, warnings=warnings)
            stats = colour_summary(image)
            summary = self._stats_summary(info, stats)
            text = ""
            if self.backend == "ocr":
                try:
                    if self.ocr is run_tesseract:
                        text = self.ocr(data, timeout=min(20.0, remaining()))
                    else:
                        text = self.ocr(data)
                except AnalysisTimeout:
                    raise
                except Exception as exc:  # any OCR failure degrades to stats
                    warnings.append(f"OCR unavailable: {str(exc)[:300]}")
                remaining()
                if text:
                    summary += f" Text on screen: {text[:1200]}"
            return Analysis(summary, self.backend, info, text=text, warnings=warnings)

        if self.backend == "ollama":
            if not self.ollama_model:
                raise RuntimeError("NEURO_VISION_MODEL must name the Ollama model")
            image_b64 = base64.b64encode(data).decode("ascii")
            try:
                text = ask_ollama(self.ollama_url, self.ollama_model, prompt, image_b64, timeout=remaining())
            except RuntimeError as exc:
                if deadline - time.monotonic() <= 0:
                    raise AnalysisTimeout("the analysis deadline was reached") from exc
                raise
            remaining()
            text = text[:MAX_SUMMARY_CHARS]
            return Analysis(text, "ollama", info, text=text, warnings=warnings)

        if self.backend == "openai":
            if not self.openai_url or not self.openai_model:
                raise RuntimeError("NEURO_VISION_OPENAI_URL and NEURO_VISION_MODEL must be set")
            mime = {"PNG": "image/png", "JPEG": "image/jpeg", "GIF": "image/gif", "WEBP": "image/webp"}[info.format]
            image_b64 = base64.b64encode(data).decode("ascii")
            try:
                text = ask_openai_compatible(
                    self.openai_url, self.openai_model, prompt, mime, image_b64, self.openai_key, timeout=remaining()
                )
            except RuntimeError as exc:
                if deadline - time.monotonic() <= 0:
                    raise AnalysisTimeout("the analysis deadline was reached") from exc
                raise
            remaining()
            text = text[:MAX_SUMMARY_CHARS]
            return Analysis(text, "openai", info, text=text, warnings=warnings)

        raise AssertionError("unreachable: backend validated in __init__")

    @staticmethod
    def _size_summary(info: ImageInfo) -> str:
        return f"A {info.format} screenshot, {info.width}x{info.height} pixels. Image content is not analysed by this backend."

    @staticmethod
    def _stats_summary(info: ImageInfo, stats: Dict[str, object]) -> str:
        brightness = int(stats["brightness"])  # type: ignore[arg-type]
        dominant = ", ".join(stats["dominant"])  # type: ignore[arg-type]
        return (
            f"A {info.format} screenshot, {info.width}x{info.height} pixels, {brightness_word(brightness)} "
            f"(mean brightness {brightness}/255). Dominant colours: {dominant or 'none'}. "
            "Shapes and text are not identified by this backend."
        )
