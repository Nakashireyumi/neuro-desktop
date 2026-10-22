"""Event-driven GTK manager. HTTP never runs on GTK's main thread."""

from __future__ import annotations

import copy
import json
import re
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime
from urllib.parse import quote

import gi

gi.require_version("Gtk", "4.0")
gi.require_version("Gdk", "4.0")
from gi.repository import Gdk, Gio, GLib, Gtk

from . import __version__
from .client import ApiError, Client, SCOPES, external_url, normalise_policy, poll_delay

CSS = b"""
.nd-splash { background-image: linear-gradient(125deg, #111735 0%, #423ba5 48%, #e92d76 80%, #ff892b 100%); padding: 48px; }
.nd-splash label { color: white; }
.nd-splash .nd-brand { font-size: 40px; font-weight: 800; }
.nd-splash .nd-subtitle { font-size: 24px; }
.nd-heading { font-size: 22px; font-weight: 700; }
.nd-subheading { font-size: 16px; font-weight: 700; }
.nd-muted { opacity: 0.7; }
.nd-card { padding: 16px; border-radius: 10px; background: alpha(@theme_fg_color, 0.035); }
.nd-banner { padding: 10px 16px; background: alpha(#df8030, 0.15); }
.nd-controls { padding: 12px 18px; border-bottom: 1px solid alpha(@theme_fg_color, 0.12); }
.nd-stopped { background: alpha(#df8030, 0.15); }
.nd-list-button { padding: 10px 14px; }
"""


def box(vertical=True, spacing=10, margin=0):
    widget = Gtk.Box(orientation=Gtk.Orientation.VERTICAL if vertical else Gtk.Orientation.HORIZONTAL, spacing=spacing)
    for side in ("top", "bottom", "start", "end"):
        getattr(widget, "set_margin_" + side)(margin)
    return widget


def label(text="", style=None):
    widget = Gtk.Label(label=str(text), xalign=0, wrap=True)
    if style:
        widget.add_css_class(style)
    return widget


def clear(widget):
    child = widget.get_first_child()
    while child:
        following = child.get_next_sibling()
        widget.remove(child)
        child = following


def button(text, callback, style=None):
    widget = Gtk.Button(label=text)
    widget.connect("clicked", lambda _: callback())
    if style:
        widget.add_css_class(style)
    return widget


def text_view(text="", editable=False, height=120):
    view = Gtk.TextView(editable=editable, cursor_visible=editable, wrap_mode=Gtk.WrapMode.WORD_CHAR, monospace=True)
    view.get_buffer().set_text(text)
    scroll = Gtk.ScrolledWindow(min_content_height=height, child=view, hexpand=True, vexpand=True)
    return scroll, view


def credential_entry(**properties):
    """Create an ASCII-token field without Win32's native IME lifecycle."""
    entry = Gtk.PasswordEntry(show_peek_icon=True, **properties)
    if sys.platform == "win32":
        # GTK's Win32 IME can retain a native-window message filter across
        # credential-widget replacement. Tokens already require printable
        # ASCII, so only these fields can use GtkText's per-widget override.
        # Keep normal IME support in server, search and settings inputs.
        entry.get_delegate().set_property("im-module", "simple")
    return entry


class ManagerApplication(Gtk.Application):
    def __init__(self, server, diagnostics=False, non_unique=False, smoke=False):
        GLib.set_application_name("Neurcro Manager")
        super().__init__(application_id="io.neurodesktop.Manager", flags=Gio.ApplicationFlags.NON_UNIQUE if non_unique else Gio.ApplicationFlags(0))
        self.server = server
        self.diagnostics = diagnostics
        self.smoke = smoke
        self.started_at = time.monotonic()
        self.pool = ThreadPoolExecutor(max_workers=3, thread_name_prefix="nd-http")
        self.window = self.splash = self.client = None
        self.closed = False
        self.generation = self.revision = 0
        self.timer = 0
        self.refresh_pending = self.refresh_again = False
        self.failures = 0
        self.status = {}
        self.rendered = {}
        self.busy = set()
        self.policy = None
        self.policy_dirty = False
        self.config = None
        self.selected_extension = self.selected_game = ""
        self.extension_data = self.game_data = self.audit_data = {}
        self.observation = ""
        self.session_role = "operator"
        self.session_dialogs = {}
        self.connect("activate", self.on_activate)
        quit_action = Gio.SimpleAction.new("quit", None)
        quit_action.connect("activate", lambda *_: self.quit())
        self.add_action(quit_action)
        self.set_accels_for_action("app.quit", ["<Primary>q"])

    def submit(self, work, callback):
        generation = self.generation
        future = self.pool.submit(work)

        def done(completed):
            try:
                value, error = completed.result(), None
            except Exception as failure:
                value, error = None, failure
            GLib.idle_add(deliver, value, error)

        def deliver(value, error):
            if not self.closed and generation == self.generation:
                if self.client and isinstance(error, ApiError) and error.status == 401:
                    self.sign_out("Your manager session expired. Sign in again.")
                    return GLib.SOURCE_REMOVE
                callback(value, error)
            return GLib.SOURCE_REMOVE

        future.add_done_callback(done)

    def on_activate(self, _app):
        if self.window:
            self.window.present()
            return
        if self.splash:
            self.splash.present()
            return
        provider = Gtk.CssProvider()
        provider.load_from_data(CSS)
        Gtk.StyleContext.add_provider_for_display(Gdk.Display.get_default(), provider, Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
        self.splash = Gtk.ApplicationWindow(application=self, title="Starting Neurcro", decorated=False)
        self.splash.set_resizable(False)
        self.splash.set_default_size(660, 360)
        content = box(spacing=14)
        content.add_css_class("nd-splash")
        content.append(label("Neurcro", "nd-brand"))
        content.append(label("Manager", "nd-subtitle"))
        spacer = box()
        spacer.set_vexpand(True)
        content.append(spacer)
        self.spinner = Gtk.Spinner(spinning=True, halign=Gtk.Align.START)
        content.append(self.spinner)
        content.append(label("Starting the native manager…"))
        content.append(label(__version__))
        self.splash.set_child(content)
        self.splash.present()
        self.submit(lambda: Client(self.server).request("/health"), self.finish_startup)

    def finish_startup(self, _health, error):
        self.window = Gtk.ApplicationWindow(application=self, title="Neurcro Manager", default_width=1100, default_height=760)
        self.window.connect("notify::is-active", self.on_focus)
        self.window.connect("close-request", self.on_close)
        self.header = Gtk.HeaderBar()
        self.window.set_titlebar(self.header)
        self.show_signin(str(error) if error else "")
        self.window.present()
        self.spinner.stop()
        self.splash.close()
        self.splash = None
        if self.diagnostics:
            print(json.dumps({"event": "native_manager_ready", "startup_ms": round((time.monotonic() - self.started_at) * 1000), "toolkit": "GTK4"}), flush=True)
        if self.smoke:
            GLib.timeout_add(250, self.quit)

    def show_signin(self, reason=""):
        # Focus out while the old entry is still rooted. Win32's IME removes
        # its native message filter during focus-out, before widget teardown.
        self.window.set_focus(None)
        self.header.set_title_widget(label("Neurcro Manager", "nd-subheading"))
        if hasattr(self, "signout_button"):
            self.header.remove(self.signout_button)
            del self.signout_button
        center = box(spacing=18, margin=36)
        center.set_halign(Gtk.Align.CENTER)
        center.set_valign(Gtk.Align.CENTER)
        center.set_size_request(420, -1)
        center.append(label("Connect to Neurcro", "nd-heading"))
        center.append(label("Use your operator token from neuro-integration setup, or a limited Neuro/Evil manager token. It stays in memory until sign-out or exit.", "nd-muted"))
        center.append(label("Server"))
        self.server_entry = Gtk.Entry(text=self.server, placeholder_text="http://127.0.0.1:8300")
        center.append(self.server_entry)
        center.append(label("Dashboard token"))
        self.token_entry = credential_entry(hexpand=True)
        self.token_entry.connect("activate", lambda _: self.sign_in())
        center.append(self.token_entry)
        self.login_button = button("Sign in", self.sign_in, "suggested-action")
        center.append(self.login_button)
        self.login_error = label(reason)
        center.append(self.login_error)
        self.window.set_child(center)
        self.token_entry.grab_focus()

    def sign_in(self):
        token = self.token_entry.get_text().strip()
        if not token or not self.login_button.get_sensitive():
            return
        try:
            client = Client(self.server_entry.get_text(), token)
        except (ValueError, ApiError) as error:
            self.login_error.set_text(str(error))
            return
        self.login_button.set_sensitive(False)
        self.login_error.set_text("Connecting…")

        def connect():
            session = client.request("/api/session")
            return session, client.request("/api/status")

        def connected(result, error):
            self.login_button.set_sensitive(True)
            if error:
                client.token = ""
                self.login_error.set_text("That token was not accepted." if isinstance(error, ApiError) and error.status == 401 else str(error))
                return
            self.token_entry.set_text("")
            self.client = client
            self.server = client.server
            session, status = result
            self.session_role = session.get("role", "operator")
            self.build_manager()
            self.apply_status(status)
            self.refresh(force=True)

        self.submit(connect, connected)

    def sign_out(self, reason=""):
        self.generation += 1
        self.cancel_timer()
        self.close_session_dialogs()
        if self.client:
            self.client.token = ""
        self.client = None
        self.status = {}
        self.rendered.clear()
        self.policy = self.config = None
        self.policy_dirty = False
        self.refresh_pending = self.refresh_again = False
        self.busy.clear()
        self.extension_data = self.game_data = self.audit_data = {}
        self.observation = ""
        self.show_signin(reason)

    def track_session_dialog(self, dialog, cleanup):
        self.session_dialogs[dialog] = cleanup

        def closed(*_):
            dialog.set_focus(None)
            clean = self.session_dialogs.pop(dialog, None)
            if clean:
                clean()
            return False

        dialog.connect("close-request", closed)

    def close_session_dialogs(self):
        if self.window:
            self.window.set_focus(None)
        if hasattr(self, "policy_operator_token"):
            self.policy_operator_token.set_text("")
        dialogs = list(self.session_dialogs.items())
        self.session_dialogs.clear()
        for dialog, cleanup in dialogs:
            dialog.set_focus(None)
            cleanup()
            dialog.close()

    def build_manager(self):
        self.window.set_focus(None)
        for name in ("policy_editor", "request_box", "audit_list"):
            if hasattr(self, name):
                delattr(self, name)
        root = box(spacing=0)
        self.banner = label()
        self.banner.add_css_class("nd-banner")
        self.banner.set_visible(False)
        root.append(self.banner)
        controls = box(vertical=False, spacing=12)
        controls.add_css_class("nd-controls")
        control_text = box(spacing=4)
        control_text.set_hexpand(True)
        self.control_label = label("Checking controls…", "nd-subheading")
        self.connection_label = label("", "nd-muted")
        control_text.append(self.control_label)
        control_text.append(self.connection_label)
        controls.append(control_text)
        self.pause_button = button("Pause actions", self.toggle_pause, "destructive-action")
        self.release_button = button("Release all input", lambda: self.perform("release", "/api/games/release", widget=self.release_button))
        self.refresh_button = button("Refresh", lambda: self.refresh(force=True))
        for widget in (self.pause_button, self.release_button, self.refresh_button):
            controls.append(widget)
        root.append(controls)
        self.control_box = controls
        self.pages = Gtk.Stack(transition_type=Gtk.StackTransitionType.NONE, vexpand=True, hexpand=True)
        self.page_boxes = {}
        for name, title in (("extensions", "Extensions"), ("marketplace", "Marketplace"), ("games", "Games"), ("permissions", "Permissions"), ("activity", "Activity"), ("status", "Status")):
            if self.session_role != "operator" and name in ("permissions", "activity"):
                continue
            content = box(margin=20, spacing=14)
            scroll = Gtk.ScrolledWindow(child=content, hscrollbar_policy=Gtk.PolicyType.NEVER, vexpand=True)
            self.pages.add_titled(scroll, name, title)
            self.page_boxes[name] = content
        self.pages.connect("notify::visible-child-name", lambda *_: self.refresh(force=True))
        switcher = Gtk.StackSwitcher(stack=self.pages)
        self.header.set_title_widget(switcher)
        self.signout_button = button("Sign out", self.sign_out)
        self.header.pack_end(self.signout_button)
        root.append(self.pages)
        self.window.set_child(root)

    def notice(self, message=""):
        self.banner.set_text(message)
        self.banner.set_visible(bool(message))

    def apply_status(self, status, reachable=True):
        self.status = status
        control = status.get("control") or {}
        executor = status.get("executor") or {}
        neuro = status.get("neuro") or {}
        stopped = control.get("paused") or control.get("kill_switch_active")
        self.control_label.set_text("Connection lost" if not reachable else "Kill switch active" if control.get("kill_switch_active") else "Actions paused" if stopped else "Actions enabled")
        self.connection_label.set_text(f"Server {status.get('version', 'unknown')}  ·  Neuro {'connected' if neuro.get('connected') else 'waiting'}  ·  Client {'connected' if executor.get('connected') else 'offline'}")
        self.pause_button.set_label("Resume actions" if control.get("paused") else "Pause actions")
        self.pause_button.set_sensitive(reachable and not control.get("kill_switch_active", False) and "pause" not in self.busy and not (self.session_role != "operator" and control.get("paused")))
        self.release_button.set_sensitive(reachable and "release" not in self.busy)
        if stopped:
            self.control_box.add_css_class("nd-stopped")
        else:
            self.control_box.remove_css_class("nd-stopped")
        if control.get("kill_switch_active"):
            self.notice("Kill switch active: " + str(control.get("kill_switch_file", "")))

    def cancel_timer(self):
        if self.timer:
            GLib.source_remove(self.timer)
            self.timer = 0

    def schedule(self):
        self.cancel_timer()
        if self.client and not self.closed:
            self.timer = GLib.timeout_add_seconds(poll_delay(self.window.is_active(), self.failures), self.tick)

    def tick(self):
        self.timer = 0
        self.refresh()
        return GLib.SOURCE_REMOVE

    def on_focus(self, *_):
        if self.client:
            self.refresh(force=True) if self.window.is_active() else self.schedule()

    def refresh(self, force=False):
        if not self.client or self.closed:
            return
        if self.refresh_pending:
            self.refresh_again = self.refresh_again or force
            return
        self.cancel_timer()
        self.refresh_pending = True
        client, view, revision = self.client, self.pages.get_visible_child_name(), self.revision
        need_policy, need_config = self.policy is None, self.config is None

        def load():
            status = client.request("/api/status")
            data = {}
            if view == "extensions":
                data = client.request("/api/extensions")
            elif view == "marketplace":
                data = client.request("/api/marketplace")
            elif view == "games":
                data = client.request("/api/games")
            elif view == "permissions":
                data = {"requests": client.request("/api/permission-requests")}
                if need_policy:
                    data["policy"] = client.request("/api/permissions")
            elif view == "activity":
                data = client.request("/api/audit?limit=100")
            elif view == "status" and need_config:
                data = client.request("/api/config")
            return status, data

        def loaded(result, error):
            self.refresh_pending = False
            if error:
                if isinstance(error, ApiError) and error.status in (401, 503):
                    self.sign_out(str(error))
                    return
                self.failures += 1
                self.apply_status(self.status, reachable=False)
                self.notice(str(error) + " Retrying automatically.")
                self.schedule()
                return
            self.failures = 0
            if revision != self.revision:
                self.refresh(force=True)
                return
            status, data = result
            self.apply_status(status)
            if self.banner.get_text().endswith("Retrying automatically."):
                self.notice()
            if self.pages.get_visible_child_name() == view:
                shown_status = {
                    "version": status.get("version"), "minutes": int(status.get("uptime", 0)) // 60,
                    "neuro": {key: (status.get("neuro") or {}).get(key) for key in ("connected", "last_error")},
                    "executor": {key: (status.get("executor") or {}).get(key) for key in ("connected", "version", "completed_commands", "failed_commands")},
                    "relay": {key: (status.get("relay") or {}).get(key) for key in ("connected", "enabled", "peer_count")},
                }
                signature = json.dumps([shown_status, self.config, data] if view == "status" else data, sort_keys=True)
                if self.rendered.get(view) != signature:
                    self.rendered[view] = signature
                    getattr(self, "render_" + view)(data)
            again = self.refresh_again or self.pages.get_visible_child_name() != view
            self.refresh_again = False
            self.refresh(force=True) if again else self.schedule()

        self.submit(load, loaded)

    def perform(self, name, path, method="POST", body=None, widget=None, completed=None, *, operator_token=""):
        if not self.client or name in self.busy:
            return
        self.busy.add(name)
        self.revision += 1
        if widget:
            widget.set_sensitive(False)
        client = self.client

        def done(result, error):
            self.busy.discard(name)
            if widget:
                widget.set_sensitive(True)
            if error:
                if isinstance(error, ApiError) and error.status in (401, 503):
                    self.sign_out(str(error))
                    return
                self.notice(str(error))
            else:
                self.notice(result.get("message") or "Change applied.")
                if completed:
                    completed(result)
            self.rendered.clear()
            self.refresh(force=True)

        self.submit(lambda: client.request(path, method, body, operator_token=operator_token), done)

    def operator_change(self, title, path, body=None, method="POST"):
        from .operator_dialog import json_action
        json_action(self, title, path, body or {}, method=method, description="Approve this change with your operator token. Delegated manager sessions cannot change permissions.")

    def toggle_pause(self):
        paused = (self.status.get("control") or {}).get("paused", False)
        self.perform("pause", "/api/control/" + ("resume" if paused else "pause"), widget=self.pause_button)

    def render_extensions(self, data):
        self.extension_data = data
        parent = self.page_boxes["extensions"]
        clear(parent)
        parent.append(label("Extensions", "nd-heading"))
        parent.append(label("Install trusted catalog entries and choose which integrations run.", "nd-muted"))
        from .vision_dialog import service_card
        self.vision_card = service_card(self, (data.get("built_in_services") or {}).get("vision"))
        parent.append(self.vision_card)
        installed = {item["id"]: item for item in data.get("installed") or []}
        catalog = {item["id"]: item for item in data.get("catalog") or []}
        items = {**catalog, **{key: {**catalog.get(key, {}), **value} for key, value in installed.items()}}
        self.extension_items, self.installed_extensions = items, installed
        if not items:
            parent.append(label("No extensions in this catalog."))
            return
        row = box(vertical=False, spacing=20)
        choices = box(spacing=6)
        choices.set_size_request(230, -1)
        self.extension_details = box(spacing=14)
        self.extension_details.set_hexpand(True)
        for item_id, item in items.items():
            choices.append(button(item.get("name", item_id), lambda key=item_id: self.select_extension(key), "nd-list-button"))
        row.append(choices)
        row.append(self.extension_details)
        parent.append(row)
        self.select_extension(self.selected_extension if self.selected_extension in items else next(iter(items)))

    def select_extension(self, item_id):
        self.selected_extension = item_id
        item = self.extension_items[item_id]
        installed = self.installed_extensions.get(item_id)
        target = self.extension_details
        clear(target)
        target.append(label(item.get("name", item_id), "nd-subheading"))
        target.append(label(item.get("description", "No description provided.")))
        target.append(label(f"{'Enabled' if installed and installed.get('enabled') else 'Disabled' if installed else 'Not installed'}  ·  Signature: {item.get('signature_state') or item.get('trust', 'unknown')}"))
        actions = box(vertical=False)
        for title, action, enabled in (("Install", "install", not installed), ("Disable" if installed and installed.get("enabled") else "Enable", "disable" if installed and installed.get("enabled") else "enable", bool(installed)), ("Uninstall", "uninstall", bool(installed))):
            widget = button(title, lambda name=action: self.perform("extension", f"/api/extensions/{quote(item_id, safe='')}/{name}"))
            widget.set_sensitive(enabled and "extension" not in self.busy)
            actions.append(widget)
        target.append(actions)
        if self.session_role == "operator":
            target.append(button("Permissions and settings…", lambda: self.extension_permissions(item_id)))
        target.append(label("Install mode: " + str(self.extension_data.get("install_mode", "unknown")), "nd-muted"))

    def extension_permissions(self, item_id):
        from .extension_dialog import show_policy
        show_policy(self, item_id)

    def render_marketplace(self, data):
        self.marketplace_data = data
        parent = self.page_boxes["marketplace"]
        clear(parent)
        parent.append(label("Integration marketplace", "nd-heading"))
        parent.append(label("Choose a dedicated integration, or keep using general desktop controls. Source downloads still need the game's or application's setup.", "nd-muted"))
        if self.session_role == "operator":
            from .operator_dialog import import_integration
            parent.append(button("Connect an existing integration…", lambda: import_integration(self)))
        search = Gtk.SearchEntry(placeholder_text="Find a game or application")
        search.set_text(getattr(self, "marketplace_query", ""))
        parent.append(search)
        row = box(vertical=False, spacing=20)
        choices = box(spacing=6)
        choices.set_size_request(230, -1)
        details = box(spacing=14)
        details.set_hexpand(True)
        row.append(choices)
        row.append(details)
        parent.append(row)
        items = data.get("items") or []

        def select(item):
            self.selected_marketplace = item["id"]
            clear(details)
            details.append(label(item["name"], "nd-subheading"))
            details.append(label(item.get("description", "")))
            install, license_info = item.get("install") or {}, item.get("license") or {}
            details.append(label("Setup required · " + license_info.get("spdx", "License unverified"), "nd-muted"))
            for requirement in install.get("requirements") or []:
                details.append(label("• " + requirement))
            for note in install.get("notes") or []:
                details.append(label(note, "nd-muted"))
            setup_url = external_url(install.get("instructions_url")) or external_url(item.get("repository"))
            if setup_url:
                details.append(Gtk.LinkButton(uri=setup_url, label="Open setup instructions"))
            details.append(label("Reviewed source: " + item["source_commit"], "nd-muted"))
            if item.get("can_stage_source"):
                stage = button("Source downloaded" if item.get("source_staged") else "Download pinned source", lambda: self.perform("marketplace", f"/api/marketplace/{quote(item['id'], safe='')}/stage"))
                stage.set_sensitive(not item.get("source_staged") and "marketplace" not in self.busy)
                details.append(stage)
            else:
                details.append(label("Review the upstream license before downloading or redistributing this project.", "nd-muted"))
            if self.session_role == "operator":
                details.append(button("Permissions and settings…", lambda: self.extension_permissions(item["id"])))

        def filter_items(*_):
            clear(choices)
            query = search.get_text().strip().lower()
            self.marketplace_query = search.get_text()
            filtered = [item for item in items if query in (item["id"] + " " + item["name"] + " " + item.get("description", "") + " " + " ".join(item.get("game_ids") or [])).lower()]
            for item in filtered:
                choices.append(button(item["name"], lambda chosen=item: select(chosen), "nd-list-button"))
            if filtered:
                select(next((item for item in filtered if item["id"] == getattr(self, "selected_marketplace", "")), filtered[0]))
            else:
                clear(details)
                details.append(label("No matching integrations."))

        search.connect("search-changed", filter_items)
        filter_items()
        integrations = data.get("integrations") or []
        if integrations:
            parent.append(label("Your connected ecosystem", "nd-subheading"))
        for integration in integrations:
            manifest = integration.get("manifest") or {}
            item_id = integration["id"]
            card = box(spacing=8, margin=12)
            card.add_css_class("nd-card")
            card.append(label(f"{manifest.get('name', item_id)} · {'Connected' if integration.get('connected') else 'Offline'}", "nd-subheading"))
            card.append(label("Games: " + (", ".join(manifest.get("game_ids") or []) or "None")))
            if self.session_role == "operator":
                from .operator_dialog import json_action
                actions = Gtk.FlowBox(selection_mode=Gtk.SelectionMode.NONE, column_spacing=8, row_spacing=8, min_children_per_line=1, max_children_per_line=3)
                actions.append(button("Permissions…", lambda key=item_id: self.extension_permissions(key)))
                actions.append(button("Connection token…", lambda key=item_id: json_action(self, "Create scoped connection token", f"/api/integrations/{quote(key, safe='')}/token", {"kind": "integration"}, description="Only this integration can use the token. Creating a new one revokes its previous connection credential.")))
                actions.append(button("Viewer token…", lambda key=item_id: json_action(self, "Create viewer token", f"/api/integrations/{quote(key, safe='')}/token", {"kind": "viewer"}, description="This read-only token is for an OBS browser source at /viewer/#integration=ID&token=TOKEN. It cannot manage the desktop. Sharing remains off until you or Neuro explicitly enable visibility.")))
                actions.append(button("Viewer visibility…", lambda key=item_id: json_action(self, "Viewer visibility", f"/api/integrations/{quote(key, safe='')}/visibility", {"visible": False, "actor": "neuro", "minutes": 15}, description="Share only curated activity from the selected actor, for at most 60 minutes. The discord.viewer permission is required. Hidden, offline and expired feeds are empty.")))
                card.append(actions)
            parent.append(card)

    def render_games(self, data):
        self.game_data = data
        parent = self.page_boxes["games"]
        clear(parent)
        parent.append(label("Games", "nd-heading"))
        session = data.get("session")
        if session:
            row = box(vertical=False)
            title = label(f"Playing {session['profile_name']} · {session['control_mode']} · {session.get('actions_issued', 0)} actions", "nd-subheading")
            title.set_hexpand(True)
            row.append(title)
            row.append(button("End session", lambda: self.perform("game", "/api/games/session", "DELETE")))
            parent.append(row)
        row = box(vertical=False, spacing=20)
        choices = box(spacing=6)
        choices.set_size_request(230, -1)
        self.game_details = box(spacing=14)
        self.game_details.set_hexpand(True)
        self.profiles = {profile["id"]: profile for profile in data.get("profiles") or []}
        for profile_id, profile in self.profiles.items():
            choices.append(button(profile["name"], lambda key=profile_id: self.select_game(key), "nd-list-button"))
        row.append(choices)
        row.append(self.game_details)
        parent.append(row)
        if self.profiles:
            self.select_game(self.selected_game if self.selected_game in self.profiles else next(iter(self.profiles)))
        else:
            parent.append(label("No game profiles found."))
        observe = button("Show me what Neuro sees", self.observe)
        observe.set_sensitive(bool((self.status.get("executor") or {}).get("connected")) and "observe" not in self.busy)
        parent.append(observe)
        self.observation_label = label(getattr(self, "observation", ""))
        self.observation_label.set_selectable(True)
        parent.append(self.observation_label)

    def select_game(self, profile_id):
        self.selected_game = profile_id
        profile = self.profiles[profile_id]
        target = self.game_details
        clear(target)
        target.append(label(profile["name"], "nd-subheading"))
        target.append(label(profile.get("description", "")))
        target.append(label("Keybinds: " + ", ".join(f"{name}: {key}" for name, key in (profile.get("keys") or {}).items()), "nd-muted"))
        target.append(label("Control mode"))
        self.mode = Gtk.DropDown.new_from_strings(["Auto", "Neurcro", "External integration", "Hybrid"])
        target.append(self.mode)
        self.launch = Gtk.CheckButton(label="Launch the game (requires system permission)")
        self.launch.set_sensitive(bool(profile.get("launchable")))
        target.append(self.launch)
        start = button("Start session", self.start_game, "suggested-action")
        start.set_sensitive(not self.game_data.get("session") and "game" not in self.busy)
        target.append(start)
        target.append(label("Starting a session selects keybinds. Input is sent when Neuro requests a game action.", "nd-muted"))

    def start_game(self):
        self.perform("game", "/api/games/session", body={"profile_id": self.selected_game, "launch": self.launch.get_active(), "mode": ("auto", "nd", "external", "hybrid")[self.mode.get_selected()]})

    def observe(self):
        def observed(result):
            self.observation = result.get("observation", "")
            self.observation_label.set_text(self.observation)
        self.perform("observe", "/api/games/observe", body={"vision": True}, completed=observed)

    def render_permissions(self, data):
        parent = self.page_boxes["permissions"]
        if "policy" in data and not self.policy_dirty:
            self.policy = normalise_policy(data["policy"])
        if self.policy is not None and not hasattr(self, "policy_editor"):
            clear(parent)
            parent.append(label("Permissions", "nd-heading"))
            parent.append(label("Changes apply to the running server only after you save.", "nd-muted"))
            self.policy_editor = box(spacing=10)
            parent.append(self.policy_editor)
            self.build_policy_editor()
            parent.append(label("Permission requests", "nd-subheading"))
            self.request_box = box()
            parent.append(self.request_box)
        if hasattr(self, "request_box"):
            self.render_requests(data.get("requests") or {})

    def build_policy_editor(self):
        clear(self.policy_editor)
        self.scope_widgets = {}
        self.default_allow = Gtk.CheckButton(label="Allow unspecified actions", active=bool(self.policy.get("default_allow")))
        self.default_allow.connect("toggled", self.mark_dirty)
        self.policy_editor.append(self.default_allow)
        grid = Gtk.Grid(column_spacing=18, row_spacing=10)
        for column, title in enumerate(("Scope", "Allowed", "Requestable", "Actions / minute (0 = unlimited)")):
            grid.attach(label(title, "nd-muted"), column, 0, 1, 1)
        for row, scope in enumerate(SCOPES, 1):
            config = self.policy["scopes"][scope]
            allowed = Gtk.CheckButton(active=config["allowed"])
            requestable = Gtk.CheckButton(active=config["requestable"])
            rate = Gtk.SpinButton.new_with_range(0, 100000, 1)
            rate.set_value(config["limits"].get("max_actions_per_minute", 0))
            for widget, signal in ((allowed, "toggled"), (requestable, "toggled"), (rate, "value-changed")):
                widget.connect(signal, self.mark_dirty)
            for column, widget in enumerate((label(scope.title()), allowed, requestable, rate)):
                grid.attach(widget, column, row, 1, 1)
            self.scope_widgets[scope] = allowed, requestable, rate
        self.policy_editor.append(grid)
        advanced = Gtk.Expander(label="Explicit allow and deny lists")
        lists = box()
        self.action_views = {}
        for key, title in (("allowed_actions", "Allowed actions"), ("denied_actions", "Denied actions (take priority)")):
            lists.append(label(title))
            scroll, view = text_view("\n".join(self.policy.get(key) or []), editable=True, height=80)
            view.get_buffer().connect("changed", self.mark_dirty)
            lists.append(scroll)
            self.action_views[key] = view
        advanced.set_child(lists)
        self.policy_editor.append(advanced)
        self.policy_editor.append(label("Re-enter your operator token to save permission changes.", "nd-muted"))
        self.policy_operator_token = credential_entry()
        self.policy_editor.append(self.policy_operator_token)
        row = box(vertical=False)
        self.save_policy_button = button("Save policy", self.save_policy, "suggested-action")
        self.save_policy_button.set_sensitive(False)
        row.append(self.save_policy_button)
        row.append(button("Reload policy", self.reload_policy))
        self.dirty_label = label("", "nd-muted")
        row.append(self.dirty_label)
        self.policy_editor.append(row)

    def mark_dirty(self, *_):
        self.policy_dirty = True
        if hasattr(self, "save_policy_button"):
            self.save_policy_button.set_sensitive("policy" not in self.busy)
            self.dirty_label.set_text("Unsaved changes")

    def save_policy(self):
        secret = self.policy_operator_token.get_text().strip()
        if not secret:
            self.notice("Enter your operator token before saving permission changes.")
            self.policy_operator_token.grab_focus()
            return
        policy = copy.deepcopy(self.policy)
        policy["default_allow"] = self.default_allow.get_active()
        for name, (allowed, requestable, rate) in self.scope_widgets.items():
            config = policy["scopes"][name]
            config.update(allowed=allowed.get_active(), requestable=requestable.get_active())
            config["limits"]["max_actions_per_minute"] = rate.get_value_as_int()
        for key, view in self.action_views.items():
            buffer = view.get_buffer()
            policy[key] = list(dict.fromkeys(re.split(r"[\s,]+", buffer.get_text(*buffer.get_bounds(), False).strip())))
            policy[key] = [name for name in policy[key] if name]
        overlap = set(policy["allowed_actions"]) & set(policy["denied_actions"])
        if overlap:
            self.notice("Remove actions listed as both allowed and denied: " + ", ".join(sorted(overlap)))
            return

        def saved(_result):
            self.policy = policy
            self.policy_dirty = False
            self.save_policy_button.set_sensitive(False)
            self.dirty_label.set_text("Saved and applied live")
        self.policy_operator_token.set_text("")
        self.perform("policy", "/api/permissions", "PUT", policy, self.policy_editor, saved, operator_token=secret)

    def reload_policy(self):
        self.policy_dirty = False
        self.policy = None
        if hasattr(self, "policy_editor"):
            del self.policy_editor
        self.rendered.pop("permissions", None)
        self.refresh(force=True)

    def render_requests(self, data):
        clear(self.request_box)
        pending, grants = data.get("pending") or [], data.get("grants") or []
        if not pending and not grants:
            self.request_box.append(label("No pending requests or active grants.", "nd-muted"))
        for request in pending:
            row = box(vertical=False)
            description = label(f"{request['scope']}: {request.get('reason', '')}")
            description.set_hexpand(True)
            row.append(description)
            minutes = Gtk.SpinButton.new_with_range(0, 1440, 5)
            minutes.set_value(request.get("minutes") or 15)
            minutes.set_tooltip_text("Grant duration in minutes; 0 lasts until revoked")
            row.append(minutes)
            path = "/api/permission-requests/" + quote(request["id"], safe="")
            row.append(button("Approve", lambda url=path, duration=minutes: self.operator_change("Approve permission request", url + "/approve", {"minutes": duration.get_value_as_int()})))
            row.append(button("Deny", lambda url=path: self.operator_change("Deny permission request", url + "/deny")))
            self.request_box.append(row)
        for grant in grants:
            row = box(vertical=False)
            description = label(f"{grant['scope']} · expires {grant.get('expires_at') or 'when revoked'}")
            description.set_hexpand(True)
            row.append(description)
            row.append(button("Revoke", lambda scope=grant["scope"]: self.operator_change("Revoke permission grant", f"/api/permissions/grants/{quote(scope, safe='')}/revoke")))
            self.request_box.append(row)

    def render_activity(self, data):
        self.audit_data = data
        parent = self.page_boxes["activity"]
        if not hasattr(self, "audit_list"):
            clear(parent)
            parent.append(label("Activity", "nd-heading"))
            self.audit_filter = Gtk.SearchEntry(placeholder_text="Filter actions, results, or reasons")
            self.audit_filter.connect("search-changed", lambda _: self.filter_activity())
            parent.append(self.audit_filter)
            self.audit_list = box(spacing=10)
            parent.append(self.audit_list)
        self.filter_activity()

    def filter_activity(self):
        clear(self.audit_list)
        if not self.audit_data.get("enabled"):
            self.audit_list.append(label(self.audit_data.get("message", "Set NEURO_AUDIT_LOG on the server to record activity.")))
            return
        query = self.audit_filter.get_text().casefold()
        count = 0
        for entry in reversed(self.audit_data.get("entries") or []):
            result = entry.get("decision") or ("Paused" if entry.get("paused") is True else "Resumed" if entry.get("paused") is False else "")
            details = entry.get("reason") or entry.get("source") or ""
            text = f"{entry.get('action') or entry.get('event', '')} · {result}\n{details}"
            if query not in text.casefold():
                continue
            try:
                stamp = datetime.fromisoformat(entry["time"].replace("Z", "+00:00")).astimezone().strftime("%x %X")
            except (KeyError, ValueError):
                stamp = ""
            item = box(spacing=5)
            item.add_css_class("nd-card")
            item.append(label(stamp, "nd-muted"))
            item.append(label(text))
            self.audit_list.append(item)
            count += 1
        if not count:
            self.audit_list.append(label("No matching activity." if query else "No activity recorded yet.", "nd-muted"))

    def render_status(self, data):
        if data:
            self.config = data
        parent = self.page_boxes["status"]
        clear(parent)
        parent.append(label("Status", "nd-heading"))
        if self.session_role == "operator":
            from .operator_dialog import json_action
            row = box(vertical=False)
            row.append(button("Create Neuro/Evil manager sign-in…", lambda: json_action(self, "Create delegated manager sign-in", "/api/delegation/token", {"minutes": 60, "persona": "neuro"}, description="Use this limited token in the manager on Neuro's desktop. It cannot approve permissions or resume an operator pause. Sign out of the operator session on that desktop first. Change persona to evil for Evil.")))
            row.append(button("Revoke delegated sign-in…", lambda: json_action(self, "Revoke delegated manager access", "/api/delegation/token", {}, method="DELETE", description="Immediately invalidate the current delegated manager token.")))
            parent.append(row)
        status = self.status
        neuro, executor, relay = (status.get(name) or {} for name in ("neuro", "executor", "relay"))
        for title, text in (
            ("Server", f"{self.server}\nVersion {status.get('version', 'unknown')} · uptime {int(status.get('uptime', 0)) // 60} minutes"),
            ("Neuro API", "Connected" if neuro.get("connected") else neuro.get("last_error") or "Waiting for Neuro"),
            ("Client", f"{'Connected' if executor.get('connected') else 'Offline'} · protocol {executor.get('version', 'unknown')}\n{executor.get('completed_commands', 0)} commands · {executor.get('failed_commands', 0)} failed"),
            ("Relay", f"{'Connected' if relay.get('connected') else 'Disabled' if not relay.get('enabled') else 'Waiting'} · {relay.get('peer_count', 0)} peers"),
        ):
            card = box(spacing=6)
            card.add_css_class("nd-card")
            card.append(label(title, "nd-subheading"))
            card.append(label(text))
            parent.append(card)
        paths = (self.config or {}).get("paths") or {}
        if paths:
            advanced = Gtk.Expander(label="Configuration paths")
            advanced.set_child(label("\n".join(f"{key}: {value}" for key, value in paths.items())))
            parent.append(advanced)

    def on_close(self, *_):
        self.closed = True
        self.generation += 1
        self.cancel_timer()
        self.close_session_dialogs()
        if self.client:
            self.client.token = ""
        self.pool.shutdown(wait=False, cancel_futures=True)
        return False

    def do_shutdown(self):
        self.on_close()
        Gtk.Application.do_shutdown(self)
