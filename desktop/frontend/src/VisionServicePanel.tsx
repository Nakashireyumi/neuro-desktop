import { useEffect, useRef, useState } from "react";
import { api, type VisionServiceSettings, type VisionServiceState } from "./api";

export default function VisionServicePanel({ state, operator, onSaved }: {
  state: VisionServiceState | null;
  operator: boolean;
  onSaved: (state: VisionServiceState) => void;
}) {
  const [draft, setDraft] = useState<VisionServiceSettings | null>(state);
  const [dirty, setDirty] = useState(false);
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const pending = useRef<AbortController | null>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      pending.current?.abort();
    };
  }, []);

  useEffect(() => {
    // The parent's normal Extensions refresh must not erase unsaved switches
    // or replace a freshly entered approval token.
    if (!dirty) setDraft(state);
  }, [state, dirty]);

  function change(key: "enabled" | "neuro" | "evil", checked: boolean) {
    if (!draft || !operator) return;
    setDraft(key === "enabled" ? { ...draft, enabled: checked } : { ...draft, actors: { ...draft.actors, [key]: checked } });
    setDirty(true);
    setMessage("");
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!draft || !operator || saving) return;
    const approval = token;
    setToken("");
    setSaving(true);
    setMessage("");
    const controller = new AbortController();
    pending.current = controller;
    try {
      // Send only writable settings, never the status fields in the snapshot.
      const result = await api.saveVisionService({ enabled: draft.enabled, actors: { ...draft.actors } }, approval, controller.signal);
      if (!alive.current || controller.signal.aborted) return;
      setDirty(false);
      setMessage("Vision Service settings saved.");
      onSaved(result.vision);
    } catch (failure) {
      if (alive.current && !controller.signal.aborted) setMessage(failure instanceof Error ? failure.message : "Could not save Vision Service settings.");
    } finally {
      if (alive.current && !controller.signal.aborted) setSaving(false);
      if (pending.current === controller) pending.current = null;
    }
  }

  return <section className="vision-service-card" aria-labelledby="vision-service-title">
    <h2 id="vision-service-title">Vision Service</h2>
    <p className="hint">Default built-in service</p>
    <p>Disabling stops image capture and analysis in Neurcro. An external vision server keeps running.</p>
    {state && draft ? <>
      <p><strong>Bridge persona:</strong> {state.persona} · <strong>Service available to this bridge:</strong> {state.effective ? "yes" : "no"} · <strong>Analysis endpoint:</strong> {state.configured ? "configured" : "not configured"}</p>
      {state.reason && <p className="hint">{state.reason}</p>}
      <form onSubmit={(event) => void save(event)}>
        <fieldset disabled={!operator || saving} style={{ border: 0, padding: 0, margin: 0 }}>
          <legend className="sr-only">Vision Service permissions</legend>
          <div className="vision-service-switches">
            {(["enabled", "neuro", "evil"] as const).map((key) => <label key={key}>
              <input type="checkbox" role="switch" checked={key === "enabled" ? draft.enabled : draft.actors[key]}
                onChange={(event) => change(key, event.target.checked)} />
              {key === "enabled" ? "Enabled" : key === "neuro" ? "Neuro" : "Evil"}
            </label>)}
          </div>
          {operator && <>
            <label htmlFor="vision-operator-token">Re-enter your operator token to approve changes</label>
            <input id="vision-operator-token" type="password" autoComplete="new-password" value={token}
              onChange={(event) => setToken(event.target.value)} style={{ display: "block", width: "100%", maxWidth: "34rem", boxSizing: "border-box", margin: "0.5rem 0" }} />
            <button className="primary" type="submit" disabled={!dirty || !token.trim()}>{saving ? "Saving…" : "Approve and save"}</button>
          </>}
        </fieldset>
      </form>
      {!operator && <p className="hint">Use your operator session to change these controls.</p>}
      <p className="hint">Screen access still needs the global Vision permission. A shared bridge requires both Neuro and Evil to be allowed.</p>
      {message && <p role="status">{message}</p>}
    </> : <p className="hint">Vision controls unavailable. Update the bridge to manage this service.</p>}
  </section>;
}
