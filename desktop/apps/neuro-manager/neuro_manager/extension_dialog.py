"""Fresh operator approval for per-extension rights; never copies a session token."""

import json
from urllib.parse import quote


def show_policy(app, item_id):
    from .app import box, label, button, text_view, Gtk

    client = app.client
    if not client:
        return
    path = f"/api/extensions/{quote(item_id, safe='')}/permissions"

    def loaded(data, error):
        if error:
            app.notice(str(error))
            return
        dialog = Gtk.Window(title=f"Permissions · {item_id}", transient_for=app.window, modal=True, default_width=600, default_height=630)
        content = box(spacing=14, margin=22)
        content.append(label("Choose what this extension may do", "nd-heading"))
        content.append(label("These approvals also need the matching global permissions. External integrations keep their own operating-system access.", "nd-muted"))
        policy = data.get("policy") or {}
        manage = Gtk.CheckButton(label="Allow Neuro and Evil to install, enable, disable and remove this extension", active=bool(policy.get("manage_allowed")))
        run = Gtk.CheckButton(label="Allow this extension to run", active=bool(policy.get("run_allowed")))
        content.append(manage)
        content.append(run)
        grants = {}
        for declaration in data.get("permissions") or []:
            key = declaration["id"]
            widget = Gtk.CheckButton(label=f"{key}{' (required)' if declaration.get('required') else ' (optional)'} · {declaration.get('description', '')}", active=bool((policy.get("grants") or {}).get(key)))
            content.append(widget)
            grants[key] = widget
        content.append(label("Extension settings (JSON; use environment references for secrets)", "nd-subheading"))
        scroll, settings = text_view(json.dumps(policy.get("settings") or {}, indent=2), editable=True, height=100)
        content.append(scroll)
        content.append(label("Re-enter your operator token to approve these changes. Your sign-in token is never filled in here.", "nd-muted"))
        token = Gtk.PasswordEntry(show_peek_icon=True)
        content.append(token)
        message = label()
        content.append(message)
        closed = False

        def cleanup():
            nonlocal closed
            closed = True
            token.set_text("")
            settings.get_buffer().set_text("")

        def save():
            secret = token.get_text().strip()
            if not secret:
                message.set_text("Enter your operator token to save.")
                return
            try:
                buffer = settings.get_buffer()
                values = json.loads(buffer.get_text(buffer.get_start_iter(), buffer.get_end_iter(), True))
                if not isinstance(values, dict):
                    raise ValueError("Settings must be a JSON object.")
            except ValueError as failure:
                message.set_text(str(failure))
                return
            body = {"manage_allowed": manage.get_active(), "run_allowed": run.get_active(), "grants": {key: widget.get_active() for key, widget in grants.items()}, "settings": values}
            token.set_text("")
            save_button.set_sensitive(False)
            message.set_text("Saving…")

            def saved(_result, failure):
                if closed:
                    return
                save_button.set_sensitive(True)
                if failure:
                    message.set_text(str(failure))
                else:
                    dialog.close()
                    app.notice("Extension permissions saved.")
                    app.refresh(force=True)

            app.submit(lambda: client.request(path, "PUT", body, operator_token=secret), saved)

        save_button = button("Approve and save", save, "suggested-action")
        content.append(save_button)
        content.append(button("Cancel", dialog.close))
        app.track_session_dialog(dialog, cleanup)
        dialog.set_child(Gtk.ScrolledWindow(child=content, hscrollbar_policy=Gtk.PolicyType.NEVER))
        dialog.present()

    app.submit(lambda: client.request(path), loaded)
