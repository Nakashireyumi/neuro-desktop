# Ollama Neuro Tester

Local **Neuro API mock** for testing `neuro-integration` without a live Neuro
backend. Same role as [Randy](https://github.com/VedalAI/neuro-sdk/tree/main/Randy)
in the official SDK, but actions can be chosen by a local LLM —

**[heredos/rwkv7:2.9b](https://ollama.com/heredos/rwkv7)** (RWKV-7 / BlinkDL lineage).

RWKV-7 is a recurrent architecture (not a transformer): linear-time inference and
a tiny state — usually the friendliest “real LLM” option on CPU laptops.

## Quick start

```bash
cd desktop/tools/ollama-neuro
./setup.sh                 # venv + ollama pull heredos/rwkv7:2.9b
./run.sh --mode ollama --warm   # WS :8000 + UI :1337, preload model
```

Point neuro-desktop / neuro-integration at `ws://localhost:8000`
(already in `config/integration-config.yml`).

Control UI: [http://127.0.0.1:1337/](http://127.0.0.1:1337/)

## Slow CPU note (Dell Latitude E7490 and friends)

These boxes are fine for RWKV-7, but **cold starts are slow** (weights loading
into RAM + first tokens). Defaults assume that:

| Setting | Default | Why |
|---------|---------|-----|
| `--ollama-timeout` | `600` s | First `/force` may take several minutes |
| `--num-predict` | `96` | Short tool-call replies only |
| `--keep-alive` | `30m` | Keep weights resident between actions |
| `--warm` | off | Pass it once at startup to preload |

Recommended workflow on an E7490-class laptop:

```bash
# 1) Leave Ollama running, preload once (grab coffee)
ollama run heredos/rwkv7:2.9b "ok"
# or:
./run.sh --mode ollama --warm --keep-alive -1

# 2) For snappier iteration while wiring IPC, use manual/random
./run.sh --mode manual
./run.sh --mode random --auto --auto-interval 15

# 3) When you want LLM decisions, 2.9b is the sweet spot; 1.5b is faster:
./run.sh --mode ollama --model heredos/rwkv7:1.5b --warm
```

`7.2b` / `13.3b` will work but are painful without a GPU on this class of CPU.

## Modes

| Mode | Behavior |
|------|----------|
| `ollama` (default) | RWKV7 picks among registered Neuro actions (tool-call + JSON fallback) |
| `manual` | Randy-like: only HTTP-triggered actions (best while debugging IPC) |
| `random` | Uniform random registered action (no LLM wait) |

```bash
./run.sh --mode manual
./run.sh --mode ollama --model heredos/rwkv7:2.9b --warm
./run.sh --mode ollama --model heredos/rwkv7:7.2b   # stronger, much slower on CPU
./run.sh --auto --auto-interval 20                  # free-play; give CPU time
```

## Randy-compatible control

```bash
curl -s http://127.0.0.1:1337/health | jq

# Force the LLM (wait patiently on CPU)
curl -s -X POST http://127.0.0.1:1337/force \
  -H 'Content-Type: application/json' \
  -d '{"query":"Type hello in a short phrase"}'

# Manual action (exact Neuro `action` command) — instant, no LLM
curl -s -X POST http://127.0.0.1:1337/action \
  -H 'Content-Type: application/json' \
  -d '{"name":"type_text","data":{"text":"hello from rwkv7","execute_now":true}}'

# Randy raw forward
curl -s -X POST http://127.0.0.1:1337/ \
  -H 'Content-Type: application/json' \
  -d '{
    "command": "action",
    "data": {
      "id": "test-1",
      "name": "mouse_click",
      "data": "{\"button\":\"left\",\"execute_now\":true}"
    }
  }'
```

## Model notes

- Default: `heredos/rwkv7:2.9b` (~2.6GB) — good middle ground for this use case.
- Smaller (`0.4b` / `1.5b`) for smoke tests on slow CPUs.
- Larger (`7.2b` / `13.3b`) when you have GPU headroom or overnight patience.
- Heredos’s build includes tool-calling template tweaks; the tester still falls
  back to JSON if a reply has no `tool_calls`.

## Env vars

| Variable | Default |
|----------|---------|
| `OLLAMA_HOST` | `http://127.0.0.1:11434` |
| `NEURO_MOCK_MODEL` | `heredos/rwkv7:2.9b` |
| `NEURO_MOCK_MODE` | `ollama` |
| `NEURO_MOCK_WS_PORT` | `8000` |
| `NEURO_MOCK_HTTP_PORT` | `1337` |
| `NEURO_MOCK_OLLAMA_TIMEOUT` | `600` |
| `NEURO_MOCK_NUM_PREDICT` | `96` |
| `NEURO_MOCK_KEEP_ALIVE` | `30m` |

## Related SDK tools

- [Randy](https://github.com/VedalAI/neuro-sdk/tree/main/Randy) — random / manual
- [Tony](https://github.com/Pasu4/neuro-api-tony) — GUI manual
- [Jippity](https://github.com/EnterpriseScratchDev/neuro-api-jippity) — OpenAI
- [Gary](https://github.com/Govorunb/gary) — local LLM + UI

This tool is the in-repo, Ollama+RWKV7 path tailored to neuro-desktop.
