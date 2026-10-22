import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  api,
  type ActionInfo,
  type PermissionPolicyFile,
  type ScopeName,
} from "./api";
import PermissionRequests from "./PermissionRequests";

type ScopeConfig = {
  allowed: boolean;
  requestable?: boolean;
  limits?: { max_actions_per_minute?: number };
};

const SCOPE_DESCRIPTIONS: Record<ScopeName, { title: string; description: string; icon: string }> = {
  shell: {
    title: "Shell Commands",
    description: "Run commands on this PC. Explicit consent: stays off unless you turn it on yourself.",
    icon: "💻",
  },
  extensions: {
    title: "Extensions",
    description: "Install catalog extensions and run MCP servers. Explicit consent: off by default.",
    icon: "🧩",
  },
  input: {
    title: "Input Control",
    description: "Mouse, keyboard, and desktop interaction actions",
    icon: "🖱️",
  },
  game: {
    title: "Game Interface",
    description:
      "Playing a game through Neurcro itself: moving, looking, pressing keys, and running the game session",
    icon: "🎮",
  },
  filesystem: {
    title: "Filesystem Access",
    description: "File read/write, extension installation, and configuration access",
    icon: "📁",
  },
  process: {
    title: "Process Management",
    description: "Application launching, process control, and system utilities",
    icon: "⚙️",
  },
  network: {
    title: "Network Access",
    description: "External connections, catalog queries, and API communication",
    icon: "🌐",
  },
  system: {
    title: "System Actions",
    description: "Shutdown, lock workstation, launching programs, and system-wide operations",
    icon: "🔒",
  },
  vision: {
    title: "Vision & Context",
    description: "Screenshot capture, context collection, and vision analysis",
    icon: "👁️",
  },
};

const ALL_SCOPES = Object.keys(SCOPE_DESCRIPTIONS) as ScopeName[];

function defaultPolicy(): PermissionPolicyFile {
  return {
    version: "1.0.0",
    default_allow: false,
    allowed_actions: [],
    denied_actions: [],
    scopes: Object.fromEntries(
      ALL_SCOPES.map((scope) => [
        scope,
        { allowed: !["filesystem", "system", "shell", "extensions"].includes(scope) },
      ]),
    ),
  };
}

/** Normalises whatever the bridge returned into the shape this editor edits. */
function normalisePolicy(raw: PermissionPolicyFile): PermissionPolicyFile {
  const scopes: PermissionPolicyFile["scopes"] = {};
  for (const scope of ALL_SCOPES) {
    const incoming = raw.scopes?.[scope];
    scopes[scope] = {
      allowed: Boolean(incoming?.allowed),
      requestable: Boolean(incoming?.requestable),
      limits: { max_actions_per_minute: incoming?.limits?.max_actions_per_minute ?? 0 },
    };
  }

  return {
    version: raw.version ?? "1.0.0",
    default_allow: Boolean(raw.default_allow),
    allowed_actions: [...(raw.allowed_actions ?? [])],
    denied_actions: [...(raw.denied_actions ?? [])],
    scopes,
  };
}

type SaveState =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "saved"; message: string }
  | { kind: "error"; message: string };

export default function PermissionsPage() {
  const [policy, setPolicy] = useState<PermissionPolicyFile>(() => defaultPolicy());
  const [source, setSource] = useState<string>("loading…");
  const [selectedScope, setSelectedScope] = useState<ScopeName>("input");
  const [showJsonPreview, setShowJsonPreview] = useState(false);
  const [isDirty, setIsDirty] = useState(false);
  const [saveState, setSaveState] = useState<SaveState>({ kind: "idle" });
  const [knownActions, setKnownActions] = useState<ActionInfo[]>([]);
  const [operatorToken, setOperatorToken] = useState("");

  const load = useCallback(async () => {
    setOperatorToken("");
    setSource("loading…");
    try {
      const [incoming, actions] = await Promise.all([
        api.permissions(),
        api.actions().catch(() => null),
      ]);

      setPolicy(normalisePolicy(incoming));
      setIsDirty(false);
      setSaveState({ kind: "idle" });
      setSource(
        "note" in incoming && incoming.note
          ? String((incoming as Record<string, unknown>).note)
          : "Loaded from the bridge policy file",
      );
      if (actions) {
        setKnownActions(actions.actions.filter((action) => !action.reserved));
      }
    } catch (error) {
      setPolicy(defaultPolicy());
      setSource(
        error instanceof ApiError
          ? `${error.message} — showing editor defaults`
          : "Could not read the policy from the bridge",
      );
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (!operatorToken.trim() || saveState.kind === "saving") return;
    const freshToken = operatorToken;
    setOperatorToken("");
    setSaveState({ kind: "saving" });
    try {
      const result = await api.savePermissions(policy, freshToken);
      setIsDirty(false);
      setSaveState({
        kind: "saved",
        message: result.applied_live
          ? "Saved and applied to the running bridge."
          : "Saved.",
      });
    } catch (error) {
      setSaveState({
        kind: "error",
        message:
          error instanceof ApiError
            ? error.status === 401
              ? `${error.message}. Sign in again to continue.`
              : error.message
            : "Could not save the policy",
      });
    }
  }

  function updatePolicy(next: Partial<PermissionPolicyFile>) {
    setPolicy((prev) => ({ ...prev, ...next }));
    setIsDirty(true);
  }

  function updateScope(scope: ScopeName, config: Partial<ScopeConfig>) {
    setPolicy((prev) => ({
      ...prev,
      scopes: {
        ...prev.scopes,
        [scope]: { ...prev.scopes[scope], ...config },
      },
    }));
    setIsDirty(true);
  }

  function addActionToList(listType: "allowed" | "denied", action: string) {
    const name = action.toLowerCase().trim();
    if (!name) return;

    setPolicy((prev) => {
      const other = listType === "allowed" ? "denied" : "allowed";
      const otherKey = other === "allowed" ? "denied_actions" : "allowed_actions";
      if (prev[listType === "allowed" ? "allowed_actions" : "denied_actions"].includes(name)) {
        return prev;
      }

      return {
        ...prev,
        // An action cannot be on both lists; the deny list wins in the bridge, so
        // adding an allow silently would look like it did nothing.
        [otherKey]: prev[otherKey].filter((item) => item !== name),
        [listType === "allowed" ? "allowed_actions" : "denied_actions"]: [
          ...prev[listType === "allowed" ? "allowed_actions" : "denied_actions"],
          name,
        ],
      };
    });
    setIsDirty(true);
  }

  function removeActionFromList(listType: "allowed" | "denied", index: number) {
    const key = listType === "allowed" ? "allowed_actions" : "denied_actions";
    setPolicy((prev) => ({ ...prev, [key]: prev[key].filter((_, i) => i !== index) }));
    setIsDirty(true);
  }

  function resetToDefaults() {
    if (confirm("Reset the editor to the default policy? Nothing is saved until you press Save.")) {
      setPolicy(defaultPolicy());
      setIsDirty(true);
    }
  }

  function exportPolicy() {
    const blob = new Blob([JSON.stringify(policy, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "permissions.json";
    a.click();
    URL.revokeObjectURL(url);
  }

  function importPolicy(file: File) {
    const reader = new FileReader();
    reader.onload = (event) => {
      try {
        const imported = JSON.parse(String(event.target?.result)) as PermissionPolicyFile;
        setPolicy(normalisePolicy(imported));
        setIsDirty(true);
        setSource(`Imported from ${file.name} — press Save to apply`);
      } catch {
        alert("Failed to parse policy file: invalid JSON");
      }
    };
    reader.readAsText(file);
  }

  const selectedConfig = policy.scopes[selectedScope];
  const scopeActions = useMemo(
    () => knownActions.filter((action) => action.scope === selectedScope),
    [knownActions, selectedScope],
  );
  const actionNames = useMemo(() => knownActions.map((action) => action.name), [knownActions]);

  return (
    <div className="permissions-page">
      <header className="permissions-header">
        <div>
          <h1>Permission Policy</h1>
          <p>{source}</p>
        </div>
        <div className="header-actions">
          <button className="secondary" onClick={() => void load()}>
            Reload
          </button>
          <button className="secondary" onClick={resetToDefaults}>
            Reset
          </button>
          <label className="secondary">
            Import
            <input
              type="file"
              accept=".json"
              onChange={(event) => event.target.files?.[0] && importPolicy(event.target.files[0])}
              style={{ display: "none" }}
            />
          </label>
          <button className="secondary" onClick={exportPolicy}>
            Export
          </button>
          <button className="secondary" onClick={() => setShowJsonPreview(!showJsonPreview)}>
            {showJsonPreview ? "Hide" : "Preview"} JSON
          </button>
          <label style={{ fontSize: "0.85rem" }}>Re-enter your operator token to save
            <input type="password" autoComplete="new-password" value={operatorToken} onChange={(event) => setOperatorToken(event.target.value)} disabled={saveState.kind === "saving"} style={{ display: "block" }} />
          </label>
          <button className="primary" onClick={() => void save()} disabled={saveState.kind === "saving" || !operatorToken.trim()}>
            {saveState.kind === "saving" ? "Saving…" : "Save"}
          </button>
        </div>
      </header>

      <PermissionRequests onChanged={() => { if (!isDirty) void load(); }} />

      {saveState.kind === "error" && <div className="scope-warning">⚠️ {saveState.message}</div>}
      {saveState.kind === "saved" && <div className="save-notice">✅ {saveState.message}</div>}

      <main className="permissions-main">
        <aside className="permissions-sidebar">
          <section>
            <h2>Permission Scopes</h2>
            {ALL_SCOPES.map((scope) => {
              const config = policy.scopes[scope];
              const desc = SCOPE_DESCRIPTIONS[scope];
              return (
                <button
                  key={scope}
                  className={`scope-item ${selectedScope === scope ? "selected" : ""}`}
                  onClick={() => setSelectedScope(scope)}
                >
                  <span className="scope-icon">{desc.icon}</span>
                  <div className="scope-info">
                    <strong>{desc.title}</strong>
                    <small className={config?.allowed ? "allowed" : "denied"}>
                      {config?.allowed ? "Allowed" : "Restricted"}
                    </small>
                  </div>
                </button>
              );
            })}
          </section>

          <section className="global-settings">
            <h2>Global Settings</h2>
            <label className="setting-row">
              <span>Default Permission</span>
              <select
                value={policy.default_allow ? "allow" : "deny"}
                onChange={(event) => updatePolicy({ default_allow: event.target.value === "allow" })}
              >
                <option value="deny">Deny by Default</option>
                <option value="allow">Allow by Default</option>
              </select>
            </label>
            <label className="setting-row">
              <span>Policy Version</span>
              <input
                type="text"
                value={String(policy.version ?? "")}
                onChange={(event) => updatePolicy({ version: event.target.value })}
                placeholder="1.0.0"
              />
            </label>
          </section>
        </aside>

        <section className="permissions-panel">
          <div className="panel-header">
            <div>
              <h2>{SCOPE_DESCRIPTIONS[selectedScope].title}</h2>
              <p>{SCOPE_DESCRIPTIONS[selectedScope].description}</p>
            </div>
            <label className="toggle">
              <input
                type="checkbox"
                checked={Boolean(selectedConfig?.allowed)}
                onChange={(event) => updateScope(selectedScope, { allowed: event.target.checked })}
              />
              <span className="toggle-slider" />
            </label>
          </div>

          <div className="requestable-row">
            <label className="toggle">
              <input
                type="checkbox"
                checked={Boolean(selectedConfig?.requestable)}
                onChange={(event) => updateScope(selectedScope, { requestable: event.target.checked })}
              />
              <span className="toggle-slider" />
            </label>
            <div>
              <strong>Neuro may request this</strong>
              <p>
                Neuro can ask you for this scope. Approvals are temporary and can be revoked at any time.
                {["shell", "system", "extensions"].includes(selectedScope)
                  ? " This scope needs explicit consent: it stays off unless you switch it on yourself."
                  : ""}
              </p>
            </div>
          </div>

          {!selectedConfig?.allowed && (
            <div className="scope-warning">
              ⚠️ This scope is restricted. Every action in this category is denied, regardless of the
              action lists below.
            </div>
          )}

          <div className="limits-section">
            <h3>Rate Limit</h3>
            <div className="limits-grid">
              <label className="limit-input">
                <span>Max Actions/Minute</span>
                <input
                  type="number"
                  min="0"
                  value={selectedConfig?.limits?.max_actions_per_minute || ""}
                  onChange={(event) =>
                    updateScope(selectedScope, {
                      limits: { max_actions_per_minute: parseInt(event.target.value, 10) || 0 },
                    })
                  }
                  placeholder="Unlimited"
                />
              </label>
            </div>
            <p className="hint">
              Actions above the limit are refused with a retry hint instead of piling up on the
              desktop. Leave empty for unlimited.
            </p>
          </div>

          {scopeActions.length > 0 && (
            <div className="limits-section">
              <h3>Actions in this scope</h3>
              <ul className="list-items">
                {scopeActions.map((action) => {
                  const state = action.allowed ? "Allowed" : "Denied";
                  return (
                    <li key={action.name}>
                      <code>{action.name}</code>
                      <span className={action.allowed ? "allowed" : "denied"}>{state}</span>
                    </li>
                  );
                })}
              </ul>
              <p className="hint">
                "Denied" here is what the bridge would answer right now, taking the action lists and
                the default permission into account.
              </p>
            </div>
          )}
        </section>

        <aside className="actions-sidebar">
          <section>
            <h2>Explicitly Allowed Actions</h2>
            <ActionListEditor
              actions={policy.allowed_actions}
              suggestions={actionNames}
              onAdd={(action) => addActionToList("allowed", action)}
              onRemove={(index) => removeActionFromList("allowed", index)}
              placeholder="e.g., run_script, move_mouse_to"
            />
          </section>

          <section>
            <h2>Explicitly Denied Actions</h2>
            <ActionListEditor
              actions={policy.denied_actions}
              suggestions={actionNames}
              onAdd={(action) => addActionToList("denied", action)}
              onRemove={(index) => removeActionFromList("denied", index)}
              placeholder="e.g., shutdown_immediately"
            />
          </section>

          <section className="policy-info">
            <h3>Policy Summary</h3>
            <div className="info-card">
              <p>
                <strong>Version:</strong> {String(policy.version ?? "")}
              </p>
              <p>
                <strong>Default:</strong> {policy.default_allow ? "Allow" : "Deny"}
              </p>
              <p>
                <strong>Allowed Actions:</strong> {policy.allowed_actions.length}
              </p>
              <p>
                <strong>Denied Actions:</strong> {policy.denied_actions.length}
              </p>
              <p>
                <strong>Bridge Actions Known:</strong> {knownActions.length}
              </p>
            </div>
            {isDirty && <p className="dirty-notice">⚠️ Unsaved changes</p>}
          </section>
        </aside>
      </main>

      {showJsonPreview && (
        <div className="json-preview">
          <header>
            <h3>Policy JSON (exactly what Save writes)</h3>
            <button onClick={() => setShowJsonPreview(false)}>Close</button>
          </header>
          <pre>{JSON.stringify(policy, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

function ActionListEditor({
  actions,
  suggestions,
  onAdd,
  onRemove,
  placeholder,
}: {
  actions: string[];
  suggestions: string[];
  onAdd: (action: string) => void;
  onRemove: (index: number) => void;
  placeholder: string;
}) {
  const [value, setValue] = useState("");
  const listId = useMemo(() => `nd-actions-${Math.random().toString(36).slice(2)}`, []);

  function handleAdd() {
    if (value.trim()) {
      onAdd(value);
      setValue("");
    }
  }

  return (
    <div className="action-list-editor">
      <div className="list-input-row">
        <input
          type="text"
          value={value}
          list={listId}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => event.key === "Enter" && handleAdd()}
          placeholder={placeholder}
        />
        <datalist id={listId}>
          {suggestions.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>
        <button onClick={handleAdd} disabled={!value.trim()}>
          Add
        </button>
      </div>
      {actions.length > 0 && (
        <ul className="list-items">
          {actions.map((action, index) => (
            <li key={`${action}-${index}`}>
              <code>{action}</code>
              <button onClick={() => onRemove(index)}>×</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
