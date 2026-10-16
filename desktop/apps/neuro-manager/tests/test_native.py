"""Opt-in real GTK smoke test. Run under Xvfb in CI; no desktop input is sent."""

import copy
import json
import os
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

GUI_TESTS = os.environ.get("NEURO_MANAGER_GUI_TESTS") == "1"
if GUI_TESTS:
    from gi.repository import GLib
    from neuro_manager.app import ManagerApplication


@unittest.skipUnless(GUI_TESTS, "set NEURO_MANAGER_GUI_TESTS=1 inside a graphical session or Xvfb")
class NativeManagerTests(unittest.TestCase):
    def pump(self, predicate, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            while GLib.MainContext.default().pending():
                GLib.MainContext.default().iteration(False)
            if predicate():
                return
            time.sleep(0.01)
        self.fail("native manager did not reach the expected state")

    def test_native_views_controls_and_unsaved_policy(self):
        status = {"version": "test", "uptime": 0, "control": {"paused": False}, "executor": {"connected": True, "version": "2"}, "neuro": {"connected": True}}
        policy = {"default_allow": False, "scopes": {"input": True, "shell": False}, "metadata": {"owner": "operator"}}
        seen = []
        session_role = {"role": "operator"}

        class Handler(BaseHTTPRequestHandler):
            def handle_request(self):
                seen.append((self.command, self.path))
                if self.path != "/health" and self.headers.get("X-ND-Token") != "fixture-token":
                    self.send_response(401)
                    self.end_headers()
                    self.wfile.write(b'{"error":"invalid token"}')
                    return
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or "{}")
                if self.path == "/api/permissions" and self.command == "PUT" and self.headers.get("X-ND-Operator-Token") != "fixture-token":
                    self.send_response(403)
                    self.end_headers()
                    self.wfile.write(b'{"error":"fresh operator approval required"}')
                    return
                if self.path == "/api/control/pause":
                    status["control"]["paused"] = True
                elif self.path == "/api/control/resume":
                    status["control"]["paused"] = False
                elif self.path == "/api/permissions" and self.command == "PUT":
                    policy.clear()
                    policy.update(body)
                result = {
                    "/health": {"ok": True}, "/api/session": {"ok": True, **session_role},
                    "/api/status": status,
                    "/api/extensions": {"installed": [], "catalog": [{"id": "memory", "name": "Memory", "description": "Test extension", "signature_state": "verified"}]},
                    "/api/marketplace": {"items": [{"id": "game-source", "name": "Game source", "description": "Manual game integration", "repository": "https://example.com/game", "source_commit": "a" * 40, "license": {"spdx": "MIT"}, "can_stage_source": True}], "integrations": []},
                    "/api/games": {"profiles": [{"id": "generic", "name": "Generic game", "keys": {"forward": "w"}}], "session": None},
                    "/api/permissions": policy,
                    "/api/permission-requests": {"pending": [], "grants": []},
                    "/api/audit?limit=100": {"enabled": True, "entries": [{"event": "control", "paused": False, "time": "2026-10-15T00:00:00Z"}]},
                    "/api/config": {"paths": {"permissions": "permissions.json"}},
                }.get(self.path, {"ok": True, "message": "Applied"})
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(result).encode())

            do_GET = do_POST = do_PUT = do_DELETE = handle_request

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        app = ManagerApplication(f"http://127.0.0.1:{server.server_port}", non_unique=True)
        app.register(None)
        app.activate()
        try:
            self.pump(lambda: app.window is not None)
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: hasattr(app, "extension_items"))
            self.assertEqual(app.extension_items["memory"]["name"], "Memory")
            self.assertEqual(app.token_entry.get_text(), "")
            app.toggle_pause()
            self.pump(lambda: app.status.get("control", {}).get("paused") is True)
            self.assertEqual(app.control_label.get_text(), "Actions paused")
            app.perform("release", "/api/games/release")
            self.pump(lambda: "release" not in app.busy)
            app.toggle_pause()
            self.pump(lambda: app.status.get("control", {}).get("paused") is False)
            for view, attribute in [("marketplace", "marketplace_data"), ("games", "profiles"), ("permissions", "scope_widgets"), ("activity", "audit_list"), ("status", "config")]:
                app.pages.set_visible_child_name(view)
                self.pump(lambda name=attribute: bool(getattr(app, name, None)))
            app.pages.set_visible_child_name("permissions")
            app.scope_widgets["input"][0].set_active(False)
            self.assertTrue(app.policy_dirty)
            app.refresh(force=True)
            self.pump(lambda: not app.refresh_pending)
            self.assertFalse(app.scope_widgets["input"][0].get_active())
            app.save_policy()
            self.assertTrue(app.policy_dirty, "a signed-in session must not fill in fresh approval")
            app.policy_operator_token.set_text("fixture-token")
            app.save_policy()
            self.pump(lambda: not app.policy_dirty)
            self.assertFalse(policy["scopes"]["input"]["allowed"])
            self.assertEqual(policy["metadata"], {"owner": "operator"})
            app.sign_out()
            self.assertIsNone(app.client)
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: app.client is not None and not app.refresh_pending)
            app.pages.set_visible_child_name("permissions")
            self.pump(lambda: app.page_boxes["permissions"].get_first_child() is not None)
            self.assertIn(("POST", "/api/games/release"), seen)
            self.assertNotIn(("GET", "/api/runtime"), seen)
            app.sign_out()
            session_role["role"] = "neuro"
            status["control"]["paused"] = True
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: app.client is not None and not app.refresh_pending)
            self.assertIsNone(app.pages.get_child_by_name("permissions"))
            self.assertIsNone(app.pages.get_child_by_name("activity"))
            self.assertFalse(app.pause_button.get_sensitive(), "a delegated session cannot resume an operator pause")
            app.pages.set_visible_child_name("marketplace")
            self.pump(lambda: app.pages.get_visible_child_name() == "marketplace" and not app.refresh_pending)
            self.assertEqual(app.marketplace_data["items"][0]["id"], "game-source")
        finally:
            app.on_close()
            if app.window:
                app.window.destroy()
            if app.splash:
                app.splash.destroy()
            server.shutdown()
            server.server_close()
            thread.join()
