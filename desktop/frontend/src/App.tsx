import RuntimePanel from "./RuntimePanel";
import { useCallback, useEffect, useRef, useState } from "react";
import OperatorControls from "./OperatorControls";
import ActivityPanel from "./ActivityPanel";
import ExtensionPolicyEditor from "./ExtensionPolicyEditor";
import GamesPanel from "./GamesPanel";
import MarketplacePanel from "./MarketplacePanel";
import PermissionsPage from "./PermissionsPage";
import SignIn from "./SignIn";
import VisionServicePanel from "./VisionServicePanel";
import {
  ApiError,
  AUTH_REQUIRED_EVENT,
  api,
  safeExternalURL,
  setAdminToken,
  type CatalogItem,
  type ConfigPayload,
  type InstalledExtension,
  type ManagerSession,
  type RelayStatus,
  type StatusPayload,
  type VisionServiceState,
} from "./api";

type Tab = "extensions" | "marketplace" | "games" | "permissions" | "activity" | "settings";

const INSTALL_MODE_LABELS: Record<string, string> = {
  metadata_only: "Metadata only (records the install)",
  git_clone: "GitHub clone into the plugin directory",
  mcp_endpoint: "MCP endpoint (external server)",
};

export default function App() {
  const [activeTab, setActiveTab] = useState<Tab>("extensions");
  const [session, setSession] = useState<ManagerSession | null>(null);
  const [status, setStatus] = useState<StatusPayload | null>(null);
  const [config, setConfig] = useState<ConfigPayload | null>(null);
  const [extensions, setExtensions] = useState<InstalledExtension[]>([]);
  const [catalog, setCatalog] = useState<CatalogItem[]>([]);
  const [visionService, setVisionService] = useState<VisionServiceState | null>(null);
  const [installMode, setInstallMode] = useState<string>("");
  const [extensionDir, setExtensionDir] = useState<string>("");
  const [selectedId, setSelectedId] = useState<string>("");
  const [message, setMessage] = useState<{ kind: "ok" | "error"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [connectionError, setConnectionError] = useState("");
  const [extensionsError, setExtensionsError] = useState("");
  const [editingPolicy, setEditingPolicy] = useState(false);
  const [operatorActionToken, setOperatorActionToken] = useState("");
  const [recoveryBusy, setRecoveryBusy] = useState<"pause" | "release" | "">("");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const loadVersion = useRef(0);
  const authVersion = useRef(0);
  const sessionRef = useRef<ManagerSession | null>(null);
  const configLoaded = useRef(false);
  const failedRefreshes = useRef(0);
  // "ok" shows the dashboard. "signin" and "setup" show the gate instead, and
  // the reason is the server's own text when it gave one.
  const [gate, setGate] = useState<{ kind: "ok" | "checking" } | { kind: "signin" | "setup"; reason?: string }>({
    kind: "checking",
  });
  const polling = gate.kind === "ok" || gate.kind === "checking";

  const signOut = useCallback(() => {
    loadVersion.current++;
    authVersion.current++;
    sessionRef.current = null;
    configLoaded.current = false;
    setSession(null);
    setActiveTab("extensions");
    setSelectedId("");
    setEditingPolicy(false);
    setOperatorActionToken("");
    setBusy(false);
    setRecoveryBusy("");
    setAdminToken("");
    setStatus(null);
    setConfig(null);
    setExtensions([]);
    setCatalog([]);
    setVisionService(null);
    setMessage(null);
    setConnectionError("");
    setExtensionsError("");
    setLastUpdated(null);
    setGate({ kind: "signin" });
  }, []);

  const load = useCallback(async () => {
    const version = ++loadVersion.current;
    try {
      const sessionPayload = sessionRef.current ?? await api.session();
      if (sessionPayload.role !== "operator" && sessionPayload.role !== "neuro") {
        throw new ApiError(0, "The bridge did not identify this session's role. Update the bridge before using the manager.");
      }
      const [statusPayload, extensionsResult, configPayload] = await Promise.all([
        api.status(),
        activeTab === "extensions" ? api.extensions().then((value) => ({ value, error: undefined })).catch((error) => ({ value: undefined, error })) : Promise.resolve(null),
        configLoaded.current ? Promise.resolve(null) : api.config().catch(() => null),
      ]);
      if (version !== loadVersion.current) return;
      sessionRef.current = sessionPayload;
      setSession(sessionPayload);
      setStatus(statusPayload);
      if (extensionsResult?.value) {
        const extensionsPayload = extensionsResult.value;
        setExtensions(extensionsPayload.installed);
        setCatalog(extensionsPayload.catalog);
        setVisionService(extensionsPayload.built_in_services?.vision ?? null);
        setInstallMode(extensionsPayload.install_mode);
        setExtensionDir(extensionsPayload.extension_dir);
        setExtensionsError("");
        setSelectedId((current) => {
          if (current && (extensionsPayload.installed.some((item) => item.id === current) || extensionsPayload.catalog.some((item) => item.id === current))) return current;
          return extensionsPayload.installed[0]?.id ?? extensionsPayload.catalog[0]?.id ?? "";
        });
      } else if (extensionsResult?.error) {
        setExtensions([]);
        setCatalog([]);
        setVisionService(null);
        setExtensionsError(extensionsResult.error instanceof Error ? extensionsResult.error.message : "Could not read the extension catalog.");
      }
      if (configPayload) {
        setConfig(configPayload);
        configLoaded.current = true;
      }
      failedRefreshes.current = 0;
      setConnectionError("");
      setLastUpdated(new Date());
      setGate({ kind: "ok" });
    } catch (error) {
      if (version !== loadVersion.current) return;
      if (error instanceof ApiError && error.status === 401) {
        setGate({ kind: "signin" });
        return;
      }
      if (error instanceof ApiError && error.status === 503) {
        setGate({ kind: "setup", reason: error.message });
        return;
      }
      failedRefreshes.current++;
      setConnectionError(error instanceof Error ? error.message : "Could not reach the bridge");
    }
  }, [activeTab]);

  const operator = session?.role === "operator";
  useEffect(() => {
    if (session && !operator && (activeTab === "permissions" || activeTab === "activity")) setActiveTab("extensions");
  }, [activeTab, operator, session]);

  useEffect(() => {
    setEditingPolicy(false);
    setOperatorActionToken("");
  }, [selectedId, activeTab]);

  useEffect(() => {
    window.addEventListener(AUTH_REQUIRED_EVENT, signOut);
    return () => window.removeEventListener(AUTH_REQUIRED_EVENT, signOut);
  }, [signOut]);

  useEffect(() => {
    if (!polling) return;
    let cancelled = false;
    let timer: number;
    const refresh = async () => {
      await load();
      if (!cancelled) timer = window.setTimeout(() => void refresh(), document.hidden ? 30000 : Math.min(60000, 5000 * 2 ** Math.min(failedRefreshes.current, 4)));
    };
    void refresh();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      loadVersion.current++;
    };
  }, [load, polling]);

  async function extensionAction(id: string, action: "install" | "enable" | "disable" | "uninstall") {
    const generation = authVersion.current;
    const freshToken = operatorActionToken;
    setOperatorActionToken("");
    setBusy(true);
    setMessage(null);
    try {
      const result = await api.extensionAction(id, action, operator ? freshToken : undefined);
      if (generation !== authVersion.current) return;
      setMessage({ kind: "ok", text: result.message || `${action} ok` });
      await load();
    } catch (error) {
      if (generation !== authVersion.current) return;
      setMessage({
        kind: "error",
        text:
          error instanceof ApiError
            ? `${action} failed: ${error.message}`
            : `${action} failed`,
      });
    } finally {
      if (generation === authVersion.current) setBusy(false);
    }
  }

  async function delegatedRecovery(action: "pause" | "release") {
    const generation = authVersion.current;
    setRecoveryBusy(action);
    setMessage(null);
    try {
      const result = await (action === "pause" ? api.pause() : api.releaseInput());
      if (generation !== authVersion.current) return;
      setMessage({ kind: "ok", text: result.message });
      if (action === "pause") await load();
    } catch (failure) {
      if (generation === authVersion.current) setMessage({ kind: "error", text: failure instanceof Error ? failure.message : "Could not complete recovery." });
    } finally {
      if (generation === authVersion.current) setRecoveryBusy("");
    }
  }

  const installedById = new Map(extensions.map((item) => [item.id, item]));
  const catalogById = new Map(catalog.map((item) => [item.id, item]));
  const allIds = [
    ...extensions.map((item) => item.id),
    ...catalog.filter((item) => !installedById.has(item.id)).map((item) => item.id),
  ];
  const selected = (
    installedById.get(selectedId) ??
    (catalogById.get(selectedId)
      ? {
          id: selectedId,
          enabled: false,
          name: catalogById.get(selectedId)?.name,
          description: catalogById.get(selectedId)?.description,
          type: catalogById.get(selectedId)?.type,
          repository: catalogById.get(selectedId)?.repository,
          source: "catalog",
        }
      : undefined)
  ) as InstalledExtension | undefined;
  const selectedInstalled = installedById.has(selectedId);

  const core = extensions.filter((item) => item.type === "desktop-integration" || item.id === "neuro-desktop");
  const installedPlugins = extensions.filter((item) => !core.includes(item));

  const relay: RelayStatus | undefined = status?.relay;
  const executorOnline = !connectionError && Boolean(status?.executor.connected);

  if (gate.kind === "signin" || gate.kind === "setup") {
    return <SignIn mode={gate.kind} reason={gate.reason} onSignedIn={() => void load()} />;
  }

  if (gate.kind === "checking") {
    return (
      <div className="signin-root">
        <section className="signin-card" aria-live="polite">
          <h1>Neurcro</h1>
          <p>{connectionError || "Connecting to the bridge…"}</p>
          {connectionError && <button className="primary" onClick={() => void load()}>Try again</button>}
        </section>
      </div>
    );
  }

  return (
    <div className="manager-root">
      <header className="manager-titlebar">
        <div className="titlebar-left">
          <h1>Neurcro</h1>
          <nav className="titlebar-nav">
            {(["extensions", "marketplace", "games", ...(operator ? ["permissions", "activity"] : []), "settings"] as Tab[]).map((tab) => (
              <button
                key={tab}
                className={`nav-tab ${activeTab === tab ? "active" : ""}`}
                onClick={() => setActiveTab(tab)}
              >
                {tab === "settings" ? "Status" : tab[0].toUpperCase() + tab.slice(1)}
              </button>
            ))}
          </nav>
        </div>
        <div className="titlebar-right">
          <span className={`status-indicator ${executorOnline ? "online" : "offline"}`}>●</span>
          <span className="status-text">
            {connectionError ? "bridge unreachable · reconnecting…" : status
              ? `bridge ${status.version} · executor ${executorOnline ? "online" : "offline"} · ${
                  status.actions.actions_received ?? 0
                } actions`
              : "connecting to the bridge…"}
          </span>
          <button className="secondary" onClick={signOut}>Sign out</button>
        </div>
      </header>

      {connectionError && (
        <div className="scope-warning" role="alert">
          {connectionError} {lastUpdated && `Showing data from ${lastUpdated.toLocaleTimeString()}. `}
          Retrying automatically. <button className="secondary" onClick={() => void load()}>Retry now</button>
        </div>
      )}

      {operator ? <OperatorControls control={status?.control ?? null} connected={!connectionError && Boolean(status)} onChanged={load} /> : (
        <section className="operator-controls" aria-label="Delegated controls">
          <div><strong>Delegated session</strong><p className="hint">You can use your granted abilities, pause and release input. Permission changes and resuming require your operator session.{session?.expires_at && ` Expires ${new Date(session.expires_at).toLocaleTimeString()}.`}</p></div>
          <div className="button-row">
            <button className="danger" disabled={Boolean(recoveryBusy) || Boolean(connectionError) || !status || Boolean(status.control?.paused)} onClick={() => void delegatedRecovery("pause")}>{status?.control?.paused ? "Actions paused" : "Pause actions"}</button>
            <button className="secondary" disabled={recoveryBusy === "release" || Boolean(connectionError) || !status} onClick={() => void delegatedRecovery("release")}>{recoveryBusy === "release" ? "Releasing…" : "Release all input"}</button>
          </div>
        </section>
      )}

      {message && (
        <div className={message.kind === "ok" ? "save-notice" : "scope-warning"}>
          {message.kind === "ok" ? "✅" : "⚠️"} {message.text}
        </div>
      )}

      {operator && activeTab === "permissions" && <PermissionsPage />}

      {activeTab === "marketplace" && <MarketplacePanel operator={operator} />}

      {activeTab === "games" && <GamesPanel executor={executorOnline ? status?.executor ?? null : null} />}

      {operator && activeTab === "activity" && <ActivityPanel />}

      {activeTab === "extensions" && (
        <>
        {extensionsError && <p className="scope-warning" role="alert">{extensionsError}</p>}
        <VisionServicePanel state={visionService} operator={operator} onSaved={(next) => { setVisionService(next); void load(); }} />
        <main className="manager-main manager-main--extensions">
          <aside className="manager-sidebar">
            <section>
              <h2>Core Services</h2>
              {core.map((item) => (
                <button
                  key={item.id}
                  className={`sidebar-item ${selectedId === item.id ? "selected" : ""}`}
                  onClick={() => setSelectedId(item.id)}
                >
                  <div>
                    <strong>{item.name || item.id}</strong>
                    <small>{item.enabled ? "Active" : "Installed"}</small>
                  </div>
                </button>
              ))}
            </section>

            <section>
              <h2>Extensions</h2>
              {installedPlugins.map((item) => (
                <button
                  key={item.id}
                  className={`sidebar-item ${selectedId === item.id ? "selected" : ""}`}
                  onClick={() => setSelectedId(item.id)}
                >
                  <div>
                    <strong>{item.name || item.id}</strong>
                    <small>
                    {item.enabled ? "Enabled" : "Disabled"} · signature {item.signature_state ?? item.trust ?? "unknown"}
                  </small>
                  </div>
                </button>
              ))}
              {catalog
                .filter((item) => !installedById.has(item.id))
                .map((item) => (
                  <button
                    key={item.id}
                    className={`sidebar-item ${selectedId === item.id ? "selected" : ""}`}
                    onClick={() => setSelectedId(item.id)}
                  >
                    <div>
                      <strong>{item.name}</strong>
                      <small>Not installed · signature {item.signature_state ?? "unknown"}</small>
                    </div>
                  </button>
                ))}
              {allIds.length === 0 && (
                <p className="hint">
                  No catalog entries. Check the catalog path in Status.
                </p>
              )}
            </section>
          </aside>

          <section className="manager-panel">
            {selected ? (
              <>
                <div className="panel-header">
                  <div>
                    <h1>{selected.name || selected.id}</h1>
                    <p>{selected.description?.replace("once Vedal enables this extension", "after you enable this extension") || "No description in the catalog."}</p>
                  </div>
                  {safeExternalURL(selected.repository) && (
                    <a href={safeExternalURL(selected.repository)} target="_blank" rel="noreferrer">
                      Open Repository ↗
                    </a>
                  )}
                </div>

                {operator && <label style={{ display: "block", marginTop: "1rem" }}>Operator token for your next direct action (optional when management is delegated)
                  <input type="password" value={operatorActionToken} autoComplete="new-password" onChange={(event) => setOperatorActionToken(event.target.value)} style={{ display: "block", width: "100%", maxWidth: "34rem", boxSizing: "border-box" }} />
                </label>}

                <div className="status-row">
                  <span className={`pill ${selectedInstalled ? "ok" : "warn"}`}>
                    {selectedInstalled ? "Installed" : "Not Installed"}
                  </span>
                  <span className={`pill ${selected.enabled ? "ok" : "idle"}`}>
                    {selected.enabled ? "Enabled" : "Disabled"}
                  </span>
                  <span className="pill neutral">{INSTALL_MODE_LABELS[installMode] ?? installMode}</span>
                </div>

                {operator && <div className="button-row"><button className="secondary" onClick={() => setEditingPolicy(!editingPolicy)}>{editingPolicy ? "Hide permissions and settings" : "Permissions and settings"}</button></div>}
                {operator && editingPolicy && <ExtensionPolicyEditor key={selected.id} id={selected.id} onClose={() => setEditingPolicy(false)} onSaved={() => void load()} />}

                <div className="button-row">
                  <button
                    className="primary"
                    onClick={() => void extensionAction(selected.id, "install")}
                    disabled={busy || selectedInstalled}
                  >
                    Install
                  </button>
                  <button
                    className="secondary"
                    onClick={() =>
                      void extensionAction(selected.id, selected.enabled ? "disable" : "enable")
                    }
                    disabled={busy || !selectedInstalled}
                  >
                    {selected.enabled ? "Disable" : "Enable"}
                  </button>
                  <button
                    className="danger"
                    onClick={() => void extensionAction(selected.id, "uninstall")}
                    disabled={busy || !selectedInstalled}
                  >
                    Uninstall
                  </button>
                </div>

                <div className="meta-grid">
                  <article>
                    <h3>Installed At</h3>
                    <p>{selected.installed_at || "not installed"}</p>
                  </article>
                  <article>
                    <h3>Source</h3>
                    <p>{selected.source || "—"}</p>
                  </article>
                  <article>
                    <h3>Path</h3>
                    <p>{selected.path || extensionDir}</p>
                  </article>
                  <article>
                    <h3>Type</h3>
                    <p>{selected.type || "extension"}</p>
                  </article>
                </div>

                <p className="hint">
                  Installation follows the mode shown above. Grant management and runtime access separately in Permissions and settings. Metadata-only entries do not fetch code.
                </p>
              </>
            ) : (
              <div className="panel-header">
                <div>
                  <h1>Nothing selected</h1>
                  <p>Pick an extension on the left, or add one to the catalog.</p>
                </div>
              </div>
            )}
          </section>
        </main>
        </>
      )}

      {activeTab === "settings" && (
        <section className="settings-panel">
          {operator && <RuntimePanel />}
          <h1>Bridge Status</h1>
          <div className="settings-grid">
            <article>
              <h3>Runtime</h3>
              <p>Version {status?.version ?? "unknown"}</p>
              <p>Uptime {status ? Math.round(status.uptime / 60) : 0} minutes</p>
              <p>Started {status?.started_at ?? "—"}</p>
            </article>
            <article>
              <h3>Neuro API</h3>
              <p>{status?.neuro?.connected ? "Connected" : "Waiting for Neuro API"}</p>
              <p>{status?.neuro?.attempts ?? 0} connection attempt(s)</p>
              {status?.neuro?.last_error && <p className="denied">{status.neuro.last_error}</p>}
              {!status?.neuro?.connected && <p className="hint">Start Neuro's API server or a local test brain at the configured WebSocket address.</p>}
            </article>
            <article>
              <h3>Executor</h3>
              <p>{executorOnline ? `Connected (${status?.executor.remote})` : "Not connected"}</p>
              <p>
                {status?.executor.completed_commands ?? 0} commands, {status?.executor.failed_commands ?? 0} failed
              </p>
              {status?.executor.last_error && <p className="denied">{status.executor.last_error}</p>}
            </article>
            <article>
              <h3>Relay / coexistence</h3>
              <p>{relay?.enabled ? `Enabled → ${relay.url}` : "Disabled"}</p>
              <p>
                {relay?.registered ? "Registered" : "Not registered"} · {relay?.peer_count ?? 0} peer(s)
              </p>
              {relay?.peers && Object.keys(relay.peers).length > 0 && (
                <ul className="list-items">
                  {Object.entries(relay.peers).map(([name, kind]) => (
                    <li key={name}>
                      <code>{name}</code>
                      <span>{kind}</span>
                    </li>
                  ))}
                </ul>
              )}
              {relay?.reserved_actions && relay.reserved_actions.length > 0 && (
                <p>Reserved for other integrations: {relay.reserved_actions.join(", ")}</p>
              )}
              {relay?.last_error && <p className="denied">{relay.last_error}</p>}
            </article>
            <article>
              <h3>Paths</h3>
              {config &&
                Object.entries(config.paths).map(([name, path]) => (
                  <p key={name}>
                    <strong>{name}:</strong> {path}
                  </p>
                ))}
            </article>
            <article>
              <h3>Dashboard token</h3>
              <p>
                Every request to this dashboard is checked against the token from{" "}
                <code>neuro-integration setup</code>, including requests from this machine.
              </p>
              <p>
                <strong>Source:</strong> {status?.admin.token_source ?? "unknown"}
              </p>
              <div className="button-row">
                <button
                  className="secondary"
                  onClick={signOut}
                >
                  Sign out
                </button>
              </div>
            </article>
            <article>
              <h3>Features</h3>
              {config &&
                Object.entries(config.features).map(([name, enabled]) => (
                  <p key={name}>
                    <strong>{name}:</strong> {enabled ? "on" : "off"}
                  </p>
                ))}
            </article>
          </div>
        </section>
      )}
    </div>
  );
}
