import { useEffect, useId, useState, type FormEvent } from "react";
import { api, type ExtensionPermissionPolicy, type ExtensionPolicyPayload, type SettingsSchema } from "./api";

export default function ExtensionPolicyEditor({ id, onClose, onSaved }: {
  id: string;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const formId = useId();
  const [payload, setPayload] = useState<ExtensionPolicyPayload | null>(null);
  const [policy, setPolicy] = useState<ExtensionPermissionPolicy | null>(null);
  const [settingsText, setSettingsText] = useState("{}");
  const [operatorToken, setOperatorToken] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    setPayload(null);
    setPolicy(null);
    setOperatorToken("");
    setError("");
    setNotice("");
    void api.extensionPolicy(id, controller.signal).then((next) => {
      if (controller.signal.aborted) return;
      setPayload(next);
      setPolicy(next.policy);
      setSettingsText(JSON.stringify(next.policy.settings ?? {}, null, 2));
    }).catch((failure) => {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : "Could not read this extension's permissions.");
    });
    return () => controller.abort();
  }, [id]);

  let settings: Record<string, unknown> = {};
  let settingsError = "";
  try {
    const parsed: unknown = JSON.parse(settingsText);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Settings must be a JSON object.");
    settings = parsed as Record<string, unknown>;
  } catch {
    settingsError = "Settings must be a valid JSON object.";
  }

  function updateSetting(key: string, value: unknown) {
    const next = { ...settings };
    if (value === undefined) delete next[key];
    else next[key] = value;
    setSettingsText(JSON.stringify(next, null, 2));
  }

  function settingField(key: string, schema: SettingsSchema) {
    const value = settings[key];
    const fieldId = `${formId}-${key}`;
    const required = payload?.settings_schema?.required?.includes(key);
    const structured = schema.type === "object" || schema.type === "array";
    return (
      <div key={key} style={{ marginTop: "0.8rem" }}>
        <label htmlFor={fieldId}><strong>{schema.title || key}{required ? " (required)" : ""}</strong></label>
        {schema.description && <p className="hint">{schema.description}</p>}
        {schema.enum ? (
          <select id={fieldId} value={value === undefined ? "" : JSON.stringify(value)} onChange={(event) => updateSetting(key, event.target.value === "" ? undefined : JSON.parse(event.target.value))}>
            <option value="">Use default</option>
            {schema.enum.map((choice) => <option key={JSON.stringify(choice)} value={JSON.stringify(choice)}>{String(choice)}</option>)}
          </select>
        ) : schema.type === "boolean" ? (
          <input id={fieldId} type="checkbox" checked={Boolean(value ?? schema.default ?? false)} onChange={(event) => updateSetting(key, event.target.checked)} />
        ) : structured ? (
          <p className="hint">Edit this structured setting in Settings JSON below.</p>
        ) : (
          <input id={fieldId} style={{ display: "block", width: "100%", boxSizing: "border-box" }}
            type={schema.secret || schema.writeOnly ? "password" : schema.type === "number" || schema.type === "integer" ? "number" : "text"}
            step={schema.type === "integer" ? 1 : "any"} min={schema.minimum} max={schema.maximum}
            minLength={schema.minLength} maxLength={schema.maxLength}
            value={value === undefined ? "" : String(value)} autoComplete="off"
            placeholder={schema.default === undefined ? "Not set" : `Default: ${String(schema.default)}`}
            onChange={(event) => {
              const raw = event.target.value;
              updateSetting(key, raw === "" ? undefined : schema.type === "number" || schema.type === "integer" ? Number(raw) : raw);
            }} />
        )}
        {value !== undefined && <button type="button" className="linkish" onClick={() => updateSetting(key, undefined)}>Use default</button>}
      </div>
    );
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!policy || busy || settingsError || !operatorToken.trim()) return;
    const freshToken = operatorToken;
    setOperatorToken("");
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const next = await api.saveExtensionPolicy(id, { ...policy, settings }, freshToken);
      setPayload(next);
      setPolicy(next.policy);
      setSettingsText(JSON.stringify(next.policy.settings ?? {}, null, 2));
      setNotice("Your extension permissions and settings are saved.");
      onSaved?.();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not save extension permissions.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby={`${formId}-title`} style={{ marginTop: "1.5rem", borderTop: "1px solid var(--line)", paddingTop: "1rem" }}>
      <div className="panel-header">
        <div><h2 id={`${formId}-title`}>Permissions and settings</h2><p><code>{id}</code> · Approve only the abilities you want to provide.</p></div>
        <button className="secondary" type="button" onClick={() => { setOperatorToken(""); onClose(); }}>Close</button>
      </div>
      {error && <p className="scope-warning" role="alert">{error}</p>}
      {notice && <p className="save-notice" role="status">{notice}</p>}
      {!policy ? <p className="hint">{error ? "Close and reopen to retry." : "Reading this extension's policy…"}</p> : (
        <form onSubmit={(event) => void save(event)}>
          <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0 }}>
            <div className="button-row">
              <label><input type="checkbox" checked={policy.manage_allowed} onChange={(event) => setPolicy({ ...policy, manage_allowed: event.target.checked })} /> Allow Neuro and Evil to manage this item</label>
              <label><input type="checkbox" checked={policy.run_allowed} onChange={(event) => setPolicy({ ...policy, run_allowed: event.target.checked })} /> Allow this extension to run</label>
            </div>
            <p className="hint">Management covers source downloads, installation, enabling and removal. Runtime access stays separate and also needs the global Extensions permission.</p>
            {payload?.legacy && <p className="hint">This policy reflects a previously enabled extension. Saving makes your choices explicit.</p>}
            <h3>Declared capabilities</h3>
            {(payload?.permissions ?? []).map((permission) => (
              <div key={permission.id} style={{ marginBottom: "0.8rem" }}>
                <label><input type="checkbox" checked={Boolean(policy.grants[permission.id])}
                  onChange={(event) => setPolicy({ ...policy, grants: { ...policy.grants, [permission.id]: event.target.checked } })} /> <strong>{permission.id}</strong>{permission.required ? " · required to run" : " · optional"}</label>
                <p className="hint">{permission.description}</p>
              </div>
            ))}
            {!payload?.permissions?.length && <p className="hint">No individual capabilities are declared. Managed grants do not sandbox native code or separately installed integrations.</p>}
            <h3>Settings</h3>
            {!settingsError && Object.entries(payload?.settings_schema?.properties ?? {}).map(([key, schema]) => settingField(key, schema))}
            {!Object.keys(payload?.settings_schema?.properties ?? {}).length && <p className="hint">No named settings are declared.</p>}
            <button type="button" className="linkish" onClick={() => setAdvanced(!advanced)}>{advanced ? "Hide Settings JSON" : "Edit Settings JSON"}</button>
            {advanced && <label style={{ display: "block" }}>Settings JSON
              <textarea rows={8} spellCheck={false} style={{ display: "block", width: "100%", boxSizing: "border-box" }} value={settingsText} onChange={(event) => setSettingsText(event.target.value)} />
            </label>}
            {settingsError && <p className="scope-warning" role="alert">{settingsError}</p>}
            <p className="hint">Your token is required for each approval. This field clears as soon as you submit.</p>
            <label htmlFor={`${formId}-operator`}>Re-enter your operator token</label>
            <input id={`${formId}-operator`} type="password" autoComplete="new-password" value={operatorToken}
              style={{ display: "block", width: "100%", boxSizing: "border-box" }}
              onChange={(event) => setOperatorToken(event.target.value)} />
            <div className="button-row"><button className="primary" disabled={busy || Boolean(settingsError) || !operatorToken.trim()}>{busy ? "Saving…" : "Approve and save"}</button></div>
          </fieldset>
        </form>
      )}
    </section>
  );
}
