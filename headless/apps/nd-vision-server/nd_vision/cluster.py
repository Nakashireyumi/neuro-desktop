"""A small, operator-configured forwarding backend for nd-vision-server.

Clustering here means a fixed list of vision servers that the operator names in
``NEURO_VISION_WORKERS``. There is no discovery, no model-supplied URL and no
parallel fanout: one worker receives one image, and at most one failover worker
is tried sequentially. A worker that fails is put in a short cooldown so a
dashboard poll cannot hammer it.

Plain HTTP is accepted only on loopback. Any other worker must use HTTPS,
because the request body contains a private screenshot.
"""

from __future__ import annotations

import base64
import re
import threading
import time
import urllib.parse
from dataclasses import dataclass
from typing import Dict, List, Optional, Sequence, Tuple

from .analysis import (
    ANALYSIS_TIMEOUT,
    MAX_SUMMARY_CHARS,
    Analysis,
    AnalysisError,
    AnalysisTimeout,
    _post_json,
    check_image_limits,
    sniff_image,
)

MAX_WORKERS = 8
MAX_FAILOVER_ATTEMPTS = 2
DEFAULT_COOLDOWN_SECONDS = 15.0
LOOPBACK_WORKER_HOSTS = {"127.0.0.1", "::1", "localhost"}
_NAME = re.compile(r"^[A-Za-z0-9_.-]{1,32}$")


@dataclass(frozen=True)
class VisionWorker:
    name: str
    url: str


def _worker_url(raw: str) -> str:
    parsed = urllib.parse.urlsplit(raw.strip())
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise ValueError("a Vision worker must be an http:// or https:// URL")
    if parsed.username or parsed.password:
        raise ValueError("Vision worker URLs must not contain credentials")
    if parsed.query or parsed.fragment:
        raise ValueError("Vision worker URLs must not contain a query or fragment")
    if parsed.path not in ("", "/", "/describe"):
        raise ValueError("Vision worker URLs must point at the host or /describe")
    try:
        parsed.port
    except ValueError as exc:
        raise ValueError("a Vision worker URL has an invalid port") from exc
    if parsed.scheme == "http" and (parsed.hostname or "").lower() not in LOOPBACK_WORKER_HOSTS:
        raise ValueError("a non-loopback Vision worker must use https:// so the screenshot is encrypted")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "/describe", "", ""))


def parse_workers(raw: str) -> Tuple[VisionWorker, ...]:
    """Parse ``name=url,name=url`` or a bare comma-separated URL list."""
    workers: List[VisionWorker] = []
    names = set()
    for index, item in enumerate((raw or "").split(","), start=1):
        item = item.strip()
        if not item:
            continue
        if "=" in item:
            name, value = (part.strip() for part in item.split("=", 1))
        else:
            name, value = f"worker-{index}", item
        if not _NAME.fullmatch(name):
            raise ValueError("a Vision worker name must use 1-32 letters, digits, dots, dashes or underscores")
        if name in names:
            raise ValueError("Vision worker names must be unique")
        names.add(name)
        workers.append(VisionWorker(name=name, url=_worker_url(value)))
    if not workers:
        raise ValueError("NEURO_VISION_WORKERS must list at least one worker, for example primary=http://127.0.0.1:8611")
    if len(workers) > MAX_WORKERS:
        raise ValueError(f"NEURO_VISION_WORKERS lists {len(workers)} workers; at most {MAX_WORKERS} are supported")
    return tuple(workers)


def _summary_text(result: dict) -> str:
    summary = result.get("summary")
    if not isinstance(summary, str) or not summary.strip():
        raise RuntimeError("the Vision worker answered without a summary")
    return summary.strip()[:MAX_SUMMARY_CHARS]


class ClusterAnalyzer:
    """Sequential failover across a fixed, operator-approved worker list."""

    backend = "cluster"

    def __init__(
        self,
        workers: Sequence[VisionWorker],
        token: str = "",
        cooldown_seconds: float = DEFAULT_COOLDOWN_SECONDS,
        max_attempts: int = MAX_FAILOVER_ATTEMPTS,
    ) -> None:
        if not workers:
            raise ValueError("at least one Vision worker is required")
        if max_attempts < 1:
            raise ValueError("max_attempts must be at least one")
        self.workers = tuple(workers)
        self.token = token
        self.cooldown_seconds = max(0.0, float(cooldown_seconds))
        self.max_attempts = min(int(max_attempts), len(self.workers), MAX_FAILOVER_ATTEMPTS)
        self._lock = threading.Lock()
        self._cooldowns: Dict[str, float] = {}
        self._cursor = 0

    def worker_status(self) -> List[dict]:
        """Names and availability only. URLs and tokens stay in operator config."""
        now = time.monotonic()
        with self._lock:
            return [
                {
                    "name": worker.name,
                    "available": self._cooldowns.get(worker.name, 0.0) <= now,
                    "cooldown_seconds": max(0, round(self._cooldowns.get(worker.name, 0.0) - now)),
                }
                for worker in self.workers
            ]

    def _available(self, now: float) -> List[VisionWorker]:
        with self._lock:
            rotated = self.workers[self._cursor :] + self.workers[: self._cursor]
            return [worker for worker in rotated if self._cooldowns.get(worker.name, 0.0) <= now]

    def _failed(self, worker: VisionWorker) -> None:
        with self._lock:
            self._cooldowns[worker.name] = time.monotonic() + self.cooldown_seconds

    def _succeeded(self, worker: VisionWorker) -> None:
        with self._lock:
            self._cooldowns.pop(worker.name, None)
            index = self.workers.index(worker)
            self._cursor = (index + 1) % len(self.workers)

    def describe(self, data: bytes, prompt: str = "", deadline: Optional[float] = None) -> Analysis:
        info = sniff_image(data)
        check_image_limits(info)
        if deadline is None:
            deadline = time.monotonic() + ANALYSIS_TIMEOUT
        if deadline <= time.monotonic():
            raise AnalysisTimeout("the analysis deadline was reached")

        available = self._available(time.monotonic())
        if not available:
            raise RuntimeError("all Vision workers are cooling down after failures; retry shortly")

        image_b64 = None
        errors: List[str] = []
        for worker in available[: self.max_attempts]:
            left = deadline - time.monotonic()
            if left <= 0:
                raise AnalysisTimeout("the analysis deadline was reached")

            try:
                if image_b64 is None:
                    image_b64 = base64.b64encode(data).decode("ascii")
                result = _post_json(
                    worker.url,
                    {"image_base64": image_b64, "prompt": prompt, "metadata": {"source": "nd-vision-cluster"}},
                    headers={"Authorization": f"Bearer {self.token}"} if self.token else None,
                    timeout=left,
                )
                summary = _summary_text(result)
            except (AnalysisError, AnalysisTimeout):
                raise
            except Exception as exc:
                if deadline - time.monotonic() <= 0:
                    raise AnalysisTimeout("the analysis deadline was reached") from exc
                reason = str(exc)[:200] or exc.__class__.__name__
                self._failed(worker)
                errors.append(f"{worker.name}: {reason}")
                continue

            self._succeeded(worker)
            return Analysis(summary, f"cluster/{worker.name}", info, text=summary)

        raise RuntimeError("no Vision worker answered: " + "; ".join(errors))
