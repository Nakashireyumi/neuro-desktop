import { useEffect, useState } from "react";
import { api, type RuntimePayload } from "./api";

const POLL_MS = 5000;

type Tone = "ok" | "warn" | "bad" | "muted";

// Maps each runtime state to a colour tone. Kept in one place so the cards
// agree with each other and with the states documented in docs/RELAY.md.
const TONES: Record<string, Tone> = {
  running: "ok",
  connected: "ok",
  registered: "ok",
  reachable: "ok",
  idle: "warn",
  waiting: "warn",
  starting: "warn",
  connecting: "warn",
  disconnected: "bad",
  unreachable: "bad",
  failed: "bad",
  disabled: "muted",
  not_configured: "muted",
  off: "muted",
};

const LABELS: Record<string, string> = {
  not_configured: "not configured",
};

function toneFor(state: string): Tone {
  return TONES[state] ?? "muted";
}

function label(state: string): string {
  return LABELS[state] ?? state.replace(/_/g, " ");
}

function Card({ title, state, lines }: { title: string; state: string; lines: string[] }) {
  return (
    <article className={`runtime-card runtime-card--${toneFor(state)}`}>
      <header>
        <strong>{title}</strong>
        <span className={`runtime-badge runtime-badge--${toneFor(state)}`}>{label(state)}</span>
      </header>
      {lines.filter(Boolean).map((line) => (
        <small key={line}>{line}</small>
      ))}
    </article>
  );
}

/**
 * Live state of everything the bridge depends on. It is shown above the
 * extension list because "is it running" is the question operators ask first.
 */
export default function RuntimePanel() {
  const [runtime, setRuntime] = useState<RuntimePayload | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const next = await api.runtime();
        if (!cancelled) {
          setRuntime(next);
          setError("");
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  if (!runtime) {
    return (
      <section className="runtime-panel" aria-label="Live runtime">
        <p className="hint">{error ? `Could not read the runtime: ${error}` : "Checking what is running…"}</p>
      </section>
    );
  }

  const { bridge, neuro, executor, relay, vision, mcp } = runtime;
  const mcpRunning = mcp.servers.filter((server) => server.state === "running").map((server) => server.id);

  return (
    <section className="runtime-panel" aria-label="Live runtime">
      <div className="runtime-grid">
        <Card
          title="Bridge"
          state={bridge.state}
          lines={[`up ${Math.round(bridge.uptime_seconds / 60)} min`, bridge.paused ? "paused by the operator" : ""]}
        />
        <Card
          title="Neuro API"
          state={neuro?.state ?? "waiting"}
          lines={[neuro?.connected ? "ready to receive actions" : "waiting for Neuro's API server", neuro?.last_error ?? ""]}
        />
        <Card
          title="Executor (the PC agent)"
          state={executor.state}
          lines={[executor.connected ? "the agent is connected" : "start the agent to act on this PC"]}
        />
        <Card
          title="Relay"
          state={relay.state}
          lines={[
            relay.url ? relay.url : "",
            relay.state === "idle" ? "registered, nothing behind it yet" : "",
            relay.last_error ?? "",
          ]}
        />
        <Card
          title="Vision server"
          state={vision.state}
          lines={[
            vision.url ?? "set NEURO_VISION_URL to enable",
            vision.backend ? `backend: ${vision.backend}` : "",
            vision.latency_ms !== undefined && vision.reachable ? `${vision.latency_ms} ms` : "",
            vision.error ?? "",
          ]}
        />
        <Card
          title="MCP bridge"
          state={mcp.state}
          lines={[
            mcp.running > 0 ? `running: ${mcpRunning.join(", ")}` : "no MCP servers running",
            ...mcp.servers.filter((server) => server.last_error).map((server) => `${server.id}: ${server.last_error}`),
          ]}
        />
      </div>
      {error && <small className="runtime-error">Last refresh failed: {error}</small>}
    </section>
  );
}
