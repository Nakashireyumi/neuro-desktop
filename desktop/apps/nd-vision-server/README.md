# nd-vision-server

An optional screen-description service for Neuro Desktop. The server calls it
through `NEURO_VISION_URL` when Neuro asks what is on screen (`game_observe`), and
the dashboard shows whether it is reachable.

You control Neuro Desktop's image use from **Extensions → Built-in services →
Vision Service**. Review the [per-twin controls and privacy guide](../../../docs/VISION.md).
Those controls do not stop this independently managed HTTP process.

It is Python on the standard library, so it runs on a machine with no GUI and
needs nothing installed to start. Pillow and tesseract add capabilities; see
`requirements.txt`.

## Run it

```bash
cd desktop/apps/nd-vision-server
NEURO_VISION_BACKEND=stats NEURO_VISION_TOKEN="$(openssl rand -hex 24)" \
  python3 -m nd_vision --host 127.0.0.1 --port 8610
```

Then, on the server: `NEURO_VISION_URL=http://127.0.0.1:8610` and
`NEURO_VISION_TOKEN` set to the same value.

## Backends (`NEURO_VISION_BACKEND` or `--backend`)

| Backend | What it returns | Needs |
| --- | --- | --- |
| `size` | image size and format | stdlib |
| `stats` | size plus a colour summary (brightness, dominant colours) | Pillow |
| `ocr` | `stats`, plus text read by `tesseract` | Pillow and `tesseract` |
| `ollama` | a description from a local Ollama model | `NEURO_VISION_MODEL`, `NEURO_VISION_OLLAMA_URL` |
| `openai` | a description from an OpenAI-compatible endpoint | `NEURO_VISION_OPENAI_URL`, `NEURO_VISION_MODEL`, `NEURO_VISION_OPENAI_KEY` |
| `cluster` | forwards to one worker from a fixed operator list, with one sequential failover | `NEURO_VISION_WORKERS`, optional `NEURO_VISION_WORKER_TOKEN` |

If a backend is missing its requirement, the server says so in `warnings`
instead of failing. A backend that is not in the list above refuses to start.

## Environment

| Variable | Default | What it does |
| --- | --- | --- |
| `NEURO_VISION_HOST` / `NEURO_VISION_PORT` | `127.0.0.1` / `8610` | where to listen |
| `NEURO_VISION_TOKEN` | none | bearer token. **Required** when the host is not loopback |
| `NEURO_VISION_ROOT` | none | the only directory `image_path` may read from. With none set, the server never opens a path |
| `NEURO_VISION_BACKEND` | `stats` | which backend answers `/describe` |
| `NEURO_VISION_MAX_CONNECTIONS` | `8` | accepted sockets before extra connections get a 503 (1-64) |
| `NEURO_VISION_MAX_INFERENCE` | `2` | requests that may decode or call a model at once (1-8) |
| `NEURO_VISION_DEADLINE_SECONDS` | `40` | hard analysis deadline (1-40). The bridge waits 45 s and the manager 50 s |
| `NEURO_VISION_WORKERS` | none | `name=url,name=url` list for `--backend cluster` |
| `NEURO_VISION_WORKER_TOKEN` | none | bearer token sent to every cluster worker |

## Cluster workers

Clustering is a fixed list that the operator writes in the environment. There is
no discovery, no URL taken from a model response, and no speculative fanout: one
worker receives the image, and one further worker is tried only after the first
fails. A failed worker cools down for 15 seconds.

```bash
NEURO_VISION_BACKEND=cluster \
NEURO_VISION_WORKER_TOKEN="$(openssl rand -hex 24)" \
NEURO_VISION_WORKERS="primary=http://127.0.0.1:8611,backup=https://vision.example" \
  python3 -m nd_vision --host 127.0.0.1 --port 8610
```

Plain HTTP is allowed only for loopback workers. A remote worker must use HTTPS,
because the body contains a private screenshot. Worker URLs may name the host or
`/describe`; other paths, credentials, query strings and fragments are refused.
`GET /health` lists worker names and cooldown state, never their URLs or tokens.

## HTTP contract

`GET /health` returns `{"ok": true, "version": …, "backend": …, "max_image_bytes": …,
"max_image_pixels": …, "max_connections": …, "max_inference": …, "deadline_seconds": …,
"reads_paths": …}` plus `workers` when the cluster backend is active. The bridge probes
it to show the vision state on the dashboard.

`POST /describe` takes a JSON object with either `image_base64` (a strictly decoded
base64 image) or `image_path` (only inside `NEURO_VISION_ROOT`), plus an optional
`prompt` (up to 2000 characters) and optional `metadata`. The Neuro server always sends
`image_base64`: it never sends a path. It returns
`{"ok": true, "summary": …, "backend": …, "format": …, "width": …, "height": …, "text": …, "warnings": […]}`.

A caller may send `X-Neuro-Deadline-Ms` to shorten the configured deadline. The bridge
sends 40 seconds. The server never extends a deadline above `NEURO_VISION_DEADLINE_SECONDS`.
The analysis passes the remaining time to upstream sockets and to the OCR subprocess,
so neither keeps working after the deadline.

Limits: 12 MiB per request body, 8 MiB per image, 40 million decoded pixels, 2000
characters per prompt, 1 MiB per upstream model response, 8 accepted connections and 2
concurrent analyses by default.
Errors are `{"ok": false, "error": …}` with status 400 (bad JSON, missing image, bad
deadline header), 401 (missing or wrong bearer token), 404 (unknown route or missing
file), 413 (too large), 422 (not an image or over the pixel limit), 502 (a backend or
worker that cannot be reached, which is reported rather than faked), 503 (connection or
analysis slots are full; retry after the advertised second) or 504 (the deadline expired
before a result was ready).

## Safety

* It binds to loopback unless told otherwise. A non-loopback bind refuses to start
  without `NEURO_VISION_TOKEN`.
* It reads files only from `NEURO_VISION_ROOT`. Paths are read through an 8 MiB bound
  rather than trusting a size check that another process could race.
* It refuses upstream redirects instead of forwarding a private screenshot to a new
  host, and bounds upstream response reads.
* Pillow decompression warnings are errors at the 40-megapixel limit. The server never
  sends an image to a URL that a model supplied.
* Connection and inference threads are fixed. Excess work gets a 503 instead of another
  unbounded thread or a queued screenshot.
* Logs record the method, path, and status. They never record request bodies or tokens.

## Tests

```bash
cd desktop/apps/nd-vision-server
python3 -m unittest discover -s tests -t .
```

CI runs these tests in the `vision` job. The tests cover the contract above: the
token, size and pixel limits, strict base64, path confinement, backend errors,
connection/inference overload, deadlines, bounded upstream reads, worker URL
rules, sequential failover, cooldown, and that a successful worker receives no
speculative fanout.
