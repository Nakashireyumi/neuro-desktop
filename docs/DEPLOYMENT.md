# Deployment

This describes how to build, bundle, and run Neuro Desktop. It replaces the older
guide, which described the Rust executor and the C++ supervisor; both were removed
(see `CHANGELOG.md`).

There are three programs, and they can run on one machine or on three:

* the **server** (`neuro-integration`, Go). It talks to Neuro's API, applies the policy,
  does the vision work, and serves the dashboard at `/ui/`. Give it the spare power;
* the **dashboard program** (`neuro-dashboard`, Go). It serves the same dashboard page
  from another program and forwards its API to the server. It is optional;
* the **client** (`neuro-client`, the Python agent packaged as one executable). It runs
  on the PC Neuro controls, and it carries out the server's commands. It needs no
  Python on that PC when it is built as a program.

The reasons for the split are in `docs/ARCHITECTURE.md`.

## Requirements

| What | Version | Needed for |
| --- | --- | --- |
| Go | 1.22 or newer (`go.mod` says 1.22) | the server and the dashboard program |
| Python | 3.12 is what CI runs; other 3.x versions are untested | running the client from source, and building `neuro-client` |
| PyInstaller | 6.22.3 (pinned in `scripts/build-client.sh`) | building `neuro-client` only |
| Node.js | 22 is what CI runs | building the dashboard only |
| Git | any | cloning |

Optional: `tesseract` (OCR for the vision server), Ollama (the local test brain), and
`npx` on the PATH for MCP servers from the catalog.

**Never run any of it with `sudo`.** The agent needs your logged-in desktop session
(Wayland and X authorisation belong to that session), and `sudo` makes the files it
writes owned by root.

## Build

From the repository root:

```bash
cd desktop
# the server
(cd apps/neuro-integration && go build -o neuro-integration .)
# the dashboard program (optional)
(cd apps/neuro-dashboard && go build -o neuro-dashboard .)
# the dashboard page
(cd frontend && npm ci && npm run build)
# the client, for the PC Neuro controls: one executable (see "The client" below)
./scripts/build-client.sh
```

On Windows, use `go build -o neuro-integration.exe .` in `apps\neuro-integration`, and
`.\scripts\build-client.ps1` for the client. `scripts/build-go.ps1` does the server for
all three platforms.

Before the first start, run `./neuro-integration setup` once (see Run). It creates the
two secrets that the server, the relay host, and the dashboard share.

## Bundle

The bundle scripts put the server, the dashboard program, the client, the catalog, and
the policy into one folder:

| Platform | Development bundle | Release bundle |
| --- | --- | --- |
| Linux / macOS | `scripts/bundle/dev.sh` | `scripts/bundle/prod.sh` |
| Windows | `scripts/bundle/dev.ps1` | `scripts/bundle/prod.ps1` |

The output is `desktop/dist/neuro-desktop/`:

```
neuro-desktop/
├── neuro-integration(.exe)     the server
├── neuro-dashboard(.exe)       the dashboard program (optional to run)
├── neuro-client/               the client for the PC Neuro controls (release bundles,
│                               when it could be built; see "The client")
├── agent/                      the Python agent (controller/, requirements): the fallback
│                               on this PC when neuro-client/ is absent
├── start.sh / start.bat        starts the server and the local agent
├── frontend/                   the dashboard page
├── catalog/                    the signed extension index and publisher keys
├── config/                     example configuration
├── permissions.json            the policy (copied from the example; edit it)
├── integration-docs/           documentation for Neuro
└── README.txt
```

`NEURO_BUNDLE_SKIP_VENV=1` skips creating a Python virtual environment for `agent/`.
`NEURO_BUNDLE_SKIP_CLIENT=1` skips building `neuro-client/`. A release bundle tries to
build it; when that fails (on Linux it needs the Python headers and a shared `libpython`,
see below), the bundle is still made and the launcher uses `agent/`. Read the
`!` lines in the build output.

**Edit `permissions.json` before you start.** It is copied from the example, which is
safe by default: the shell, system, filesystem, and extensions scopes are off. Neuro
can ask for filesystem and extensions with `request_permission`; you decide on the
Permissions page.

## Run

On the PC Neuro uses, from a logged-in session:

```bash
cd dist/neuro-desktop
./start.sh          # Windows: start.bat
```

The first start runs `neuro-integration setup`. It creates the relay token
(`relay-token`) and prints the dashboard token once. Copy that token now: it is the
sign-in token, and the server keeps only its hash. Every later start runs the same
step, which keeps the existing tokens and checks that they agree. If the check fails,
the launcher stops and prints the line to fix.

This starts the server and the local agent: `neuro-client/` when the bundle has it, else
the Python agent in `agent/`. `NEURO_NO_AGENT=1` starts only the server, for a split-machine
setup where the client runs elsewhere.

Open the dashboard at `http://127.0.0.1:8300/ui/` and sign in with the dashboard token.

### Split machines

Run the server on the machine that does the work. Run the client on the PC Neuro
controls. Optionally, run the dashboard program on the PC you sit at. The client always
dials the server, so the PC it controls needs no inbound port:

```bash
# on the PC Neuro controls: the built program, which needs no Python
./neuro-client --bridge <server-ip>:9876 --token "$NEURO_EXECUTOR_TOKEN"
# or, from source (needs Python and the requirements):
python3 -m controller.agent --bridge <server-ip>:9876 --token "$NEURO_EXECUTOR_TOKEN"
```

On the server, the hub must listen on an address the client can reach, for example
`NEURO_EXECUTOR_LISTEN=0.0.0.0:9876`, and `NEURO_EXECUTOR_TOKEN` must be set. The hub
refuses to listen beyond loopback without a token, and it says why. Allow port 9876 from
the client's address only. On the server's bundle, set `NEURO_NO_AGENT=1` so that
`start.sh` does not start a client there.

**The dashboard on another PC.** The server's admin port is on loopback by default, and
it is plain HTTP. Forward it over SSH rather than opening it, then run the dashboard
program against the forwarded port:

```bash
# on the PC you sit at
ssh -L 8300:127.0.0.1:8300 user@<server-ip>          # keep this open
./neuro-dashboard --server http://127.0.0.1:8300 --listen 127.0.0.1:8310
# then open http://127.0.0.1:8310/ui/ and sign in with the dashboard token
```

The dashboard program adds no secret and no token of its own, so the sign-in is still the
server's. It prints a warning when it listens beyond loopback or talks to a server over
plain HTTP on another machine.

### The client (`neuro-client`)

The client is the Python agent, packaged with PyInstaller as one program for the OS it
is built on. PyInstaller does not cross-compile: build the Windows client on Windows and
the Linux client on Linux.

```bash
cd desktop
./scripts/build-client.sh        # -> dist/neuro-client/neuro-client
./dist/neuro-client/neuro-client --help
```

On Windows: `.\scripts\build-client.ps1` -> `dist\neuro-client\neuro-client.exe`.

A Go client is in progress in `desktop/apps/neuro-client-go`. It is not in the bundle yet:
slice 1 runs the executor link, status, shell and lifecycle commands, and it refuses input and
screen commands, which still run only in this Python client. Build it with
`go build -o neuro-client-go .` in that folder, if you want to test it.

Copy the `neuro-client` folder to the PC Neuro controls, then run it with the server's
address and the executor token. The program contains its own Python, so the PC needs no
Python install. It holds no policy, no dashboard, and no vision, and it does only what
the server sends.

**Linux build prerequisites.** `pynput` depends on `evdev`, which has no prebuilt wheel,
so pip builds it from source. That needs the Python development headers (`Python.h`). On
Debian and Ubuntu, `sudo apt install python3-dev` installs them. PyInstaller also needs a
Python built with a shared `libpython`, and a Python that was linked statically is
refused ("Python shared library ... was not found"). The script prints this hint when the
install fails. CI builds `neuro-client` on Ubuntu with `actions/setup-python` (the
`client-build` job), so that path is checked on every push. The sandbox where this was
developed had neither the headers nor a shared `libpython`, so the build was not run there.

**The executor link is not encrypted yet.** The token and every command cross the
network as plain TCP. On an untrusted network, keep the hub on loopback and tunnel it:
run `ssh -L 9876:127.0.0.1:9876 user@<server>` on the desktop, and point the agent at
`127.0.0.1:9876`. TLS with a pinned certificate is an open item in
[PRODUCTION_TODO.md](PRODUCTION_TODO.md). The server does not dial the agent: reverse
connections are allowed in the design, and they must be authenticated, but they are not
built yet.

### Headless (no display)

On a machine with no graphical session, set `NEURO_HEADLESS=1`. The agent then skips the
GUI libraries, so it starts without a display. Shell commands still run (within the
allowlist), and a screenshot reports that there is no display session. The dashboard and
the policy work the same. See `docs/CAPABILITIES.md`.

To open the dashboard from another machine, forward the port over SSH rather than
exposing it:

```bash
ssh -L 8300:127.0.0.1:8300 user@server
```

## Ports

| Port | Process | Default address | Expose it? |
| --- | --- | --- | --- |
| 8300 | server: dashboard and admin API | `127.0.0.1:8300` (`NEURO_ADMIN_LISTEN`) | no; use SSH forwarding |
| 8310 | dashboard program: the dashboard page and the forwarded API | `127.0.0.1:8310` (`NEURO_DASHBOARD_LISTEN`) | no; it is for your own PC |
| 9876 | server: executor hub, clients connect here | `127.0.0.1:9876` (`NEURO_EXECUTOR_LISTEN`) | only on a trusted LAN, with a token |
| 8765 | relay host: integrations and watchers | `127.0.0.1:8765` | only on loopback or a firewalled LAN |
| 8766 | relay host: `/health` | `127.0.0.1:8766` | no |
| 8610 | vision server | `127.0.0.1:8610` | no |
| 8000 | Neuro API (outbound) | `NEURO_SDK_WS_URL` | outbound only |

Allow outbound traffic to the Neuro API and, if you use them, to the npm registry (for
MCP servers) and to GitHub (for `git_clone` extension installs). Nothing else needs to
reach the machine from outside.

## The relay and the vision server (optional)

- **Relay:** run `neuro-integration relay` next to the server, then set the server's
  `NEURO_RELAY_ENABLED` and `NEURO_RELAY_URL`. The host and the server both read one
  token file (`relay-token`, written by `setup`), so no token is copied by hand. Details
  are in `docs/RELAY.md`.
- **Vision:** run `python3 -m nd_vision` from `desktop/apps/nd-vision-server` and set
  `NEURO_VISION_URL` and `NEURO_VISION_TOKEN` on the server. Details are in that
  folder's README. The server sends each screenshot to it as image bytes, so the vision
  service can run on a different machine from the client.

The Extensions page shows whether each one is reachable.

## Configuration

Every environment variable the server reads is listed in `desktop/README.md`
(Configuration). The ones an operator usually sets:

| Variable | Set it to |
| --- | --- |
| `NEURO_SDK_WS_URL` | the Neuro API websocket |
| `NEURO_ADMIN_TOKEN` | optional. The dashboard token, if you set it yourself; it must equal the token `setup` printed |
| `NEURO_DASHBOARD_TOKEN_FILE` | where the dashboard token's hash is kept (default `./dashboard-token`) |
| `NEURO_RELAY_TOKEN_FILE` | where the shared relay token is kept (default `./relay-token`) |
| `NEURO_EXECUTOR_TOKEN` | the secret every agent presents |
| `NEURO_PERMISSIONS_FILE` | the policy file (defaults to `permissions.json` in the bundle) |
| `NEURO_SHELL_ALLOWLIST` | the programs the shell action may run |
| `NEURO_AUDIT_LOG` | where the audit log is written (JSON lines) |
| `NEURO_KILL_SWITCH_FILE` | a file whose existence stops every action |

## Upgrading

1. Stop the running processes (Ctrl-C in the terminal that runs `start.sh`).
2. Build and bundle the new version.
3. Keep your `permissions.json`, your audit log, the extension state file, and the two
   token files (`relay-token` and `dashboard-token`). Copy them into the new bundle, then
   compare the new example policy with yours. Without `relay-token`, the next `setup` makes
   a new relay token, and a relay that still has the old one refuses the server until both
   are updated. Without `dashboard-token`, the next `setup` prints a new dashboard token,
   and the old one stops working.
4. Start it again and check the Status tab and the Extensions page.

## Health checks

```bash
curl -s http://127.0.0.1:8300/health            # the server is up (the only public route)
curl -s http://127.0.0.1:8310/health            # the dashboard program, if you run it: it forwards /health
curl -s -H "X-ND-Token: <dashboard token>" http://127.0.0.1:8300/api/runtime   # bridge, relay, vision, and MCP state
curl -s http://127.0.0.1:8766/health            # the relay host, if you run it
curl -s http://127.0.0.1:8610/health            # the vision server, if you run it
```

The dashboard's Extensions page shows the same runtime state, live.

## Backups

Back up `permissions.json`, the audit log, the extension state file, and the two token
files. The token files are secrets, so store the backup as you would store a password:
`relay-token` holds the token in clear, and `dashboard-token` holds only a hash of the
dashboard token. Permission approvals and pending requests are in memory, so a restart
clears them.

## Service installs

The bundle does not install a service, and this repository does not ship one. Run the
bundle from a logged-in session. A service that runs without the desktop session cannot
control the desktop, so it is not a supported setup for the agent. The server alone can
run as a service on a headless machine, but that has not been tested here.

## What is verified here, and what is not

Verified in CI: the server builds for Windows, macOS, and Linux; the server and agent
test suites pass, including a run with `-tags neurodev`; the dashboard program's tests
pass on all three systems; the dashboard page builds; the release bundle builds on Linux,
and the server and the dashboard program serve it, with the dashboard refusing a request
that has no token; the agent and server run end to end on Linux with no display; the
setup smoke test runs `setup` twice (the relay token and the dashboard token are created
with mode 0600, the second run keeps both, and `setup --check` passes); and the
`client-build` job builds `neuro-client` on Linux and checks that it runs and reports a
missing server.

Verified in the development sandbox (not CI): the dashboard program against a running
server (sign-in, 401 without a token, 200 with it, `/health`, `/admin` not forwarded, and
the path traversal refused), and a client run from source that connected to the server's
hub and was reported as connected through the API.

Not verified: the bundle on a real Windows or macOS desktop, the client built on Windows
or macOS (PyInstaller has not been run there), input and screen capture on a real
desktop, and an installed (not built) release. Treat those as open until someone has run
them.
