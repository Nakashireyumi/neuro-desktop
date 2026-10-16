"""Small operator-only configuration dialogs with explicit fresh approval."""

import json
from urllib.parse import quote


def json_action(app, title, path, body, *, method="POST", description=""):
    from .app import box, label, button, text_view, Gtk

    if not app.client or app.session_role != "operator":
        return
    client = app.client
    dialog = Gtk.Window(title=title, transient_for=app.window, modal=True, default_width=620, default_height=630)
    content = box(spacing=14, margin=22)
    content.append(label(title, "nd-heading"))
    content.append(label(description, "nd-muted"))
    scroll, editor = text_view(json.dumps(body, indent=2), editable=True, height=180)
    content.append(scroll)
    content.append(label("Re-enter your operator token to approve this change.", "nd-muted"))
    token = Gtk.PasswordEntry(show_peek_icon=True)
    content.append(token)
    message = label()
    content.append(message)
    output_scroll, output = text_view(height=130)
    content.append(output_scroll)
    closed = False

    def close(*_):
        nonlocal closed
        closed = True
        token.set_text("")
        output.get_buffer().set_text("")
        return False

    def apply():
        secret = token.get_text().strip()
        if not secret:
            message.set_text("Enter your operator token to continue.")
            return
        try:
            buffer = editor.get_buffer()
            data = json.loads(buffer.get_text(buffer.get_start_iter(), buffer.get_end_iter(), True))
            if not isinstance(data, dict):
                raise ValueError("Enter a JSON object.")
            endpoint = path(data) if callable(path) else path
        except (ValueError, KeyError, TypeError) as error:
            message.set_text(str(error))
            return
        token.set_text("")
        apply_button.set_sensitive(False)
        message.set_text("Applying…")

        def finished(result, error):
            if closed:
                return
            apply_button.set_sensitive(True)
            message.set_text(str(error) if error else "Applied. Copy the credential below before closing." if result.get("token") else "Applied.")
            if not error:
                output.get_buffer().set_text(json.dumps(result, indent=2))
                app.rendered.clear()
                app.refresh(force=True)

        app.submit(lambda: client.request(endpoint, method, data, operator_token=secret), finished)

    apply_button = button("Approve and apply", apply, "suggested-action")
    content.append(apply_button)
    content.append(button("Close", dialog.close))
    dialog.connect("close-request", close)
    dialog.set_child(Gtk.ScrolledWindow(child=content, hscrollbar_policy=Gtk.PolicyType.NEVER))
    dialog.present()


def import_integration(app):
    json_action(app, "Connect an existing integration", lambda data: "/api/integrations/" + quote(data["manifest"]["id"], safe=""), {"manifest": {"schema_version": 1, "id": "my-minecraft", "name": "Minecraft", "version": "1.0.0", "kind": "game", "game_ids": ["minecraft"], "permissions": [], "settings_schema": {"type": "object", "additionalProperties": False}}}, method="PUT", description="Paste your SDK integration manifest. This registers its identity and game association; it does not launch or replace your existing integration. Approve its permissions next, then create its scoped connection token.")
