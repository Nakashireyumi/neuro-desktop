import argparse
import os
import sys
from pathlib import Path

from .client import server_url


def main():
    parser = argparse.ArgumentParser(description="Neurcro's native GTK manager")
    parser.add_argument("--server", default=os.environ.get("NEURO_DASHBOARD_SERVER", "http://127.0.0.1:8300"))
    parser.add_argument("--diagnostics", action="store_true", help="print startup timing and request counts, never credentials")
    parser.add_argument("--smoke-test", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    try:
        server = server_url(args.server)
        if sys.platform == "win32" and not getattr(sys, "frozen", False):
            os.environ.setdefault("PYGI_DLL_PATH", str(Path(sys.executable).parent))
        from .app import ManagerApplication
    except (ImportError, ValueError) as error:
        print(f"neuro-manager: {error}\nThe native manager needs GTK 4 and PyGObject for your Python interpreter.", file=sys.stderr)
        return 1
    from gi.repository import Gtk
    if not Gtk.init_check():
        print("neuro-manager: Cannot open a graphical display. Start the manager in your desktop session; use NEURO_NO_MANAGER=1 on a server.", file=sys.stderr)
        return 1
    return ManagerApplication(server, diagnostics=args.diagnostics, smoke=args.smoke_test).run([sys.argv[0]])


if __name__ == "__main__":
    raise SystemExit(main())
