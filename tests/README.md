# Testing Neuro Desktop

From `desktop/`, run `NEURO_HEADLESS=1 make test` for the Go server and dashboard,
Python client, frontend API, and repository checks. `./scripts/build-all.sh` also
builds all source components and runs their tests. Node.js 22.6+ is needed for the
frontend tests.

The Go suite includes a simulated Neuro API and the real Python client over TCP.
It verifies telemetry, an allowlisted shell command and its returned output,
pause/resume enforcement, and input release. The client runs headless and never
generates desktop input. If Python is unavailable, this cross-language test skips;
the CI runners provide it.

Additional checks, from the repository root:

```bash
cd headless/apps/neuro-integration
go test -race -count=1 ./...
go test -count=1 -tags neurodev ./...
```

CI also checks platform builds, vision, relay, MCP, catalog signatures and bundles
in `.github/workflows/ci.yml`. Actual mouse/keyboard behavior, native screenshots,
and Windows/macOS packaged clients still need verification on those desktops.
