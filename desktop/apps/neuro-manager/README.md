# Neurcro Manager

The manager is its own GTK 4 desktop application. It uses native widgets and a
native startup splash. It does not embed a browser. The HTTP dashboard remains
available for remote administration and troubleshooting.

The manager connects to the server over its existing authenticated API. Your
dashboard token stays in memory until sign-out or exit. No token is written to a
URL, process argument, or preferences file. The server still enforces permissions.
Requests connect directly to the configured server; ambient HTTP proxy settings
are ignored so session and approval headers cannot reach an unrelated proxy.
Sign-out or an expired session closes approval dialogs and clears their fields,
including one-time credential output.

## Connect and approve an integration

Start `neuro-integration` and complete its `setup` command first. Enter the
server's HTTP origin and the operator token printed during setup on the manager's
sign-in screen. Starting the manager alone does not start the server or client.

1. Open **Marketplace → Connect an existing integration…**. Paste
   `{"manifest": <your SDK manifest object>}` into the JSON editor, re-enter your
   operator token, then choose **Approve and apply**. Existing game or Discord
   backends continue using their own connection to the official Neuro API.
2. Open **Permissions** and approve the global `extensions` scope, plus any global
   scopes needed by the manifest. Saving needs fresh operator-token entry.
3. In **Marketplace → Your connected ecosystem**, choose **Permissions…** for
   that integration. Approve its runtime, required capabilities and settings.
   Enable management delegation only when you want Neuro and Evil to manage it.
4. Choose **Connection token…** and copy the returned `token` once into your
   adapter's local credential storage. This scoped token is separate from the
   operator and viewer tokens. Your adapter reports readiness through the
   [SDK](https://github.com/cassitly/neuro-desktop-sdk); registration alone leaves
   it offline. Matching ready adapters appear in the Games control decisions.

Marketplace source downloads fetch a reviewed commit; they do not install a
game mod or run its setup commands. Follow the listing's **Open setup
instructions** link and prerequisites. The **Extensions** tab manages the signed
runtime catalog separately. New installs start disabled. See
[extension permissions](../../../docs/EXTENSION_PERMISSIONS.md) and the
[integration workflow](../../../docs/WORKFLOW.md).

**Extensions → Built-in services** contains the Vision Service controls even
when the catalog is empty. You can disable image use globally or for either
twin with fresh operator approval. Review the [connection persona and privacy
guide](../../../docs/VISION.md) before using independent per-twin settings.

For the [Vencord adapter](https://github.com/cassitly/neuro-desktop-vencord), use
its `manifest.json` and approve `discord.viewer`. Its plugin needs a development
Vencord build and a scoped connection token; it does not provide Vedal's private
Discord backend. Choose **Viewer token…** for a separate OBS read credential.
Set **Viewer visibility…** to `{"visible":true,"actor":"neuro","minutes":15}`
only when ready to share; `evil` selects Evil. The local overlay is
`http://127.0.0.1:8300/viewer/#integration=YOUR_ID&token=YOUR_VIEWER_TOKEN`.
Sharing starts hidden and needs a ready, approved adapter. Revocation clears the
feed; reapproval does not restore an earlier sharing session.

Before Neuro or Evil uses the manager, create a restricted sign-in from
**Status → Create Neuro/Evil manager sign-in…**, then sign out of the operator
session on that desktop. Follow the
[delegated-session guide](../../../docs/DELEGATED_MANAGER.md) for expiry and
revocation. Only the operator session can change approvals or create credentials.

## Run and build

From `desktop/`, stage a source build with:

```sh
python3 scripts/build-manager.py
./dist/neuro-manager/neuro-manager --server http://127.0.0.1:8300
```

On Windows use `python scripts/build-manager.py`, then
`dist\neuro-manager\neuro-manager.cmd`. Set `NEURO_MANAGER_PYTHON` to select a
GTK-enabled interpreter. The agent's Python environment is independent.

To include Python, GTK and their native libraries in the application:

```sh
python3 scripts/build-manager.py --standalone
```

Build on each target OS and architecture; PyInstaller does not cross-compile.
Linux and Windows produce a directory containing `neuro-manager` or
`neuro-manager.exe`. macOS produces `Neurcro Manager.app` plus a command
line launcher. Keep the directory together. Distribution signing and macOS
notarization require the publisher's own credentials.

`scripts/bundle/prod.sh` and `prod.ps1` include the standalone manager. Development
bundles stage source. `NEURO_MANAGER_SOURCE_ONLY=1` explicitly requests source
staging for a release bundle, for example on a CI runner without GTK. Set
`NEURO_NO_MANAGER=1` when launching a server without a graphical session.

## Build dependencies

- Ubuntu/Debian: `sudo apt install python3-gi python3-gi-cairo gir1.2-gtk-4.0 python3-venv`
- Arch Linux: `sudo pacman -S python-gobject gtk4`
- macOS: `brew install pygobject3 gtk4`; use Homebrew's Python that can `import gi`.
- Windows: use the MSYS2 UCRT64 shell and install
  `mingw-w64-ucrt-x86_64-gtk4 mingw-w64-ucrt-x86_64-python mingw-w64-ucrt-x86_64-python-gobject mingw-w64-ucrt-x86_64-pyinstaller`.

See the [PyGObject setup guide](https://pygobject.gnome.org/getting_started.html)
and [PyInstaller GTK configuration](https://pyinstaller.org/en/stable/hooks-config.html).
The standalone build bundles only the selected GTK theme, icons and English
translations rather than collecting every installed desktop theme.

## Resource use and checks

Network requests run on a bounded worker pool. GTK changes run on the main
thread. Only the visible page refreshes, with one refresh in progress at a time:
five seconds while focused, thirty seconds when unfocused, and up to sixty
seconds during connection failures. Unchanged data does not rebuild widgets.
The manager requests screenshots only when asked; it does not poll the vision
runtime. The splash closes when startup work finishes, without an artificial
minimum delay.

An October 2026 Linux source run reached sign-in in 281 ms and measured 0.0%
CPU over a ten-second idle sample, 128.1 MiB RSS and 72.7 MiB proportional memory.
These are local measurements, not guarantees for other machines or platforms.

```sh
PYTHONPATH=apps/neuro-manager python3 -m unittest discover -s apps/neuro-manager/tests
NEURO_MANAGER_GUI_TESTS=1 PYTHONPATH=apps/neuro-manager python3 -m unittest discover -s apps/neuro-manager/tests
./dist/neuro-manager/neuro-manager --smoke-test --diagnostics
```

The opt-in GTK test opens actual native views against an isolated fake server,
checks controls and policy editing, and sends no keyboard or mouse actions to
the desktop. Linux CI uses Xvfb. Windows and macOS need their own native builds
and checks; a Linux pass alone does not verify them.
