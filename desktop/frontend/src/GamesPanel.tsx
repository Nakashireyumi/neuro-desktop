import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type ExecutorInfo,
  type GameDetected,
  type GameProfileSummary,
  type GameSession,
} from "./api";

type GamesState = {
  profiles: GameProfileSummary[];
  registrySource: string;
  detected: GameDetected | null;
  session: GameSession | null;
};

const MODE_LABELS: Record<string, string> = {
  nd: "Neurcro drives it",
  external: "Dedicated integration drives it",
  hybrid: "Shared (integration + Neurcro)",
  auto: "Auto (decide from the relay peers)",
};

function modeLabel(mode: string): string {
  return MODE_LABELS[mode] ?? mode;
}

export default function GamesPanel({ executor }: { executor: ExecutorInfo | null }) {
  const [state, setState] = useState<GamesState>({
    profiles: [],
    registrySource: "",
    detected: null,
    session: null,
  });
  const [selectedId, setSelectedId] = useState<string>("");
  const [mode, setMode] = useState<string>("auto");
  const [launch, setLaunch] = useState(false);
  const [error, setError] = useState<string>("");
  const [notice, setNotice] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [observation, setObservation] = useState<string>("");

  const load = useCallback(async () => {
    try {
      const payload = await api.games();
      setState({
        profiles: payload.profiles,
        registrySource: payload.registry_source,
        detected: payload.detected,
        session: payload.session,
      });
      setError("");
      setSelectedId((current) => {
        if (current && payload.profiles.some((profile) => profile.id === current)) {
          return current;
        }
        return payload.detected?.profile?.id ?? payload.session?.profile_id ?? payload.profiles[0]?.id ?? "";
      });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not load the game profiles");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 10000);
    return () => window.clearInterval(timer);
  }, [load]);

  const selected = state.profiles.find((profile) => profile.id === selectedId) ?? null;

  async function run(work: () => Promise<string>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      setNotice(await work());
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "The bridge refused the request");
    } finally {
      setBusy(false);
    }
  }

  const startSession = () =>
    run(async () => {
      if (!selected) {
        throw new ApiError(0, "Pick a game profile first");
      }
      const result = await api.startSession(selected.id, launch, mode);
      return result.message;
    });

  const endSession = () =>
    run(async () => {
      const result = await api.endSession();
      return result.message;
    });

  const observe = () =>
    run(async () => {
      const result = await api.observe();
      setObservation(result.observation);
      return "Captured what Neuro would see";
    });

  const executorOnline = Boolean(executor?.connected);

  return (
    <div className="games-page">
      <header className="permissions-header">
        <div>
          <h1>Games</h1>
          <p>
            {state.profiles.length} profile(s) from {state.registrySource || "the profile directory"} ·
            executor {executorOnline ? `connected (${executor?.version || "unknown version"})` : "offline"}
          </p>
        </div>
        <div className="header-actions">
          <button className="secondary" onClick={() => void load()}>
            Refresh
          </button>
          <button className="secondary" onClick={() => void observe()} disabled={busy || !executorOnline}>
            Show me what Neuro sees
          </button>
        </div>
      </header>

      {error && <div className="scope-warning">⚠️ {error}</div>}
      {notice && <div className="save-notice">✅ {notice}</div>}

      {state.detected?.profile && (
        <div className="save-notice">
          🎮 Detected <strong>{state.detected.profile.name}</strong>
          {state.detected.active_window ? ` in "${state.detected.active_window}"` : ""} —
          matched on {state.detected.matched_on || "window/process rules"}.
        </div>
      )}

      {state.session && (
        <div className="session-card">
          <div>
            <h3>Active session: {state.session.profile_name}</h3>
            <p>
              {modeLabel(state.session.control_mode)} · {state.session.actions_issued} action(s) issued ·
              last: {state.session.last_action || "none"}
              {state.session.external_integration
                ? ` · external integration: ${state.session.external_integration}`
                : ""}
            </p>
          </div>
          <button className="danger" onClick={() => void endSession()} disabled={busy}>
            End session
          </button>
        </div>
      )}

      <main className="manager-main manager-main--extensions">
        <aside className="manager-sidebar">
          <section>
            <h2>Game profiles</h2>
            {state.profiles.map((profile, index) => (
              <button
                key={profile.id}
                className={`sidebar-item ${selectedId === profile.id ? "selected" : ""}`}
                style={{ animationDelay: `${index * 0.04}s` }}
                onClick={() => {
                  setSelectedId(profile.id);
                  setMode("auto");
                  setLaunch(false);
                }}
              >
                <div>
                  <strong>{profile.name}</strong>
                  <small>
                    {profile.mode} · {profile.vision ? "vision" : "no vision"}
                  </small>
                </div>
              </button>
            ))}
            {state.profiles.length === 0 && (
              <p className="hint">
                No profiles found. Add JSON files to desktop/catalog/games or point
                NEURO_GAME_PROFILES_DIR at a directory.
              </p>
            )}
          </section>
        </aside>

        <section className="manager-panel">
          {selected ? (
            <>
              <div className="panel-header">
                <div>
                  <h1>{selected.name}</h1>
                  <p>{selected.description || "No description in the profile."}</p>
                </div>
                <span className={`pill ${selected.mode === "external" ? "idle" : "ok"}`}>
                  {selected.mode}
                </span>
              </div>

              <div className="status-row">
                <span className="pill neutral">{selected.mouse_look ? "mouse look" : "keys only"}</span>
                {selected.vision && <span className="pill ok">vision</span>}
                {selected.launchable && <span className="pill neutral">launchable</span>}
                {selected.tags?.map((tag) => (
                  <span className="pill neutral" key={tag}>
                    {tag}
                  </span>
                ))}
              </div>

              {selected.keys && Object.keys(selected.keys).length > 0 && (
                <div className="meta-grid">
                  <article>
                    <h3>Keybinds</h3>
                    <ul className="list-items">
                      {Object.entries(selected.keys).map(([intent, key]) => (
                        <li key={intent}>
                          <code>{intent}</code>
                          <span>{key}</span>
                        </li>
                      ))}
                    </ul>
                  </article>
                  <article>
                    <h3>Control mode</h3>
                    <p>{modeLabel(selected.mode)}</p>
                    {selected.external && (
                      <p>
                        Looks for the relay peer <code>{selected.external}</code>.
                      </p>
                    )}
                  </article>
                </div>
              )}

              <div className="option-row">
                <label>
                  Mode for this session
                  <select value={mode} onChange={(event) => setMode(event.target.value)}>
                    <option value="auto">Auto</option>
                    <option value="nd">Neurcro</option>
                    <option value="external">External integration</option>
                    <option value="hybrid">Hybrid</option>
                  </select>
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={launch}
                    disabled={!selected.launchable}
                    onChange={(event) => setLaunch(event.target.checked)}
                  />
                  Launch the game (needs the system scope)
                </label>
              </div>

              <div className="button-row">
                <button
                  className="primary"
                  onClick={() => void startSession()}
                  disabled={busy || state.session?.profile_id === selected.id}
                >
                  {state.session?.profile_id === selected.id ? "Session running" : "Start session"}
                </button>
              </div>

              <p className="hint">
                A session tells Neuro which keybinds exist for this game and which actions Neuro
                Desktop may send. Nothing is pressed until Neuro calls a game action.
              </p>
            </>
          ) : (
            <div className="panel-header">
              <div>
                <h1>No game profile selected</h1>
                <p>Add profiles to desktop/catalog/games to teach Neurcro about a game.</p>
              </div>
            </div>
          )}

          {observation && (
            <div className="limits-section">
              <h3>Current observation</h3>
              <pre className="observation">{observation}</pre>
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
