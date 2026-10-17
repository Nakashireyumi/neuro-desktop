# Let Neuro use the manager

Use a delegated session when Neuro or Evil controls the manager through its UI.
It has its own short-lived credential and cannot change permissions, approve
requests, edit extension settings, resume your pause, mint tokens or read the
audit log. Your operator token must stay outside the desktop session she controls.

In the native manager:

1. Sign in with your operator token and approve the global scopes and per-item
   management/runtime permissions you intend to delegate.
2. Open **Status → Create Neuro/Evil manager sign-in…**. The JSON editor defaults
   to `{"minutes":60,"persona":"neuro"}`; change the persona to `evil` if needed.
   Enter your operator token in the approval field and choose **Approve and apply**.
3. Copy the returned `token` once. Closing the dialog clears its output. Sign out
   of the operator manager on the controlled desktop, then sign in there using
   the delegated token and the same server origin.
4. To end access early, use **Status → Revoke delegated sign-in…** in your
   operator session and re-enter your operator token. You can run that manager
   from another desktop or forward its server connection over SSH.

The delegated native manager hides **Permissions**, **Activity**, credential
creation and integration-registration controls. You can pause or release input
from it; resuming an operator pause still requires your operator session.

For a tool you operate directly, the equivalent API contract follows.

Sign in as the operator, then create a session with
`POST /api/delegation/token`, a fresh `X-ND-Operator-Token` entry and an optional
JSON body such as `{"minutes":60,"persona":"neuro"}`. Use `evil` for Evil's
display label. Persona labels do not grant different authority. The API returns
the delegated token once; sign into the manager with that token. The manager
must never automatically copy the remembered operator token into the reentry
header.

Tokens expire after 60 minutes by default, with a maximum of 24 hours. Creating
one replaces the previous session. To revoke it, send
`DELETE /api/delegation/token` from your operator session with fresh operator
reentry. Restarting the server also revokes it. Only a hash and expiry live in
server memory; no raw delegated token is written to disk or audit logs.

`GET /api/session` reports `role: "neuro"` and `expires_at` for delegated sign-in.
`GET /api/delegation/session` also reports the persona label and UI capability
flags. An expired, revoked or invalid token receives the same 401 response. A
valid delegated token trying an operator-only route receives 403.

The route allowlist permits status and configuration inspection, catalog and
marketplace browsing, extension management, game sessions and observation.
Existing global scopes, explicit action denies, per-extension management grants,
pause/kill switches and shared action rate limits still apply. Launching a game
requires its separate system permission. Input release and pausing are always
available; resuming remains your decision. Unknown routes are denied by default.

Grant each extension's management and runtime abilities in your operator session
first. A delegated session cannot approve its own access. The token is accepted
only in `X-ND-Token` or the normal Bearer header, never in URL parameters. When
`X-ND-Token` is present it remains authoritative; a second authorization header
cannot elevate a recognized delegated session.

Delegation restricts HTTP authority. It cannot protect an operator window or a
secret left visible on a desktop that Neuro can otherwise control. Sign out of
operator windows before handing over that desktop. Revocation blocks subsequent
requests; it does not undo a completed action or cancel a request already admitted.
