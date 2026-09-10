import { useEffect, useMemo, useState } from "react";
import PermissionsPage from "./PermissionsPage";
import type { ExtensionState, NDBootstrap } from "./types";

type PluginInstallMethod = "metadata_only" | "git_clone" | "mcp_endpoint";

type Extension = {
  id: string;
  name: string;
  category: "core" | "extension";
  description: string;
  repo?: string;
  tags: string[];
  defaultEnabled?: boolean;
  installMethod: PluginInstallMethod;
  requiresRelay?: boolean;
};

type Tab = "extensions" | "permissions" | "settings";

const EXTENSIONS: Extension[] = [
  {
    id: "neuro-desktop",
    name: "Neuro Desktop",
    category: "core",
    description: "Main Windows control runtime and desktop action bridge.",
    repo: "https://github.com/cassitly/neuro-desktop",
    tags: ["core", "windows", "runtime"],
    defaultEnabled: true,
    installMethod: "metadata_only",
  },
  {
    id: "neuro-relay",
    name: "Neuro Relay",
    category: "core",
    description: "Multiplexes multiple Neuro integrations and game endpoints.",
    repo: "https://github.com/recassity/neuro-relay",
    tags: ["core", "relay", "multiplex"],
    defaultEnabled: false,
    installMethod: "git_clone",
  },
  {
    id: "nd-vision-server",
    name: "ND Vision Server",
    category: "extension",
    description: "External model inference service for screenshot summarization.",
    repo: "https://github.com/Ubuntufanboy/neuro-desktop",
    tags: ["vision", "context", "experimental"],
    defaultEnabled: false,
    installMethod: "git_clone",
  },
  {
    id: "mcp-bridge",
    name: "MCP Bridge",
    category: "extension",
    description: "Model Context Protocol bridge for tool/plugin servers. Not installed by default.",
    repo: "https://github.com/modelcontextprotocol/servers",
    tags: ["mcp", "plugins", "optional"],
    defaultEnabled: false,
    installMethod: "mcp_endpoint",
  },
];

const STORAGE_KEY = "nd_extension_state_v1";

function getBootstrapState(): Record<string, ExtensionState> | null {
  const raw = (window.__ND_BOOTSTRAP as NDBootstrap | undefined)?.extensionState;
  if (!raw || typeof raw !== "object") {
    return null;
  }
  return raw;
}

function withDefaultExtensions(
  incoming: Record<string, ExtensionState>,
): Record<string, ExtensionState> {
  const merged: Record<string, ExtensionState> = {};
  for (const extension of EXTENSIONS) {
    const existing = incoming[extension.id];
    if (existing) {
      merged[extension.id] = existing;
      continue;
    }

    const coreInstalled = extension.category === "core";
    merged[extension.id] = {
      installed: coreInstalled,
      enabled: coreInstalled ? true : Boolean(extension.defaultEnabled),
    };
  }
  return merged;
}

function loadInitialState(): Record<string, ExtensionState> {
  const bootstrap = getBootstrapState();
  if (bootstrap) {
    return withDefaultExtensions(bootstrap);
  }

  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      throw new Error("missing");
    }
    const parsed = JSON.parse(raw) as Record<string, ExtensionState>;
    if (parsed && typeof parsed === "object") {
      return withDefaultExtensions(parsed);
    }
  } catch {
    // fallback defaults below
  }

  return withDefaultExtensions({});
}

function installMethodLabel(method: PluginInstallMethod): string {
  switch (method) {
    case "git_clone":
      return "GitHub Clone";
    case "mcp_endpoint":
      return "MCP Endpoint";
    default:
      return "Metadata Only";
  }
}

export default function App() {
  console.log("App component rendering");
  const [showSplash, setShowSplash] = useState(true);
  const [selectedId, setSelectedId] = useState<string>("neuro-desktop");
  const [state, setState] = useState<Record<string, ExtensionState>>(() =>
    loadInitialState(),
  );
  const [activeTab, setActiveTab] = useState<Tab>("extensions");

  useEffect(() => {
    const timer = window.setTimeout(() => setShowSplash(false), 2100);
    return () => window.clearTimeout(timer);
  }, []);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    window.ndHost?.send("save_extension_state", state);
  }, [state]);

  const selected = useMemo(
    () => EXTENSIONS.find((item) => item.id === selectedId) ?? EXTENSIONS[0],
    [selectedId],
  );

  const selectedState = state[selected.id] ?? { installed: false, enabled: false };
  const coreServices = EXTENSIONS.filter((item) => item.category === "core");
  const plugins = EXTENSIONS.filter((item) => item.category === "extension");
  const installedCount = Object.values(state).filter((item) => item.installed).length;
  const enabledCount = Object.values(state).filter(
    (item) => item.installed && item.enabled,
  ).length;

  function updateState(next: Record<string, ExtensionState>) {
    setState(next);
  }

  function installSelected() {
    const next = {
      ...state,
      [selected.id]: { installed: true, enabled: true },
    };
    updateState(next);
  }

  function uninstallSelected() {
    if (selected.category === "core") {
      return;
    }

    const next = {
      ...state,
      [selected.id]: { installed: false, enabled: false },
    };
    updateState(next);
  }

  function toggleEnabled() {
    if (!selectedState.installed) {
      return;
    }
    const next = {
      ...state,
      [selected.id]: {
        ...selectedState,
        enabled: !selectedState.enabled,
      },
    };
    updateState(next);
  }

  const isNativeShell = Boolean(window.__ND_BOOTSTRAP?.nativeHost);

  if (showSplash) {
    return (
      <div className="splash-screen">
        <div className="splash-overlay" />
        <div className="splash-content">
          <h1>Neuro Desktop</h1>
          <p>Integration Manager</p>
          <span>2026.3</span>
        </div>
      </div>
    );
  }

  return (
    <div className="manager-root">
      <header className="manager-titlebar">
        <div className="titlebar-left">
          <h1>Neuro Desktop</h1>
          <nav className="titlebar-nav">
            <button
              className={`nav-tab ${activeTab === "extensions" ? "active" : ""}`}
              onClick={() => setActiveTab("extensions")}
            >
              Extensions
            </button>
            <button
              className={`nav-tab ${activeTab === "permissions" ? "active" : ""}`}
              onClick={() => setActiveTab("permissions")}
            >
              Permissions
            </button>
            <button
              className={`nav-tab ${activeTab === "settings" ? "active" : ""}`}
              onClick={() => setActiveTab("settings")}
            >
              Settings
            </button>
          </nav>
        </div>
        <div className="titlebar-right">
          <span className="status-indicator">●</span>
          <span className="status-text">
            {installedCount} installed, {enabledCount} enabled
          </span>
        </div>
      </header>

      <main
        className={`manager-main ${activeTab === "extensions" ? "manager-main--extensions" : "manager-main--single"}`}
      >
        {activeTab === "extensions" && (
          <>
            <aside className="manager-sidebar">
              <section>
                <h2>Core Services</h2>
                {coreServices.map((item, index) => {
                  const itemState = state[item.id];
                  return (
                    <button
                      key={item.id}
                      className={`sidebar-item ${selected.id === item.id ? "selected" : ""}`}
                      style={{ animationDelay: `${index * 0.04}s` }}
                      onClick={() => setSelectedId(item.id)}
                    >
                      <div>
                        <strong>{item.name}</strong>
                        <small>{itemState?.enabled ? "Active" : "Installed"}</small>
                      </div>
                    </button>
                  );
                })}
              </section>

              <section>
                <h2>Extensions</h2>
                {plugins.map((item, index) => {
                  const itemState = state[item.id];
                  const status = itemState?.installed
                    ? itemState.enabled
                      ? "Enabled"
                      : "Installed"
                    : "Not installed";
                  return (
                    <button
                      key={item.id}
                      className={`sidebar-item ${selected.id === item.id ? "selected" : ""}`}
                      style={{ animationDelay: `${index * 0.05 + 0.12}s` }}
                      onClick={() => setSelectedId(item.id)}
                    >
                      <div>
                        <strong>{item.name}</strong>
                        <small>{status}</small>
                      </div>
                    </button>
                  );
                })}
              </section>
            </aside>

            <section className="manager-panel">
              <div className="panel-header">
                <div>
                  <h1>{selected.name}</h1>
                  <p>{selected.description}</p>
                </div>
                <a href={selected.repo} target="_blank" rel="noreferrer">
                  Open Repository ↗
                </a>
              </div>

              <div className="status-row">
                <span className={`pill ${selectedState.installed ? "ok" : "warn"}`}>
                  {selectedState.installed ? "Installed" : "Not Installed"}
                </span>
                <span className={`pill ${selectedState.enabled ? "ok" : "idle"}`}>
                  {selectedState.enabled ? "Enabled" : "Disabled"}
                </span>
                <span className="pill neutral">{installMethodLabel(selected.installMethod)}</span>
              </div>

              <div className="option-row">
                <label>
                  <input type="radio" checked={selected.installMethod === "metadata_only"} readOnly />
                  Metadata only
                </label>
                <label>
                  <input type="radio" checked={selected.installMethod === "git_clone"} readOnly />
                  GitHub clone
                </label>
                <label>
                  <input type="radio" checked={selected.installMethod === "mcp_endpoint"} readOnly />
                  MCP endpoint
                </label>
              </div>

              <div className="button-row">
                <button
                  onClick={installSelected}
                  disabled={selectedState.installed}
                  className="primary"
                >
                  Install
                </button>
                <button
                  onClick={toggleEnabled}
                  disabled={!selectedState.installed}
                  className="secondary"
                >
                  {selectedState.enabled ? "Disable" : "Enable"}
                </button>
                <button
                  onClick={uninstallSelected}
                  disabled={selected.category === "core" || !selectedState.installed}
                  className="danger"
                >
                  Uninstall
                </button>
              </div>

              <div className="meta-grid">
                <article>
                  <h3>Tags</h3>
                  <p>{selected.tags.join(", ")}</p>
                </article>
                <article>
                  <h3>Relay Dependency</h3>
                  <p>{selected.requiresRelay ? "Requires Neuro Relay" : "No relay requirement"}</p>
                </article>
                <article>
                  <h3>Installed Extensions</h3>
                  <p>{installedCount} total, {enabledCount} enabled</p>
                </article>
                <article>
                  <h3>Minimize Mode</h3>
                  <p>
                    {isNativeShell
                      ? "Native shell enabled (minimize goes to system tray)."
                      : "Browser mode fallback active."}
                  </p>
                </article>
              </div>
            </section>
          </>
        )}

        {activeTab === "permissions" && <PermissionsPage />}

        {activeTab === "settings" && (
          <section className="settings-panel">
            <h1>Settings</h1>
            <div className="settings-grid">
              <article>
                <h3>Runtime</h3>
                <p>Configure Neuro Desktop runtime behavior and process management.</p>
              </article>
              <article>
                <h3>Network</h3>
                <p>Configure WebSocket URLs, relay settings, and network policies.</p>
              </article>
              <article>
                <h3>Vision</h3>
                <p>Configure vision server URL and context capture settings.</p>
              </article>
              <article>
                <h3>About</h3>
                <p>Neuro Desktop v0.0.3b-dev | Integration Manager 2026.3</p>
              </article>
            </div>
          </section>
        )}
      </main>

      <footer className="manager-footer">
        <button className="linkish" onClick={() => window.ndHost?.send("open_logs")}>
          Open Logs...
        </button>
        <div className="footer-actions">
          <button className="secondary" onClick={() => window.ndHost?.send("open_config")}>
            Settings
          </button>
          <button className="primary" onClick={() => window.ndHost?.send("quit_nd")}>
            Quit Neuro Desktop
          </button>
        </div>
      </footer>
    </div>
  );
}
