# Neuro Desktop Manager

The manager is its own GTK 4 desktop application. It uses native widgets and a
native startup splash. It does not embed a browser. The HTTP dashboard remains
available for remote administration and troubleshooting.

The manager connects to the server over its existing authenticated API. Your
dashboard token stays in memory until sign-out or exit. No token is written to a
URL, process argument, or preferences file. The server still enforces permissions.

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
`neuro-manager.exe`. macOS produces `Neuro Desktop Manager.app` plus a command
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
