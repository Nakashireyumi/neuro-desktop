import { useEffect, useState } from "react";

type PermissionScope = "input" | "filesystem" | "process" | "network" | "system" | "vision";

type ScopeLimits = {
  max_actions_per_minute?: number;
  max_concurrent_actions?: number;
  allowed_paths?: string[];
  denied_paths?: string[];
  allowed_processes?: string[];
  denied_processes?: string[];
  allowed_hosts?: string[];
  denied_hosts?: string[];
};

type ScopeConfig = {
  allowed: boolean;
  limits: ScopeLimits;
};

type PermissionPolicy = {
  version: string;
  default_allow: boolean;
  created_at?: string;
  signature?: string;
  signed_by?: string;
  metadata?: Record<string, string>;
  scopes: Record<PermissionScope, ScopeConfig>;
  allowed_actions: string[];
  denied_actions: string[];
};

const SCOPE_DESCRIPTIONS: Record<PermissionScope, { title: string; description: string; icon: string }> = {
  input: {
    title: "Input Control",
    description: "Mouse, keyboard, and desktop interaction actions",
    icon: "🖱️",
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
    description: "Shutdown, lock workstation, and system-wide operations",
    icon: "🔒",
  },
  vision: {
    title: "Vision & Context",
    description: "Screenshot capture, context collection, and vision analysis",
    icon: "👁️",
  },
};

const DEFAULT_POLICY: PermissionPolicy = {
  version: "1.0.0",
  default_allow: false,
  metadata: {
    description: "Neuro Desktop Permission Policy — place exported permissions.json next to neuro-integration",
    environment: "development",
  },
  scopes: {
    input: { allowed: true, limits: { max_actions_per_minute: 120, max_concurrent_actions: 3 } },
    filesystem: { allowed: false, limits: { allowed_paths: ["./plugins", "./catalog", "./config"] } },
    process: { allowed: true, limits: {} },
    network: { allowed: true, limits: { allowed_hosts: ["localhost", "127.0.0.1"] } },
    system: { allowed: false, limits: {} },
    vision: { allowed: true, limits: { max_actions_per_minute: 10 } },
  },
  allowed_actions: [],
  denied_actions: ["shutdown_immediately", "lock_workstation", "install_extension"],
};

const STORAGE_KEY = "nd_permission_policy_v1";

function loadPolicyFromStorage(): PermissionPolicy | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as PermissionPolicy;
    return parsed;
  } catch {
    return null;
  }
}

export default function PermissionsPage() {
  const [policy, setPolicy] = useState<PermissionPolicy>(() => {
    return loadPolicyFromStorage() || DEFAULT_POLICY;
  });
  const [selectedScope, setSelectedScope] = useState<PermissionScope>("input");
  const [showJsonPreview, setShowJsonPreview] = useState(false);
  const [isDirty, setIsDirty] = useState(false);

  useEffect(() => {
    window.ndHost?.send("save_permission_policy", policy);
  }, [policy]);

  function updatePolicy(next: Partial<PermissionPolicy>) {
    setPolicy((prev) => ({ ...prev, ...next }));
    setIsDirty(true);
  }

  function updateScope(scope: PermissionScope, config: Partial<ScopeConfig>) {
    setPolicy((prev) => ({
      ...prev,
      scopes: {
        ...prev.scopes,
        [scope]: { ...prev.scopes[scope], ...config },
      },
    }));
    setIsDirty(true);
  }

  function updateLimits(scope: PermissionScope, limits: Partial<ScopeLimits>) {
    setPolicy((prev) => ({
      ...prev,
      scopes: {
        ...prev.scopes,
        [scope]: {
          ...prev.scopes[scope],
          limits: { ...prev.scopes[scope].limits, ...limits },
        },
      },
    }));
    setIsDirty(true);
  }

  function addToList(scope: PermissionScope, key: keyof ScopeLimits, value: string) {
    if (!value.trim()) return;
    const current = policy.scopes[scope].limits[key] as string[] | undefined;
    const newList = current ? [...current, value.trim()] : [value.trim()];
    updateLimits(scope, { [key]: newList });
  }

  function removeFromList(scope: PermissionScope, key: keyof ScopeLimits, index: number) {
    const current = policy.scopes[scope].limits[key] as string[];
    if (!current) return;
    const newList = current.filter((_, i) => i !== index);
    updateLimits(scope, { [key]: newList });
  }

  function addActionToList(listType: "allowed" | "denied", action: string) {
    if (!action.trim()) return;
    const key = listType === "allowed" ? "allowed_actions" : "denied_actions";
    const current = policy[key];
    if (current.includes(action.trim())) return;
    updatePolicy({ [key]: [...current, action.trim()] });
  }

  function removeActionFromList(listType: "allowed" | "denied", index: number) {
    const key = listType === "allowed" ? "allowed_actions" : "denied_actions";
    const current = policy[key];
    updatePolicy({ [key]: current.filter((_, i) => i !== index) });
  }

  function resetToDefaults() {
    if (confirm("Reset policy to default settings? This will discard all changes.")) {
      setPolicy(DEFAULT_POLICY);
      setIsDirty(true);
    }
  }

  function exportPolicy() {
    // Export Go-compatible runtime policy (scopes + allow/deny lists).
    const runtimePolicy = {
      version: policy.version,
      default_allow: policy.default_allow,
      allowed_actions: policy.allowed_actions,
      denied_actions: policy.denied_actions,
      scopes: Object.fromEntries(
        Object.entries(policy.scopes).map(([k, v]) => [k, { allowed: v.allowed }])
      ),
    };
    const json = JSON.stringify(runtimePolicy, null, 2);
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "permissions.json";
    a.click();
    URL.revokeObjectURL(url);
  }

  function importPolicy(file: File) {
    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const imported = JSON.parse(e.target?.result as string) as PermissionPolicy;
        setPolicy(imported);
        setIsDirty(true);
      } catch (err) {
        alert("Failed to parse policy file: Invalid JSON");
      }
    };
    reader.readAsText(file);
  }

  const selectedScopeConfig = policy.scopes[selectedScope];
  const allScopes: PermissionScope[] = ["input", "filesystem", "process", "network", "system", "vision"];

  return (
    <div className="permissions-page">
      <header className="permissions-header">
        <div>
          <h1>Permission Policy Editor</h1>
          <p>Configure access control scopes and action permissions for Neuro Desktop</p>
        </div>
        <div className="header-actions">
          <button className="secondary" onClick={resetToDefaults}>Reset</button>
          <label className="secondary">
            Import
            <input
              type="file"
              accept=".json"
              onChange={(e) => e.target.files?.[0] && importPolicy(e.target.files[0])}
              style={{ display: "none" }}
            />
          </label>
          <button className="secondary" onClick={exportPolicy}>Export</button>
          <button className="primary" onClick={() => setShowJsonPreview(!showJsonPreview)}>
            {showJsonPreview ? "Hide" : "Preview"} JSON
          </button>
        </div>
      </header>

      <main className="permissions-main">
        <aside className="permissions-sidebar">
          <section>
            <h2>Permission Scopes</h2>
            {allScopes.map((scope) => {
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
                    <small className={config.allowed ? "allowed" : "denied"}>
                      {config.allowed ? "Allowed" : "Restricted"}
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
                onChange={(e) => updatePolicy({ default_allow: e.target.value === "allow" })}
              >
                <option value="deny">Deny by Default</option>
                <option value="allow">Allow by Default</option>
              </select>
            </label>
            <label className="setting-row">
              <span>Policy Version</span>
              <input
                type="text"
                value={policy.version}
                onChange={(e) => updatePolicy({ version: e.target.value })}
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
                checked={selectedScopeConfig.allowed}
                onChange={(e) => updateScope(selectedScope, { allowed: e.target.checked })}
              />
              <span className="toggle-slider" />
            </label>
          </div>

          {!selectedScopeConfig.allowed && (
            <div className="scope-warning">
              ⚠️ This scope is currently restricted. Actions in this category will be denied.
            </div>
          )}

          <div className="limits-section">
            <h3>Rate Limits & Restrictions</h3>

            <div className="limits-grid">
              <label className="limit-input">
                <span>Max Actions/Minute</span>
                <input
                  type="number"
                  min="0"
                  value={selectedScopeConfig.limits.max_actions_per_minute || ""}
                  onChange={(e) => updateLimits(selectedScope, { max_actions_per_minute: parseInt(e.target.value) || 0 })}
                  placeholder="Unlimited"
                  disabled={!selectedScopeConfig.allowed}
                />
              </label>

              <label className="limit-input">
                <span>Max Concurrent Actions</span>
                <input
                  type="number"
                  min="1"
                  value={selectedScopeConfig.limits.max_concurrent_actions || ""}
                  onChange={(e) => updateLimits(selectedScope, { max_concurrent_actions: parseInt(e.target.value) || 0 })}
                  placeholder="Unlimited"
                  disabled={!selectedScopeConfig.allowed}
                />
              </label>
            </div>

            {/* Path restrictions for filesystem scope */}
            {selectedScope === "filesystem" && (
              <>
                <ListEditor
                  title="Allowed Paths"
                  items={selectedScopeConfig.limits.allowed_paths || []}
                  onAdd={(value) => addToList(selectedScope, "allowed_paths", value)}
                  onRemove={(index) => removeFromList(selectedScope, "allowed_paths", index)}
                  placeholder="e.g., ./plugins, C:/Games"
                  disabled={!selectedScopeConfig.allowed}
                />
                <ListEditor
                  title="Denied Paths"
                  items={selectedScopeConfig.limits.denied_paths || []}
                  onAdd={(value) => addToList(selectedScope, "denied_paths", value)}
                  onRemove={(index) => removeFromList(selectedScope, "denied_paths", index)}
                  placeholder="e.g., C:/Windows, $HOME/.ssh"
                  disabled={!selectedScopeConfig.allowed}
                />
              </>
            )}

            {/* Process restrictions for process scope */}
            {selectedScope === "process" && (
              <>
                <ListEditor
                  title="Allowed Processes"
                  items={selectedScopeConfig.limits.allowed_processes || []}
                  onAdd={(value) => addToList(selectedScope, "allowed_processes", value)}
                  onRemove={(index) => removeFromList(selectedScope, "allowed_processes", index)}
                  placeholder="e.g., notepad.exe, explorer.exe"
                  disabled={!selectedScopeConfig.allowed}
                />
                <ListEditor
                  title="Denied Processes"
                  items={selectedScopeConfig.limits.denied_processes || []}
                  onAdd={(value) => addToList(selectedScope, "denied_processes", value)}
                  onRemove={(index) => removeFromList(selectedScope, "denied_processes", index)}
                  placeholder="e.g., taskmgr.exe, regedit.exe"
                  disabled={!selectedScopeConfig.allowed}
                />
              </>
            )}

            {/* Network restrictions for network scope */}
            {selectedScope === "network" && (
              <>
                <ListEditor
                  title="Allowed Hosts"
                  items={selectedScopeConfig.limits.allowed_hosts || []}
                  onAdd={(value) => addToList(selectedScope, "allowed_hosts", value)}
                  onRemove={(index) => removeFromList(selectedScope, "allowed_hosts", index)}
                  placeholder="e.g., localhost, api.example.com"
                  disabled={!selectedScopeConfig.allowed}
                />
                <ListEditor
                  title="Denied Hosts"
                  items={selectedScopeConfig.limits.denied_hosts || []}
                  onAdd={(value) => addToList(selectedScope, "denied_hosts", value)}
                  onRemove={(index) => removeFromList(selectedScope, "denied_hosts", index)}
                  placeholder="e.g., malicious.com"
                  disabled={!selectedScopeConfig.allowed}
                />
              </>
            )}
          </div>
        </section>

        <aside className="actions-sidebar">
          <section>
            <h2>Explicitly Allowed Actions</h2>
            <ActionListEditor
              actions={policy.allowed_actions}
              onAdd={(action) => addActionToList("allowed", action)}
              onRemove={(index) => removeActionFromList("allowed", index)}
              placeholder="e.g., run_script, move_mouse_to"
            />
          </section>

          <section>
            <h2>Explicitly Denied Actions</h2>
            <ActionListEditor
              actions={policy.denied_actions}
              onAdd={(action) => addActionToList("denied", action)}
              onRemove={(index) => removeActionFromList("denied", index)}
              placeholder="e.g., shutdown_immediately"
            />
          </section>

          <section className="policy-info">
            <h3>Policy Summary</h3>
            <div className="info-card">
              <p><strong>Version:</strong> {policy.version}</p>
              <p><strong>Default:</strong> {policy.default_allow ? "Allow" : "Deny"}</p>
              <p><strong>Allowed Actions:</strong> {policy.allowed_actions.length}</p>
              <p><strong>Denied Actions:</strong> {policy.denied_actions.length}</p>
              {policy.signed_by && (
                <p><strong>Signed By:</strong> {policy.signed_by}</p>
              )}
            </div>
            {isDirty && (
              <p className="dirty-notice">⚠️ Unsaved changes</p>
            )}
          </section>
        </aside>
      </main>

      {showJsonPreview && (
        <div className="json-preview">
          <header>
            <h3>Policy JSON Preview</h3>
            <button onClick={() => setShowJsonPreview(false)}>Close</button>
          </header>
          <pre>{JSON.stringify(policy, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

// List Editor Component
function ListEditor({
  title,
  items,
  onAdd,
  onRemove,
  placeholder,
  disabled,
}: {
  title: string;
  items: string[];
  onAdd: (value: string) => void;
  onRemove: (index: number) => void;
  placeholder: string;
  disabled?: boolean;
}) {
  const [value, setValue] = useState("");

  function handleAdd() {
    if (value.trim()) {
      onAdd(value);
      setValue("");
    }
  }

  return (
    <div className="list-editor">
      <h4>{title}</h4>
      <div className="list-input-row">
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyPress={(e) => e.key === "Enter" && handleAdd()}
          placeholder={placeholder}
          disabled={disabled}
        />
        <button onClick={handleAdd} disabled={disabled || !value.trim()}>Add</button>
      </div>
      {items.length > 0 && (
        <ul className="list-items">
          {items.map((item, index) => (
            <li key={index}>
              <span>{item}</span>
              <button onClick={() => onRemove(index)} disabled={disabled}>×</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// Action List Editor Component
function ActionListEditor({
  actions,
  onAdd,
  onRemove,
  placeholder,
}: {
  actions: string[];
  onAdd: (action: string) => void;
  onRemove: (index: number) => void;
  placeholder: string;
}) {
  const [value, setValue] = useState("");

  function handleAdd() {
    if (value.trim()) {
      onAdd(value.toLowerCase().trim());
      setValue("");
    }
  }

  return (
    <div className="action-list-editor">
      <div className="list-input-row">
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyPress={(e) => e.key === "Enter" && handleAdd()}
          placeholder={placeholder}
        />
        <button onClick={handleAdd} disabled={!value.trim()}>Add</button>
      </div>
      {actions.length > 0 && (
        <ul className="list-items">
          {actions.map((action, index) => (
            <li key={index}>
              <code>{action}</code>
              <button onClick={() => onRemove(index)}>×</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
