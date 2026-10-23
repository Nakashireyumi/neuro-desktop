import { useState } from "react";
import { api, type ControlState } from "./api";

export default function OperatorControls({ control, connected, onChanged }: {
  control: ControlState | null;
  connected: boolean;
  onChanged: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [releasing, setReleasing] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const stopped = control?.paused || control?.kill_switch_active;

  async function togglePause() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await (control?.paused ? api.resume() : api.pause());
      setNotice(result.message);
      await onChanged();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not change the pause state.");
    } finally {
      setBusy(false);
    }
  }

  async function release() {
    setReleasing(true);
    setError("");
    setNotice("");
    try {
      const result = await api.releaseInput();
      setNotice(result.message);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not release input.");
    } finally {
      setReleasing(false);
    }
  }

  return (
    <section className={`operator-controls ${stopped ? "operator-controls--stopped" : ""}`} aria-label="Operator controls">
      <div>
        <strong>{!connected ? "Connection lost" : control?.kill_switch_active ? "Kill switch active" : control?.paused ? "Actions paused" : "Actions enabled"}</strong>
        <p className="hint">{control?.kill_switch_active
          ? `Remove the kill switch file to allow actions: ${control.kill_switch_file}`
          : "Pause blocks new actions. Release all input clears held keys and mouse buttons."}</p>
      </div>
      <div className="button-row">
        <button className={control?.paused ? "primary" : "danger"} onClick={() => void togglePause()}
          disabled={busy || !connected || !control || Boolean(control.kill_switch_active)}>
          {busy ? "Applying…" : control?.paused ? "Resume actions" : "Pause actions"}
        </button>
        <button className="secondary" onClick={() => void release()} disabled={releasing || !connected}>
          {releasing ? "Releasing…" : "Release all input"}
        </button>
      </div>
      {(error || notice) && <p className={error ? "runtime-error" : "operator-notice"} role={error ? "alert" : "status"}>{error || notice}</p>}
    </section>
  );
}
