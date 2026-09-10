"""Ollama brain for choosing Neuro actions (RWKV7 / BlinkDL architecture via heredos).

Uses Ollama tool-calling when available (heredos/rwkv7 ships tool templates),
with a JSON-fallback prompt if the model returns plain text instead of tools.

Tuned for modest CPU laptops (e.g. Dell Latitude E7490): short generations,
long HTTP timeouts, and keep_alive so the model stays warm between actions.
"""

from __future__ import annotations

import json
import logging
import re
from dataclasses import dataclass
from typing import Any, Optional, Sequence

import aiohttp

log = logging.getLogger("neuro-ollama.brain")


@dataclass
class ActionChoice:
    name: str
    data: Optional[dict[str, Any]]
    reason: str = ""


class OllamaBrain:
    def __init__(
        self,
        base_url: str = "http://127.0.0.1:11434",
        model: str = "heredos/rwkv7:2.9b",
        temperature: float = 0.4,
        timeout_seconds: float = 600.0,
        num_predict: int = 96,
        keep_alive: str = "30m",
    ):
        self.base_url = base_url.rstrip("/")
        self.model = model
        self.temperature = temperature
        self.timeout_seconds = timeout_seconds
        self.num_predict = num_predict
        self.keep_alive = keep_alive

    def _timeout(self) -> aiohttp.ClientTimeout:
        # Connect can be slow too when Ollama is busy loading weights into RAM.
        return aiohttp.ClientTimeout(
            total=self.timeout_seconds,
            sock_connect=60,
            sock_read=self.timeout_seconds,
        )

    async def ensure_ready(self) -> None:
        async with aiohttp.ClientSession() as session:
            async with session.get(f"{self.base_url}/api/tags") as resp:
                if resp.status != 200:
                    raise RuntimeError(
                        f"Ollama not reachable at {self.base_url} (HTTP {resp.status}). "
                        "Install from https://ollama.com and run `ollama serve`."
                    )
                payload = await resp.json()
        names = {m.get("name") for m in payload.get("models") or []}
        if self.model not in names and not any(self.model.split(":")[0] in n for n in names):
            log.warning(
                "Model %s not found locally. Run: ollama pull %s",
                self.model,
                self.model,
            )
        else:
            log.info(
                "Ollama ready · model=%s · timeout=%.0fs · num_predict=%d "
                "(CPU laptops: first call may take minutes while weights load)",
                self.model,
                self.timeout_seconds,
                self.num_predict,
            )

    async def warm(self) -> None:
        """Cheap preload so the first real /force is not a cold start."""
        log.info(
            "Warming %s (keep_alive=%s) — this can take a while on CPU…",
            self.model,
            self.keep_alive,
        )
        body = {
            "model": self.model,
            "messages": [{"role": "user", "content": "ok"}],
            "stream": False,
            "keep_alive": self.keep_alive,
            "options": {"num_predict": 4, "temperature": 0},
        }
        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(
                    f"{self.base_url}/api/chat",
                    json=body,
                    timeout=self._timeout(),
                ) as resp:
                    text = await resp.text()
                    if resp.status != 200:
                        log.warning("Warm-up failed (%s): %s", resp.status, text[:200])
                    else:
                        log.info("Model warm — subsequent actions should be faster")
        except Exception as exc:
            log.warning("Warm-up error (continuing anyway): %s", exc)

    def _tools(self, actions: Sequence[Any]) -> list[dict[str, Any]]:
        tools = []
        for action in actions:
            params = action.schema if isinstance(action.schema, dict) else {
                "type": "object",
                "properties": {},
            }
            if params.get("type") != "object":
                params = {"type": "object", "properties": {}, **params}
            tools.append(
                {
                    "type": "function",
                    "function": {
                        "name": action.name,
                        "description": action.description or action.name,
                        "parameters": params,
                    },
                }
            )
        return tools

    def _system_prompt(self, game: str) -> str:
        return (
            "You are Neuro-sama testing a desktop control integration.\n"
            f"The connected game/app is: {game or 'Neuro Desktop'}.\n"
            "Choose exactly ONE registered tool/action that moves the desktop task forward.\n"
            "Prefer safe, reversible actions. Avoid lock_workstation and shutdown_* unless asked.\n"
            "Keep typed text short (a few words). Prefer high-level intents when available.\n"
            "Decide quickly and call a tool — do not write a long explanation."
        )

    def _user_prompt(self, context_lines: list[str], query: str) -> str:
        # Short context window keeps CPU inference cheaper.
        recent = context_lines[-6:]
        ctx = "\n".join(f"- {line[:240]}" for line in recent) or "(no context yet)"
        return (
            f"Operator query: {query or 'Do something useful on the desktop.'}\n\n"
            f"Recent context:\n{ctx}\n\n"
            "Call one tool now."
        )

    def _gen_options(self) -> dict[str, Any]:
        return {
            "temperature": self.temperature,
            "num_predict": self.num_predict,
            # Smaller context = less work per token on CPU.
            "num_ctx": 2048,
        }

    async def choose_action(
        self,
        actions: Sequence[Any],
        context_lines: list[str],
        query: str = "",
        game: str = "",
    ) -> Optional[ActionChoice]:
        if not actions:
            return None

        tools = self._tools(actions)
        messages = [
            {"role": "system", "content": self._system_prompt(game)},
            {"role": "user", "content": self._user_prompt(context_lines, query)},
        ]

        body = {
            "model": self.model,
            "messages": messages,
            "tools": tools,
            "stream": False,
            "keep_alive": self.keep_alive,
            "options": self._gen_options(),
        }

        log.info("Asking Ollama (%s) to pick among %d action(s)…", self.model, len(actions))
        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(
                    f"{self.base_url}/api/chat",
                    json=body,
                    timeout=self._timeout(),
                ) as resp:
                    text = await resp.text()
                    if resp.status != 200:
                        log.error("Ollama chat failed %s: %s", resp.status, text[:300])
                        return await self._json_fallback(
                            session, actions, context_lines, query, game
                        )
                    result = json.loads(text)
        except Exception as exc:
            log.error("Ollama request error: %s", exc)
            return None

        choice = self._parse_tool_response(result, {a.name for a in actions})
        if choice:
            log.info("Ollama chose %s via %s", choice.name, choice.reason)
            return choice

        log.info("No tool call in response; trying JSON fallback")
        async with aiohttp.ClientSession() as session:
            return await self._json_fallback(session, actions, context_lines, query, game)

    def _parse_tool_response(
        self, result: dict[str, Any], allowed: set[str]
    ) -> Optional[ActionChoice]:
        message = result.get("message") or {}
        tool_calls = message.get("tool_calls") or []
        if tool_calls:
            call = tool_calls[0]
            fn = call.get("function") or {}
            name = fn.get("name")
            if name not in allowed:
                log.warning("model requested unknown action %r", name)
                return None
            raw_args = fn.get("arguments")
            data: Optional[dict[str, Any]]
            if isinstance(raw_args, dict):
                data = raw_args
            elif isinstance(raw_args, str) and raw_args.strip():
                try:
                    data = json.loads(raw_args)
                except json.JSONDecodeError:
                    data = None
            else:
                data = None
            return ActionChoice(name=name, data=data or None, reason="ollama-tool")

        content = message.get("content") or ""
        return self._parse_json_choice(content, allowed, reason="ollama-content")

    async def _json_fallback(
        self,
        session: aiohttp.ClientSession,
        actions: Sequence[Any],
        context_lines: list[str],
        query: str,
        game: str,
    ) -> Optional[ActionChoice]:
        catalog = []
        for a in actions:
            catalog.append(
                {
                    "name": a.name,
                    "description": a.description,
                    "schema": a.schema,
                }
            )
        prompt = (
            f"{self._system_prompt(game)}\n\n"
            f"{self._user_prompt(context_lines, query)}\n\n"
            "Available actions (JSON):\n"
            f"{json.dumps(catalog, ensure_ascii=False)[:4000]}\n\n"
            "Respond with ONLY one JSON object:\n"
            '{"name":"<action_name>","data":{...} or null,"reason":"short"}\n'
        )
        body = {
            "model": self.model,
            "messages": [{"role": "user", "content": prompt}],
            "stream": False,
            "format": "json",
            "keep_alive": self.keep_alive,
            "options": self._gen_options(),
        }
        async with session.post(
            f"{self.base_url}/api/chat",
            json=body,
            timeout=self._timeout(),
        ) as resp:
            if resp.status != 200:
                log.error("JSON fallback failed: %s", await resp.text())
                return None
            result = await resp.json()

        content = (result.get("message") or {}).get("content") or ""
        choice = self._parse_json_choice(
            content,
            {a.name for a in actions},
            reason="ollama-json",
        )
        if choice:
            log.info("Ollama chose %s via %s", choice.name, choice.reason)
        return choice

    def _parse_json_choice(
        self,
        content: str,
        allowed: set[str],
        reason: str,
    ) -> Optional[ActionChoice]:
        content = content.strip()
        if not content:
            return None

        match = re.search(r"\{[\s\S]*\}", content)
        if not match:
            return None
        try:
            obj = json.loads(match.group(0))
        except json.JSONDecodeError:
            return None

        name = obj.get("name")
        if name not in allowed:
            return None
        data = obj.get("data")
        if data is not None and not isinstance(data, dict):
            data = None
        return ActionChoice(
            name=name,
            data=data,
            reason=obj.get("reason") or reason,
        )
