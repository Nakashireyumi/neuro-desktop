# Build on the target OS. GTK's own libraries are bundled, without a web engine.
import sys
from pathlib import Path

source = Path(SPECPATH)
analysis = Analysis(
    [str(source / "neuro-manager")], pathex=[str(source)],
    binaries=[], datas=[], hiddenimports=[],
    excludes=["tkinter", "PyQt5", "PyQt6", "PySide6", "numpy", "matplotlib"],
    hooksconfig={"gi": {
        "module-versions": {"Gtk": "4.0", "Gdk": "4.0"},
        "icons": ["Adwaita"], "themes": ["Default"], "languages": ["en_US", "en_GB"],
    }},
)
pyz = PYZ(analysis.pure)
exe = EXE(pyz, analysis.scripts, [], exclude_binaries=True, name="neuro-manager", console=False)
collection = COLLECT(exe, analysis.binaries, analysis.datas, name="neuro-manager")
if sys.platform == "darwin":
    application = BUNDLE(collection, name="Neuro Desktop Manager.app", bundle_identifier="io.neurodesktop.Manager")
