# nd-vision-server

An optional screen-description service for Neuro Desktop. The server calls it
through `NEURO_VISION_URL` when Neuro asks what is on screen (`game_observe`), and
the dashboard shows whether it is reachable.

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

If a backend is missing its requirement, the server says so in `warnings`
instead of failing. A backend that is not in the list above refuses to start.

## Environment

| Variable | Default | What it does |
| --- | --- | --- |
| `NEURO_VISION_HOST` / `NEURO_VISION_PORT` | `127.0.0.1` / `8610` | where to listen |
| `NEURO_VISION_TOKEN` | none | bearer token. **Required** when the host is not loopback |
| `NEURO_VISION_ROOT` | none | the only directory `image_path` may read from. With none set, the server never opens a path |
| `NEURO_VISION_BACKEND` | `stats` | which backend answers `/describe` |

## HTTP contract

`GET /health` returns `{"ok": true, "version": …, "backend": …, "max_image_bytes": …, "reads_paths": …}`.
The bridge probes it to show the vision state on the dashboard.

`POST /describe` takes a JSON object with either `image_base64` (a strictly decoded
base64 image) or `image_path` (only inside `NEURO_VISION_ROOT`), plus an optional
`prompt` (up to 2000 characters) and optional `metadata`. The Neuro server always sends
`image_base64`: it never sends a path. It returns
`{"ok": true, "summary": …, "backend": …, "format": …, "width": …, "height": …, "text": …, "warnings": […]}`.

Limits: 12 MiB per request body, 8 MiB per image, 2000 characters per prompt.
Errors are `{"ok": false, "error": …}` with status 400 (bad JSON or a missing image), 401 (missing or wrong
bearer token), 404 (unknown route or missing file), 413 (too large), 422 (not an image), or 502
(a model backend that cannot be reached, which is reported rather than faked).

## Safety

* It binds to loopback unless told otherwise. A non-loopback bind refuses to start
  without `NEURO_VISION_TOKEN`.
* It reads files only from `NEURO_VISION_ROOT`.
* Logs record the method, path, and status. They never record request bodies or tokens.

## Tests

```bash
cd desktop/apps/nd-vision-server
python3 -m unittest discover -s tests -t .
```

CI runs these tests in the `vision` job. The tests cover the contract above: the
token, the size limits, strict base64, path confinement, and the backend errors.
