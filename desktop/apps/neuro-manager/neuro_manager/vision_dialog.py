"""Built-in vision controls with an explicit, fresh operator approval."""


def service_card(app, state):
    from .app import box, label, button

    card = box(spacing=10)
    card.add_css_class("nd-card")
    card.append(label("Vision Service", "nd-subheading"))
    card.append(label("Default built-in service", "nd-muted"))
    card.append(label("Disabling stops image capture and analysis in Neuro Desktop. An external vision server keeps running."))
    if not isinstance(state, dict):
        card.append(label("Vision controls unavailable. Update the bridge to manage this service.", "nd-muted"))
        return card
    actors = state.get("actors") or {}
    card.append(label(f"Service: {'enabled' if state.get('enabled') else 'disabled'} · Neuro: {'allowed' if actors.get('neuro') else 'disabled'} · Evil: {'allowed' if actors.get('evil') else 'disabled'}"))
    card.append(label(f"Bridge persona: {state.get('persona') or 'unknown'} · Service available to this bridge: {'yes' if state.get('effective') else 'no'} · Analysis endpoint: {'configured' if state.get('configured') else 'not configured'}"))
    if state.get("reason"):
        card.append(label(state["reason"], "nd-muted"))
    card.append(label("Screen access still needs the global Vision permission. A shared bridge follows both persona switches.", "nd-muted"))
    if app.session_role == "operator":
        card.append(button("Configure Vision Service…", lambda: show_service(app)))
    else:
        card.append(label("Use your operator session to change these controls.", "nd-muted"))
    return card


def show_service(app):
    from .app import box, label, button, credential_entry, Gtk

    client = app.client
    if not client or app.session_role != "operator":
        return

    def loaded(result, error):
        if error:
            app.notice(str(error))
            return
        state = result.get("vision")
        if not isinstance(state, dict):
            app.notice("The bridge did not return Vision Service settings.")
            return
        dialog = Gtk.Window(title="Vision Service settings", transient_for=app.window, modal=True, default_width=540)
        content = box(spacing=14, margin=22)
        content.append(label("Vision Service", "nd-heading"))
        content.append(label("Disabling stops image capture and analysis in Neuro Desktop. An external vision server keeps running.", "nd-muted"))
        switches = {}
        actors = state.get("actors") or {}
        for key, title, active in (("enabled", "Enabled", state.get("enabled")), ("neuro", "Neuro", actors.get("neuro")), ("evil", "Evil", actors.get("evil"))):
            row = box(vertical=False)
            caption = label(title)
            caption.set_hexpand(True)
            row.append(caption)
            switch = Gtk.Switch(active=bool(active), valign=Gtk.Align.CENTER)
            switch.update_property([Gtk.AccessibleProperty.LABEL], [title])
            row.append(switch)
            content.append(row)
            switches[key] = switch
        content.append(label("A shared bridge requires both Neuro and Evil to be allowed. Screen access also needs the global Vision permission.", "nd-muted"))
        content.append(label("Re-enter your operator token to approve these changes. Your sign-in token is never filled in here.", "nd-muted"))
        token = credential_entry()
        content.append(token)
        message = label()
        content.append(message)
        closed = False

        def cleanup():
            nonlocal closed
            closed = True
            token.set_text("")

        def save():
            secret = token.get_text().strip()
            if not secret:
                message.set_text("Enter your operator token to save.")
                return
            body = {"enabled": switches["enabled"].get_active(), "actors": {key: switches[key].get_active() for key in ("neuro", "evil")}}
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
                    app.notice("Vision Service settings saved.")
                    app.rendered.pop("extensions", None)
                    app.refresh(force=True)

            app.submit(lambda: client.request("/api/services/vision", "PUT", body, operator_token=secret), saved)

        save_button = button("Approve and save", save, "suggested-action")
        content.append(save_button)
        content.append(button("Cancel", dialog.close))
        app.track_session_dialog(dialog, cleanup)
        dialog.set_child(content)
        dialog.present()

    app.submit(lambda: client.request("/api/services/vision"), loaded)
