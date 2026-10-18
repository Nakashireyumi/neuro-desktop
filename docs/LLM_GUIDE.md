# Driving Neuro Desktop with a small or "dumb" model

Neuro Desktop works with strong models out of the box. Most of the trouble with
desktop control comes from weak models: wrong parameter names, guessed action
names, "I moved the mouse" without moving it, or repeating a call that already
failed.

Everything here is written so a small model can still succeed. Nothing depends on
the model reading JSON Schema, using tools reliably, or planning ahead.

## 1. What the surface gives a weak model

| Signal | Where it comes from | Why it helps |
| ------ | ------------------- | ------------ |
| One-sentence descriptions, each ending with an example | every action's `description` | the model can pick by name and description alone |
| Parameter list in plain text | `/api/actions` → `params`, for example `direction (string, required: forward\|back\|left\|right\|up\|down)` | no JSON Schema knowledge needed |
| Required parameters | the `required` array and the `(required: …)` marker | a missing parameter is the most common failure |
| Corrective refusals | `action-registry.go` and `guide.go` | a refusal names the action and the parameter, and gives a valid example. Section 5 lists them |
| Ground truth about the desktop | `get_status`, `game_observe`, `game_list` | the model does not have to guess what is on screen |
| A briefing on demand | `desktop_guide` (always allowed) | the model can ask how the desktop works, at any time |
| A way out | `reset_controls` (always allowed) | a stuck input can be cleared without guessing |

The registration sent to Neuro carries the same information, so the model sees it
*before* it acts.

## 2. Shortest useful system prompt

```
You control a desktop through Neuro Desktop.

Rules:
1. Call get_status first. It tells you the platform and whether the executor is connected.
2. Only use actions from the list you were given, with the exact parameter names shown
   after them. Never invent an action or a parameter.
3. If an action is refused, read the reply. It names the parameter it wants and gives an
   example. Fix the call once and resend it. If it fails again, call desktop_guide.
4. If something is stuck (a key keeps running, input does not respond), call
   reset_controls. It takes no parameters.
5. If you need a permission that is not on, call request_permission with a short reason.
   Wait for the answer before you retry.
6. Nothing you say moves the mouse. Only the actions do.
7. One action at a time. Look at the result, then decide the next one.
```

Rules 6 and 7 matter most. Small models often announce an action without calling it, or
call five actions in one turn and lose track of which one failed.

## 3. Parameter shapes worth prompting explicitly

| Action | Call it like | Common mistake, and what the reply says |
| ------ | ------------ | --------------------------------------- |
| `move_mouse_to` | `{"x": 640, "y": 360}` | `"x": "640"` (a string). The reply asks for numeric x and y in screen pixels |
| `mouse_click` | `{"button": "left"}` or `{}` | nothing: `button` defaults to left |
| `type_text` | `{"text": "hello"}` | `{"message": …}` or an empty `text`. The reply names `"text"` |
| `key_press` | `{"key": "enter"}` | `{"keys": […]}`. The reply names `"key"` |
| `run_script` | `{"script": "…"}` | an empty script. The reply names `"script"` |
| `shell_command` | `{"command": "ls -la"}` | expecting a free shell. It runs only when the shell scope is on and the program is on the allowlist |
| `reset_controls` | `{}` | extra fields are ignored. Use it when something is stuck |
| `desktop_guide` | `{"topic": "shell"}` or `{}` | an unknown topic gets the list of valid topics. No topic means the whole guide |
| `request_permission` | `{"scope": "extensions", "reason": "install a memory tool", "minutes": 15}` | asking for a scope that is not requestable is refused |
| `game_move` | `{"direction": "forward", "seconds": 1}` | `"dir"`. Directions are `forward`, `back`, `left`, `right`, `up`, `down` |
| `game_look` | `{"dx": 120, "dy": 0}` | absolute coordinates. These are relative offsets |
| `game_action` | `{"action": "jump"}` | an invented name. Call `game_list` for the real ones |

`/api/actions` is the authority for all of these at runtime, including any change made
on the dashboard.

## 4. Tactics that reliably rescue a weak model

1. **Ground first.** `get_status`, then `game_observe` or `game_list`, before any input
   action. A model that has been told "the active window is Minecraft" stops guessing.
2. **One action per turn while debugging.** The action result window is short (about
   20 seconds, per the Neuro API), and a batch hides which call failed.
3. **Let the refusal do the teaching.** Do not paraphrase the failure back to the model.
   The reply already contains the corrected shape. Resend the call with that shape.
4. **Escalate, do not improvise.** If two calls fail, the model calls `desktop_guide`
   (or `game_list`, for a game). It does not try raw keys.
5. **Use named actions over raw keys.** `game_action {"action": "jump"}` survives a
   keybind change in the profile. `key_press {"key": "space"}` does not.
6. **Get unstuck with one call.** `reset_controls` releases held keys and buttons and
   clears queued input. It works while the bridge is paused, and under a deny-all
   policy.
7. **Ask instead of retrying.** A refused scope that is requestable can be asked for
   with `request_permission`. Retrying a refused action does not change the answer.
8. **Bound the run.** `NEURO_DENY_ACTIONS`, the pause switch, and the kill switch exist
   so a stuck model cannot cause damage while you fix the prompt.

## 5. Refusals a small model will see

These are the replies the eval pins (`testdata/weak_model_cases.json`), so a change
that weakens one fails CI.

| Call | Reply (abridged) |
| ---- | ---------------- |
| `move_mouse_to` with `{"x": "640", "y": "360"}` | `move_mouse_to needs numeric x and y in screen pixels, for example {"x": 640, "y": 360}` |
| `type_text` with `{}` | a reply that names `"text"` |
| `type_text` with `{"text": 5}` | a reply that asks for a non-empty `"text"` string |
| `"hello"` (a bare string) | a reply that says the arguments must be a JSON object |
| `shell_command` while the shell scope is off | a reply that names the shell scope and says how Vedal can allow it |
| `reset_controls` (any fields) | `Controls reset: …` when it works, or `Controls reset only partly worked. …` with what may still be held. Both end with a `Next:` hint |
| `request_permission` for a scope the operator has not made requestable | the operator has not allowed requests for that scope, so use another action |
| `request_permission` with no reason, or minutes out of range | the reply says what to add or what range to use |
| `request_permission` again while the first is pending | the request is still waiting for the operator. Do not send it again |

Every refusal names the action, so the model knows which call failed. The eval also checks
this for each refusal case.

## 6. Checking the surface yourself

```bash
# What the model will be offered, including parameter hints:
curl -s http://127.0.0.1:8300/api/actions | python3 -m json.tool | head -40

# The briefing a model gets for a game (needs the dashboard token, on every address):
curl -s -H "X-ND-Token: $TOKEN" http://127.0.0.1:8300/api/games | python3 -m json.tool
```

`TestActionsExposeLLMFriendlyMetadata` (Go) enforces the invariants: every action has a
real description, every parameterised action exposes a schema, and the plain-text `params`
rendering names every parameter, its type, and whether it is required. The weak-model eval
(`go test -run WeakModel`) checks the refusals in section 5. CI runs both.
