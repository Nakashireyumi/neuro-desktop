"""Neuro API mock server powered by Ollama (RWKV7) for local integration testing.

Mimics Neuro's WebSocket API (like Randy from VedalAI/neuro-sdk) but can also
pick actions with heredos/rwkv7 via Ollama — closer to real Neuro behavior than
random clicks.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Optional

from aiohttp import web
import websockets

from ollama_brain import OllamaBrain, ActionChoice

log = logging.getLogger("neuro-ollama")

# websockets 12 used WebSocketServerProtocol; 13+ uses ServerConnection / ClientConnection.
WebSocket = Any


# ---------------------------------------------------------------------------
# Protocol types (Neuro API — server side)
# ---------------------------------------------------------------------------

@dataclass
class RegisteredAction:
    name: str
    description: str
    schema: Optional[dict[str, Any]] = None


@dataclass
class ClientSession:
    ws: WebSocket
    game: str = ""
    actions: dict[str, RegisteredAction] = field(default_factory=dict)
    context: list[str] = field(default_factory=list)
    pending_force: Optional[dict[str, Any]] = None
    last_result: Optional[dict[str, Any]] = None
    connected_at: float = field(default_factory=time.time)


@dataclass
class ServerState:
    sessions: list[ClientSession] = field(default_factory=list)
    mode: str = "manual"  # manual | random | ollama
    brain: Optional[OllamaBrain] = None
    auto_interval: float = 8.0
    character_id: str = "neuro"
    display_name: str = "Neuro-sama (Ollama)"
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)

    def primary(self) -> Optional[ClientSession]:
        return self.sessions[0] if self.sessions else None


# ---------------------------------------------------------------------------
# Outgoing Neuro messages
# ---------------------------------------------------------------------------

async def send_json(ws: WebSocket, payload: dict[str, Any]) -> None:
    await ws.send(json.dumps(payload, ensure_ascii=False))


async def send_startup_ack(session: ClientSession, state: ServerState) -> None:
    await send_json(
        session.ws,
        {
            "command": "startup",
            "data": {
                "session": {
                    "sessionId": str(uuid.uuid4()),
                    "characterId": state.character_id,
                    "displayName": state.display_name,
                }
            },
        },
    )


async def send_action(
    session: ClientSession,
    name: str,
    data: Optional[dict[str, Any]] = None,
    action_id: Optional[str] = None,
) -> str:
    aid = action_id or str(uuid.uuid4())
    payload: dict[str, Any] = {
        "command": "action",
        "data": {
            "id": aid,
            "name": name,
        },
    }
    if data is not None:
        payload["data"]["data"] = json.dumps(data, ensure_ascii=False)
    await send_json(session.ws, payload)
    log.info("→ action %s id=%s data=%s", name, aid, data)
    return aid


# ---------------------------------------------------------------------------
# Incoming game → Neuro handling
# ---------------------------------------------------------------------------

async def handle_client_message(session: ClientSession, state: ServerState, raw: str) -> None:
    try:
        msg = json.loads(raw)
    except json.JSONDecodeError:
        log.warning("Ignoring malformed JSON from client")
        return

    command = msg.get("command")
    game = msg.get("game") or session.game
    data = msg.get("data") or {}

    if game:
        session.game = game

    if command == "startup":
        log.info("startup from game=%r", session.game)
        session.actions.clear()
        session.context.clear()
        await send_startup_ack(session, state)
        return

    if command == "context":
        message = str(data.get("message", ""))
        silent = bool(data.get("silent", True))
        session.context.append(message)
        # Keep a rolling window so RWKV7's modest context stays useful
        if len(session.context) > 40:
            session.context = session.context[-40:]
        log.info("context silent=%s: %s", silent, message[:160].replace("\n", " "))
        return

    if command == "actions/register":
        for action in data.get("actions") or []:
            name = action.get("name")
            if not name:
                continue
            session.actions[name] = RegisteredAction(
                name=name,
                description=str(action.get("description") or ""),
                schema=action.get("schema"),
            )
        log.info("registered %d action(s); total=%d", len(data.get("actions") or []), len(session.actions))
        return

    if command == "actions/unregister":
        for name in data.get("action_names") or []:
            session.actions.pop(name, None)
        log.info("unregistered; total=%d", len(session.actions))
        return

    if command == "actions/force":
        # Neuro would pick; we schedule an LLM / random / wait for manual
        session.pending_force = {
            "query": data.get("query", ""),
            "state": data.get("state"),
            "action_names": list(data.get("action_names") or []),
            "priority": data.get("priority", "low"),
            "ephemeral_context": bool(data.get("ephemeral_context", False)),
        }
        log.info("actions/force query=%r names=%s", session.pending_force["query"], session.pending_force["action_names"])
        asyncio.create_task(fulfill_force(session, state))
        return

    if command == "action/result":
        session.last_result = {
            "id": data.get("id"),
            "success": bool(data.get("success")),
            "message": data.get("message"),
            "at": time.time(),
        }
        log.info(
            "action/result id=%s success=%s msg=%s",
            data.get("id"),
            data.get("success"),
            str(data.get("message") or "")[:120],
        )
        return

    # Unknown commands are silently ignored per Neuro API
    log.debug("ignored command %r", command)


async def fulfill_force(session: ClientSession, state: ServerState) -> None:
    force = session.pending_force
    if not force:
        return

    names: list[str] = force["action_names"] or list(session.actions.keys())
    available = [session.actions[n] for n in names if n in session.actions]
    if not available:
        log.warning("force has no matching registered actions")
        session.pending_force = None
        return

    choice = await choose_action(state, session, available, query=force.get("query") or "")
    session.pending_force = None
    if choice is None:
        return
    await send_action(session, choice.name, choice.data)


async def choose_action(
    state: ServerState,
    session: ClientSession,
    available: list[RegisteredAction],
    query: str = "",
) -> Optional[ActionChoice]:
    mode = state.mode
    if mode == "manual":
        log.info("mode=manual — waiting for HTTP /action or /force (not auto-picking)")
        return None

    if mode == "random":
        import random

        action = random.choice(available)
        data = _minimal_valid_data(action)
        return ActionChoice(name=action.name, data=data, reason="random")

    # ollama
    if state.brain is None:
        log.error("ollama mode but brain is not configured")
        return None
    return await state.brain.choose_action(
        actions=available,
        context_lines=session.context,
        query=query,
        game=session.game,
    )


def _minimal_valid_data(action: RegisteredAction) -> Optional[dict[str, Any]]:
    """Best-effort empty/default params so random mode can fire schema'd actions."""
    schema = action.schema
    if not schema or not isinstance(schema, dict):
        return None
    props = schema.get("properties") or {}
    required = schema.get("required") or []
    if not required and not props:
        return None
    out: dict[str, Any] = {}
    for key in required:
        prop = props.get(key) or {}
        t = prop.get("type", "string")
        if "default" in prop:
            out[key] = prop["default"]
        elif t == "integer" or t == "number":
            out[key] = 0
        elif t == "boolean":
            out[key] = False
        elif t == "array":
            out[key] = []
        else:
            out[key] = ""
    return out or None


# ---------------------------------------------------------------------------
# WebSocket server
# ---------------------------------------------------------------------------

async def ws_handler(ws: WebSocket, state: ServerState) -> None:
    session = ClientSession(ws=ws)
    async with state.lock:
        state.sessions.append(session)
    peer = getattr(ws, "remote_address", None)
    log.info("client connected %s", peer)

    try:
        async for raw in ws:
            if isinstance(raw, bytes):
                # Neuro uses plaintext JSON; decode when possible for local testing.
                try:
                    raw = raw.decode("utf-8")
                except UnicodeDecodeError:
                    continue
            await handle_client_message(session, state, raw)
    except websockets.ConnectionClosed:
        pass
    finally:
        async with state.lock:
            if session in state.sessions:
                state.sessions.remove(session)
        log.info("client disconnected %s", peer)


# ---------------------------------------------------------------------------
# HTTP control plane (Randy-compatible + extras)
# ---------------------------------------------------------------------------

def make_http_app(state: ServerState) -> web.Application:
    app = web.Application()

    async def health(_req: web.Request) -> web.Response:
        session = state.primary()
        return web.json_response(
            {
                "ok": True,
                "mode": state.mode,
                "model": state.brain.model if state.brain else None,
                "connected": session is not None,
                "game": session.game if session else None,
                "actions": sorted(session.actions.keys()) if session else [],
                "context_lines": len(session.context) if session else 0,
                "last_result": session.last_result if session else None,
            }
        )

    async def randy_post(req: web.Request) -> web.Response:
        """Randy-compatible: POST body is a Neuro S2C message forwarded to the game."""
        session = state.primary()
        if not session:
            return web.json_response({"error": "no connected integration"}, status=400)
        body = await req.json()
        await send_json(session.ws, body)
        return web.json_response({"ok": True, "forwarded": body.get("command")})

    async def post_action(req: web.Request) -> web.Response:
        session = state.primary()
        if not session:
            return web.json_response({"error": "no connected integration"}, status=400)
        body = await req.json()
        name = body.get("name")
        if not name:
            return web.json_response({"error": "name required"}, status=400)
        data = body.get("data")
        aid = await send_action(session, name, data, action_id=body.get("id"))
        return web.json_response({"ok": True, "id": aid})

    async def post_force(req: web.Request) -> web.Response:
        """Ask the current mode (ollama/random) to pick among registered actions."""
        session = state.primary()
        if not session:
            return web.json_response({"error": "no connected integration"}, status=400)
        try:
            body = await req.json()
        except Exception:
            body = {}
        names = body.get("action_names") or list(session.actions.keys())
        query = body.get("query") or "Pick a useful desktop action and execute it."
        available = [session.actions[n] for n in names if n in session.actions]
        if not available:
            return web.json_response({"error": "no matching actions"}, status=400)

        log.info("HTTP /force query=%r actions=%d mode=%s", query, len(available), state.mode)
        prev = state.mode
        try:
            if prev == "manual":
                state.mode = "ollama" if state.brain else "random"
            choice = await choose_action(state, session, available, query=query)
        finally:
            state.mode = prev

        if choice is None:
            return web.json_response({"error": "no choice produced"}, status=500)
        aid = await send_action(session, choice.name, choice.data)
        return web.json_response(
            {
                "ok": True,
                "id": aid,
                "name": choice.name,
                "data": choice.data,
                "reason": choice.reason,
            }
        )

    async def set_mode(req: web.Request) -> web.Response:
        body = await req.json()
        mode = body.get("mode")
        if mode not in {"manual", "random", "ollama"}:
            return web.json_response({"error": "mode must be manual|random|ollama"}, status=400)
        if mode == "ollama" and state.brain is None:
            return web.json_response({"error": "ollama brain not configured"}, status=400)
        state.mode = mode
        return web.json_response({"ok": True, "mode": state.mode})

    async def ui(_req: web.Request) -> web.Response:
        return web.Response(text=CONTROL_UI, content_type="text/html")

    app.router.add_get("/", ui)
    app.router.add_get("/health", health)
    app.router.add_post("/", randy_post)
    app.router.add_post("/action", post_action)
    app.router.add_post("/force", post_force)
    app.router.add_post("/mode", set_mode)
    return app


CONTROL_UI = """<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Neuro Ollama Tester</title>
  <style>
    :root { font-family: ui-sans-serif, system-ui, sans-serif; color: #1a1a1a; background: #f6f3ee; }
    body { max-width: 720px; margin: 2rem auto; padding: 0 1rem; }
    h1 { font-size: 1.4rem; margin-bottom: 0.25rem; }
    .muted { color: #666; margin-bottom: 1.5rem; }
    section { background: #fff; border: 1px solid #ddd; border-radius: 8px; padding: 1rem; margin-bottom: 1rem; }
    button, select, input, textarea { font: inherit; }
    button { cursor: pointer; padding: 0.4rem 0.8rem; margin-right: 0.4rem; }
    pre { background: #111; color: #d6ffd6; padding: 0.75rem; overflow: auto; font-size: 0.85rem; }
    label { display: block; margin: 0.5rem 0 0.2rem; font-size: 0.9rem; }
    textarea, input[type=text] { width: 100%; box-sizing: border-box; }
  </style>
</head>
<body>
  <h1>Neuro Ollama Tester</h1>
  <p class="muted">Randy-compatible Neuro API mock · RWKV7 via Ollama · control plane<br/>
  Tip: on CPU laptops, warm the model first (<code>./run.sh --mode ollama --warm</code>) and expect the first decision to take a while.</p>
  <section>
    <div>Status: <strong id="status">…</strong></div>
    <div>Mode:
      <select id="mode">
        <option value="manual">manual</option>
        <option value="random">random</option>
        <option value="ollama">ollama</option>
      </select>
      <button id="applyMode">Apply</button>
      <button id="refresh">Refresh</button>
      <button id="force">Force (LLM/random)</button>
    </div>
  </section>
  <section>
    <label>Manual action name</label>
    <input id="aname" type="text" placeholder="e.g. type_text" />
    <label>JSON data (object, optional)</label>
    <textarea id="adata" rows="4">{"text":"hello from ollama tester","execute_now":true}</textarea>
    <p><button id="sendAction">Send action</button></p>
  </section>
  <section>
    <label>Registered actions</label>
    <pre id="actions">[]</pre>
    <label>Last health JSON</label>
    <pre id="health">{}</pre>
  </section>
  <script>
    async function refresh() {
      const r = await fetch('/health');
      const j = await r.json();
      document.getElementById('status').textContent =
        j.connected ? (j.game + ' · ' + j.mode + ' · ' + (j.model || 'no-model')) : 'waiting for integration…';
      document.getElementById('mode').value = j.mode;
      document.getElementById('actions').textContent = JSON.stringify(j.actions, null, 2);
      document.getElementById('health').textContent = JSON.stringify(j, null, 2);
    }
    document.getElementById('refresh').onclick = refresh;
    document.getElementById('applyMode').onclick = async () => {
      await fetch('/mode', { method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ mode: document.getElementById('mode').value }) });
      refresh();
    };
    document.getElementById('force').onclick = async () => {
      const r = await fetch('/force', { method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ query: 'Do something useful on the desktop.' }) });
      alert(await r.text());
      refresh();
    };
    document.getElementById('sendAction').onclick = async () => {
      let data = undefined;
      const raw = document.getElementById('adata').value.trim();
      if (raw) data = JSON.parse(raw);
      const r = await fetch('/action', { method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ name: document.getElementById('aname').value, data }) });
      alert(await r.text());
      refresh();
    };
    refresh();
    setInterval(refresh, 3000);
  </script>
</body>
</html>
"""


# ---------------------------------------------------------------------------
# Auto loop (optional free-play)
# ---------------------------------------------------------------------------

async def auto_loop(state: ServerState) -> None:
    while True:
        await asyncio.sleep(state.auto_interval)
        if state.mode not in {"ollama", "random"}:
            continue
        session = state.primary()
        if not session or not session.actions:
            continue
        # Don't stack if a force is already pending
        if session.pending_force:
            continue
        available = list(session.actions.values())
        choice = await choose_action(
            state,
            session,
            available,
            query="You are controlling a desktop. Pick one safe, useful next action.",
        )
        if choice:
            await send_action(session, choice.name, choice.data)


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------

async def main_async(args: argparse.Namespace) -> None:
    brain = None
    if args.mode in {"ollama", "manual"} or True:
        brain = OllamaBrain(
            base_url=args.ollama_url,
            model=args.model,
            temperature=args.temperature,
            timeout_seconds=args.ollama_timeout,
            num_predict=args.num_predict,
            keep_alive=args.keep_alive,
        )

    state = ServerState(
        mode=args.mode,
        brain=brain,
        auto_interval=args.auto_interval,
        character_id=args.character_id,
        display_name=args.display_name,
    )

    if args.mode == "ollama":
        await brain.ensure_ready()
        if args.warm:
            await brain.warm()

    http_app = make_http_app(state)
    runner = web.AppRunner(http_app)
    await runner.setup()
    site = web.TCPSite(runner, args.http_host, args.http_port)
    await site.start()
    log.info("HTTP control UI http://%s:%d/  (Randy-compatible POST /)", args.http_host, args.http_port)

    auto_task = None
    if args.auto:
        auto_task = asyncio.create_task(auto_loop(state))
        log.info("auto-play enabled every %.1fs (mode must be ollama|random)", args.auto_interval)

    log.info("Neuro WebSocket ws://%s:%d  mode=%s model=%s", args.ws_host, args.ws_port, state.mode, args.model)

    async def handler(ws: WebSocket) -> None:
        await ws_handler(ws, state)

    try:
        async with websockets.serve(
            handler,
            args.ws_host,
            args.ws_port,
            # Local integrations often block the client loop briefly; disable
            # aggressive keepalive so Ollama thinking time does not drop the WS.
            ping_interval=None,
            max_size=8 * 1024 * 1024,
        ):
            await asyncio.Future()  # run forever
    finally:
        if auto_task:
            auto_task.cancel()


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="Ollama-backed Neuro API mock for neuro-desktop testing")
    p.add_argument("--ws-host", default=os.getenv("NEURO_MOCK_WS_HOST", "127.0.0.1"))
    p.add_argument("--ws-port", type=int, default=int(os.getenv("NEURO_MOCK_WS_PORT", "8000")))
    p.add_argument("--http-host", default=os.getenv("NEURO_MOCK_HTTP_HOST", "127.0.0.1"))
    p.add_argument("--http-port", type=int, default=int(os.getenv("NEURO_MOCK_HTTP_PORT", "1337")))
    p.add_argument(
        "--mode",
        choices=["manual", "random", "ollama"],
        default=os.getenv("NEURO_MOCK_MODE", "ollama"),
        help="manual=Randy-like HTTP only; random=pick randomly; ollama=RWKV7 decides",
    )
    p.add_argument(
        "--model",
        default=os.getenv("NEURO_MOCK_MODEL", "heredos/rwkv7:2.9b"),
        help="Ollama model tag (default: heredos/rwkv7:2.9b)",
    )
    p.add_argument("--ollama-url", default=os.getenv("OLLAMA_HOST", "http://127.0.0.1:11434"))
    p.add_argument("--temperature", type=float, default=float(os.getenv("NEURO_MOCK_TEMPERATURE", "0.4")))
    p.add_argument(
        "--ollama-timeout",
        type=float,
        default=float(os.getenv("NEURO_MOCK_OLLAMA_TIMEOUT", "600")),
        help="Seconds to wait for Ollama (CPU laptops often need 5–10+ minutes on cold start)",
    )
    p.add_argument(
        "--num-predict",
        type=int,
        default=int(os.getenv("NEURO_MOCK_NUM_PREDICT", "96")),
        help="Max tokens to generate per decision (keep low on CPU)",
    )
    p.add_argument(
        "--keep-alive",
        default=os.getenv("NEURO_MOCK_KEEP_ALIVE", "30m"),
        help="How long Ollama keeps the model loaded (e.g. 30m, -1 forever)",
    )
    p.add_argument(
        "--warm",
        action="store_true",
        help="Preload the model at startup so the first /force is not a cold load",
    )
    p.add_argument("--auto", action="store_true", help="Periodically pick actions without actions/force")
    p.add_argument("--auto-interval", type=float, default=float(os.getenv("NEURO_MOCK_AUTO_INTERVAL", "8")))
    p.add_argument("--character-id", default="neuro")
    p.add_argument("--display-name", default="Neuro-sama (Ollama RWKV7)")
    p.add_argument("-v", "--verbose", action="store_true")
    return p


def main() -> None:
    args = build_parser().parse_args()
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    try:
        asyncio.run(main_async(args))
    except KeyboardInterrupt:
        log.info("stopped")


if __name__ == "__main__":
    main()
