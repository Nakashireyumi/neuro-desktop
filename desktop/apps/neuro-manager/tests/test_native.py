"""Opt-in real GTK smoke test. Run under Xvfb in CI; no desktop input is sent."""

import copy
from contextlib import contextmanager
import json
import os
import threading
import time
from types import SimpleNamespace
import unittest
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

GUI_TESTS = os.environ.get("NEURO_MANAGER_GUI_TESTS") == "1"
if GUI_TESTS:
    from gi.repository import GLib, Gtk
    from neuro_manager.app import ManagerApplication
    from neuro_manager.operator_dialog import json_action


@unittest.skipUnless(GUI_TESTS, "set NEURO_MANAGER_GUI_TESTS=1 inside a graphical session or Xvfb")
class NativeManagerTests(unittest.TestCase):
    def trace(self, stage):
        # Native Windows failures may terminate the process without a Python
        # traceback. These bounded markers locate the failing UI transition.
        if os.environ.get("CI"):
            print(f"[native manager] {stage}", flush=True)

    def pump(self, predicate, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            while GLib.MainContext.default().pending():
                GLib.MainContext.default().iteration(False)
            if predicate():
                return
            time.sleep(0.01)
        self.fail("native manager did not reach the expected state")

    def widgets(self, parent, kind):
        found = [parent] if isinstance(parent, kind) else []
        child = parent.get_first_child()
        while child:
            found.extend(self.widgets(child, kind))
            child = child.get_next_sibling()
        return found

    def buffer_text(self, view):
        buffer = view.get_buffer()
        return buffer.get_text(*buffer.get_bounds(), True)

    def dialog(self, app, title):
        windows = Gtk.Window.get_toplevels()
        for index in range(windows.get_n_items()):
            window = windows.get_item(index)
            if window.get_transient_for() is app.window and window.get_title() == title and window.get_visible():
                return window
        return None

    def click(self, parent, title):
        widget = next(button for button in self.widgets(parent, Gtk.Button) if button.get_label() == title)
        self.assertTrue(widget.get_sensitive())
        widget.emit("clicked")

    @contextmanager
    def session_fixture(self, *, sign_in=True):
        """A real GTK manager with a bounded, isolated credential-test server."""
        state = SimpleNamespace(
            valid=True, seen=[], hold_token=False, token_started=threading.Event(),
            token_release=threading.Event(), token_finished=threading.Event(),
            minted_token="fixture-once-only-scoped-token-0123456789",
        )
        status = {"version": "test", "uptime": 0, "control": {"paused": False}, "executor": {"connected": True}, "neuro": {"connected": True}}

        class Handler(BaseHTTPRequestHandler):
            def handle_request(self):
                state.seen.append((self.command, self.path, self.headers.get("X-ND-Operator-Token")))
                if self.path != "/health" and (not state.valid or self.headers.get("X-ND-Token") != "fixture-token"):
                    self.send_response(401)
                    self.end_headers()
                    self.wfile.write(b'{"error":"fixture session expired"}')
                    return
                self.rfile.read(int(self.headers.get("Content-Length", "0")))
                if self.path == "/api/integrations/fixture/token":
                    if self.headers.get("X-ND-Operator-Token") != "fixture-token":
                        self.send_response(403)
                        self.end_headers()
                        self.wfile.write(b'{"error":"fresh operator approval required"}')
                        return
                    state.token_started.set()
                    if state.hold_token:
                        state.token_release.wait(8)
                    result = {"ok": True, "token": state.minted_token}
                else:
                    result = {
                        "/health": {"ok": True},
                        "/api/session": {"ok": True, "role": "operator"},
                        "/api/status": status,
                        "/api/extensions": {"installed": [], "catalog": [{"id": "memory", "name": "Memory"}]},
                        "/api/permissions": {"default_allow": False, "scopes": {"input": True}},
                        "/api/permission-requests": {"pending": [], "grants": []},
                        "/api/extensions/memory/permissions": {
                            "ok": True, "policy": {"manage_allowed": False, "run_allowed": False, "grants": {}, "settings": {}},
                            "permissions": [{"id": "input", "description": "Fixture capability", "required": True}],
                        },
                    }.get(self.path, {"ok": True})
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(result).encode())
                if self.path == "/api/integrations/fixture/token":
                    state.token_finished.set()

            do_GET = do_POST = do_PUT = handle_request

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        app = ManagerApplication(f"http://127.0.0.1:{server.server_port}", non_unique=True)
        # GApplication registers an object path even with NON_UNIQUE. Each test
        # needs its own path on the process's shared desktop-session bus.
        app.set_application_id("io.neurodesktop.Manager.Test" + uuid.uuid4().hex)
        app.register(None)
        app.activate()
        try:
            self.pump(lambda: app.window is not None and app.window.get_mapped())
            if sign_in:
                app.token_entry.set_text("fixture-token")
                app.sign_in()
                self.pump(lambda: app.client is not None and not app.refresh_pending and hasattr(app, "extension_items"))
            yield app, state
        finally:
            state.token_release.set()
            app.on_close()
            app.pool.shutdown(wait=True, cancel_futures=True)
            if app.window:
                app.window.destroy()
            if app.splash:
                app.splash.destroy()
            server.shutdown()
            server.server_close()
            thread.join()
            while GLib.MainContext.default().pending():
                GLib.MainContext.default().iteration(False)

    def open_credential_dialog(self, app):
        title = "Create fixture scoped token"
        json_action(app, title, "/api/integrations/fixture/token", {"kind": "integration"})
        dialog = self.dialog(app, title)
        self.assertIsNotNone(dialog)
        entry = self.widgets(dialog, Gtk.PasswordEntry)[0]
        self.assertEqual(entry.get_text(), "", "fresh approval must never copy a signed-in token")
        output = next(view for view in self.widgets(dialog, Gtk.TextView) if not view.get_editable())
        return dialog, entry, output

    def focus_entry(self, window, entry):
        entry.grab_focus()
        self.pump(lambda: window.get_focus() is entry.get_delegate())

    def test_focused_approval_closes_after_focus_out_and_clears_credentials(self):
        with self.session_fixture() as (app, state):
            for sign_out in (False, True):
                with self.subTest(sign_out=sign_out):
                    dialog, approval, output = self.open_credential_dialog(app)
                    approval.set_text("fixture-unsaved-approval")
                    output.get_buffer().set_text(state.minted_token)
                    self.focus_entry(dialog, approval)
                    cleanup = app.session_dialogs[dialog]
                    focus_during_cleanup = []
                    focus_when_cleared = []

                    def observed_clear(*_):
                        if not approval.get_text():
                            focus_when_cleared.append(dialog.get_focus())

                    approval.connect("notify::text", observed_clear)

                    def observed_cleanup():
                        # A native IME must detach while its entry still has a
                        # root/surface, before credentials and windows go away.
                        focus_during_cleanup.append(dialog.get_focus())
                        cleanup()

                    app.session_dialogs[dialog] = observed_cleanup
                    app.sign_out() if sign_out else dialog.close()
                    self.assertEqual(focus_during_cleanup, [None])
                    self.assertTrue(focus_when_cleared)
                    self.assertTrue(all(focus is None for focus in focus_when_cleared))
                    self.assertEqual(approval.get_text(), "")
                    self.assertEqual(self.buffer_text(output), "")
                    self.assertFalse(dialog.get_visible())
                    self.assertFalse(app.session_dialogs)

    def test_invalid_pasted_token_is_handled_without_echoing_credentials(self):
        with self.session_fixture(sign_in=False) as (app, state):
            for token in ("fixture-private-credential-\u2603", "fixture-private-credential\r\nInjected: secret", "x" * 513):
                with self.subTest(token_type="unicode" if "\u2603" in token else "multiline" if "\n" in token else "overlong"):
                    app.token_entry.set_text(token)
                    app.sign_in()
                    self.assertIsNone(app.client)
                    self.assertTrue(app.login_button.get_sensitive())
                    self.assertIn("single-line token", app.login_error.get_text())
                    self.assertNotIn("fixture-private-credential", app.login_error.get_text())
                    self.assertNotIn("x" * 32, app.login_error.get_text())
            self.assertFalse(any(path == "/api/session" for _, path, _ in state.seen))
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: app.client is not None and not app.refresh_pending)
            self.assertEqual(app.token_entry.get_text(), "")

    def test_signout_closes_approval_dialogs_and_clears_all_credential_widgets(self):
        with self.session_fixture() as (app, state):
            app.pages.set_visible_child_name("permissions")
            self.pump(lambda: hasattr(app, "policy_operator_token") and not app.refresh_pending)
            global_entry = app.policy_operator_token
            global_entry.set_text("fixture-unsaved-global-approval")
            dialog, approval, output = self.open_credential_dialog(app)
            approval.set_text("fixture-token")
            self.click(dialog, "Approve and apply")
            self.pump(lambda: state.minted_token in self.buffer_text(output))
            self.assertIn(("POST", "/api/integrations/fixture/token", "fixture-token"), state.seen)
            approval.set_text("fixture-unsaved-dialog-approval")

            app.extension_permissions("memory")
            self.pump(lambda: self.dialog(app, "Permissions · memory") is not None)
            extension_dialog = self.dialog(app, "Permissions · memory")
            extension_approval = self.widgets(extension_dialog, Gtk.PasswordEntry)[0]
            extension_approval.set_text("fixture-unsaved-extension-approval")
            settings = next(view for view in self.widgets(extension_dialog, Gtk.TextView) if view.get_editable())
            settings.get_buffer().set_text('{"fixture":"unsaved settings"}')
            previous_client = app.client

            app.sign_out()

            self.assertIsNone(app.client)
            self.assertEqual(previous_client.token, "")
            self.assertEqual(global_entry.get_text(), "")
            self.assertEqual(approval.get_text(), "")
            self.assertEqual(extension_approval.get_text(), "")
            self.assertEqual(self.buffer_text(output), "")
            self.assertEqual(self.buffer_text(settings), "")
            self.assertFalse(dialog.get_visible())
            self.assertFalse(extension_dialog.get_visible())
            self.assertFalse(app.session_dialogs)

    def test_expired_session_closes_modal_and_discards_late_token_response(self):
        with self.session_fixture() as (app, state):
            dialog, approval, output = self.open_credential_dialog(app)
            state.hold_token = True
            approval.set_text("fixture-token")
            self.focus_entry(dialog, approval)
            self.click(dialog, "Approve and apply")
            self.pump(state.token_started.is_set)

            # Expire the session while a separate, already-authorized credential
            # request remains in flight. The resulting 401 comes through submit.
            state.valid = False
            app.refresh(force=True)
            self.pump(lambda: app.client is None)
            self.assertFalse(dialog.get_visible())
            self.assertEqual(approval.get_text(), "")
            self.assertEqual(self.buffer_text(output), "")
            self.assertIn("expired", app.login_error.get_text())

            state.token_release.set()
            self.pump(state.token_finished.is_set)
            # Join workers before draining GTK callbacks, so this assertion
            # actually observes delivery of the delayed successful response.
            app.pool.shutdown(wait=True, cancel_futures=False)
            while GLib.MainContext.default().pending():
                GLib.MainContext.default().iteration(False)
            self.assertIsNone(app.client)
            self.assertEqual(self.buffer_text(output), "")
            self.assertFalse(dialog.get_visible())
            self.assertFalse(app.session_dialogs)

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
        app.set_application_id("io.neurodesktop.Manager.Test" + uuid.uuid4().hex)
        app.register(None)
        app.activate()
        try:
            self.trace("main views: starting")
            self.pump(lambda: app.window is not None)
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: hasattr(app, "extension_items"))
            self.trace("main views: signed in")
            self.assertEqual(app.extension_items["memory"]["name"], "Memory")
            self.assertEqual(app.token_entry.get_text(), "")
            app.toggle_pause()
            self.pump(lambda: app.status.get("control", {}).get("paused") is True)
            self.assertEqual(app.control_label.get_text(), "Actions paused")
            app.perform("release", "/api/games/release")
            self.pump(lambda: "release" not in app.busy)
            app.toggle_pause()
            self.pump(lambda: app.status.get("control", {}).get("paused") is False)
            self.trace("main views: controls complete")
            for view, attribute in [("marketplace", "marketplace_data"), ("games", "profiles"), ("permissions", "scope_widgets"), ("activity", "audit_list"), ("status", "config")]:
                self.trace("main views: opening " + view)
                app.pages.set_visible_child_name(view)
                self.pump(lambda name=attribute: bool(getattr(app, name, None)))
            self.trace("main views: editing permissions")
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
            self.trace("main views: policy saved")
            self.assertFalse(policy["scopes"]["input"]["allowed"])
            self.assertEqual(policy["metadata"], {"owner": "operator"})
            app.sign_out()
            self.trace("main views: signed out")
            self.assertIsNone(app.client)
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: app.client is not None and not app.refresh_pending)
            self.trace("main views: signed back in")
            app.pages.set_visible_child_name("permissions")
            self.pump(lambda: app.page_boxes["permissions"].get_first_child() is not None)
            self.assertIn(("POST", "/api/games/release"), seen)
            self.assertNotIn(("GET", "/api/runtime"), seen)
            app.sign_out()
            self.trace("main views: switching to delegated session")
            session_role["role"] = "neuro"
            status["control"]["paused"] = True
            app.token_entry.set_text("fixture-token")
            app.sign_in()
            self.pump(lambda: app.client is not None and not app.refresh_pending)
            self.trace("main views: delegated session ready")
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
