#!/usr/bin/env python3
"""Stage the native manager, or bundle Python and GTK on the target OS."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "apps" / "neuro-manager"
MARKER = ".neuro-manager-build"
PYINSTALLER_VERSION = "6.22.3"


def run(*args):
    subprocess.run([str(arg) for arg in args], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--standalone", action="store_true", help="bundle the Python and GTK runtime (build on the target OS)")
    parser.add_argument("--out", type=Path, default=Path(os.environ.get("NEURO_MANAGER_OUT", ROOT / "dist" / "neuro-manager")))
    args = parser.parse_args()
    out = args.out.resolve()
    if out.exists() and any(out.iterdir()) and not (out / MARKER).exists():
        parser.error(f"{out} is not a generated manager folder; choose an empty output directory")
    out.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".manager-stage-", dir=out.parent) as temporary:
        stage = Path(temporary) / "manager"
        stage.mkdir()
        if args.standalone:
            python = os.environ.get("NEURO_MANAGER_PYTHON", sys.executable)
            run(python, "-c", "import gi; gi.require_version('Gtk', '4.0'); from gi.repository import Gtk")
            work = ROOT / "dist" / ".manager-build"
            work.mkdir(parents=True, exist_ok=True)
            if subprocess.run([python, "-c", "import PyInstaller"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                venv = work / "venv"
                run(python, "-m", "venv", "--system-site-packages", venv)
                python = venv / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
                run(python, "-m", "pip", "install", "--quiet", f"pyinstaller=={PYINSTALLER_VERSION}")
            run(python, "-m", "PyInstaller", "--noconfirm", "--distpath", work / "dist", "--workpath", work / "work", SOURCE / "neuro-manager.spec")
            if sys.platform == "darwin":
                shutil.copytree(work / "dist" / "Neurcro Manager.app", stage / "Neurcro Manager.app", symlinks=True)
                (stage / "neuro-manager").write_text('#!/bin/sh\nHERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"\nexec "$HERE/Neurcro Manager.app/Contents/MacOS/neuro-manager" "$@"\n')
                (stage / "neuro-manager").chmod(0o755)
            else:
                shutil.copytree(work / "dist" / "neuro-manager", stage, dirs_exist_ok=True, symlinks=True)
        else:
            shutil.copytree(SOURCE / "neuro_manager", stage / "neuro_manager", ignore=shutil.ignore_patterns("__pycache__"))
            shutil.copyfile(SOURCE / "neuro-manager", stage / "launch.py")
            (stage / "neuro-manager").write_text('''#!/bin/sh
HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PY="${NEURO_MANAGER_PYTHON:-python3}"
if ! "$PY" -c 'import gi; gi.require_version("Gtk", "4.0")' 2>/dev/null; then
  if [ -z "${NEURO_MANAGER_PYTHON:-}" ] && /usr/bin/python3 -c 'import gi; gi.require_version("Gtk", "4.0")' 2>/dev/null; then
    PY=/usr/bin/python3
  else
    echo "Neurcro Manager needs GTK 4 and PyGObject. See manager/README.md." >&2
    exit 1
  fi
fi
exec "$PY" "$HERE/launch.py" "$@"
''')
            (stage / "neuro-manager").chmod(0o755)
            (stage / "neuro-manager.cmd").write_text('@echo off\nif defined NEURO_MANAGER_PYTHON (\n  "%NEURO_MANAGER_PYTHON%" "%~dp0launch.py" %*\n) else if exist "C:\\msys64\\ucrt64\\bin\\python.exe" (\n  "C:\\msys64\\ucrt64\\bin\\python.exe" "%~dp0launch.py" %*\n) else (\n  python "%~dp0launch.py" %*\n)\n')
        if (SOURCE / "README.md").exists():
            shutil.copyfile(SOURCE / "README.md", stage / "README.md")
        (stage / MARKER).write_text("Native Neurcro manager build output\n")
        # Stage the complete replacement first; unrelated directories are refused above.
        if out.exists():
            shutil.rmtree(out)
        shutil.move(stage, out)
    print(f"Native manager: {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
