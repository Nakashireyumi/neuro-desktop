Neurcro
-------

What is in this folder
----------------------
  @SERVER@            the server: connects to Neuro, serves the dashboard, forwards
                      commands to the agent, and calls the vision service when one
                      is configured. Give it the machine with the spare power.
  neuro-dashboard     the dashboard program. Optional: the server already serves the
                      dashboard at /ui/. Run this one on the PC you sit at when the
                      dashboard should not run on the server.
  neuro-client/       the program for the PC Neuro controls, when this bundle has it.
                      It is the agent as one executable: no Python needed there.
  agent/              the Python agent. It runs on this PC when neuro-client/ is not
                      in the bundle (needs Python 3.9+ with agent/requirements.txt).
  frontend/           dashboard client, served at http://127.0.0.1:8300/ui/
  manager/            native GTK 4 manager with its own startup splash
  catalog/            game profiles, extension state and the audio/vision catalog
  config/             example policy and integration config
  permissions.json    the active permission policy (edit it, or use the dashboard)
  integration-docs/   action reference and platform notes

First run
---------
  ./@SERVER@ setup          (start.sh and start.bat do this for you)

  This creates two secrets and checks them:
    - relay-token: the relay token. The relay host and the server both read this
      one file, so nothing has to be copied between them.
    - the dashboard token: printed ONCE. Copy it somewhere safe. Only its hash is
      stored, so it cannot be shown again; use `setup --rotate-dashboard` for a
      new one. You type it into the dashboard's sign-in page.

  Run `./@SERVER@ setup --check` at any time to confirm the setup is still
  consistent (it changes nothing).

Run it
------
  - Windows: start.bat
  - Linux/macOS: ./start.sh

  The native manager opens automatically. Enter your dashboard token to connect.
  NEURO_NO_MANAGER=1 disables the manager for headless servers. To connect the
  manager to another server, set NEURO_MANAGER_SERVER=http://<server-ip>:8300.
  Production builds include GTK; source builds need the dependencies in
  manager/README.md. The browser dashboard is also available for remote use.

  The dashboard asks for the dashboard token in each new browser session. The token is
  never read from the URL, and the browser keeps it only for that browser session.

Where each program runs
-----------------------
  Server (@SERVER@)  the machine that talks to Neuro and does the heavy work.
  Dashboard          served by the server at http://<server-ip>:8300/ui/, or run
                     neuro-dashboard on your own PC:
                       ./neuro-dashboard --server http://<server-ip>:8300 --listen 127.0.0.1:8310
                     then open http://127.0.0.1:8310/ui/. The dashboard program adds
                     no secret of its own: sign-in is still the server's dashboard token.
  neuro-client       the PC Neuro controls. It only carries out the commands the server
                     sends. It has no policy, no dashboard and no vision.

Two machines (the usual setup for playing a game on a different PC)
------------------------------------------------------------------
  1. On the server PC:
       ./@SERVER@ setup
       NEURO_EXECUTOR_TOKEN=<a long random value> ./@SERVER@ --ws-url ws://<neuro-host>:8000 --executor-listen 0.0.0.0:9876
     Set NEURO_NO_AGENT=1 as well, so the launcher does not start an agent on this PC.
     Only agents that know the executor token can connect. Open the port in the
     firewall only for the PC Neuro controls.
  2. On the PC Neuro should control, with the same token value:
       neuro-client/neuro-client --bridge <server-ip>:9876 --token <executor token>
     Windows: neuro-client\neuro-client.exe --bridge <server-ip>:9876 --token <executor token>
     Without a neuro-client folder in this bundle, use the Python agent:
       cd agent
       python3 -m pip install -r requirements.txt
       python3 -m controller.agent --bridge <server-ip>:9876 --token <executor token>

  The token and the commands cross the network without encryption. Use this on a network
  you trust, or tunnel port 9876 over SSH instead.

Environment variables worth knowing
-----------------------------------
  NEURO_SDK_WS_URL         Neuro API websocket (default ws://localhost:8000)
  NEURO_EXECUTOR_TOKEN     shared secret required from agents
  NEURO_ADMIN_TOKEN        optional. Must equal the dashboard token; setup --check
                           reports if it does not. Normally leave it unset
  NEURO_DASHBOARD_TOKEN_FILE  where the dashboard token hash is kept (default ./dashboard-token)
  NEURO_RELAY_TOKEN_FILE   where the shared relay token is kept (default ./relay-token)
  NEURO_DASHBOARD_LISTEN   neuro-dashboard: where it listens (default 127.0.0.1:8310)
  NEURO_DASHBOARD_SERVER   neuro-dashboard: the server it forwards to (default http://127.0.0.1:8300)
  NEURO_HEADLESS=1         no display session: mouse/keyboard refuse to run and
                           Neuro is told to use shell_command instead
  NEURO_SHELL_ALLOWLIST    comma-separated programs the shell action may run
  NEURO_DENY_ACTIONS       comma-separated actions that never run
  NEURO_AUDIT_LOG          JSON-lines file with every action decision
  NEURO_PERMISSIONS_FILE   path to the policy file

Safety
------
Nothing runs unless the permission policy allows it: hard denials, the deny
list, per-scope budgets (input/filesystem/process/network/system/vision/game/
shell), a rate limit, the shell firewall and the kill switch are all evaluated
for every action, and the decision is written to the audit log when NEURO_AUDIT_LOG
is set. The dashboard can pause everything or flip the kill switch at any time.
Screenshots travel as image bytes inside the agent's reply. The server does not
read a file path that the agent names.
