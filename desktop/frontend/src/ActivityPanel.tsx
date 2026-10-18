import { useCallback, useEffect, useState } from "react";
import { api, type AuditPayload } from "./api";

export default function ActivityPanel() {
  const [audit, setAudit] = useState<AuditPayload | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const load = useCallback(async () => {
    try {
      setAudit(await api.audit());
      setError("");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load activity.");
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    let timer: number;
    const refresh = async () => {
      await load();
      if (!cancelled) timer = window.setTimeout(() => void refresh(), 5000);
    };
    void refresh();
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [load]);

  const entries = [...(audit?.entries ?? [])].reverse().filter((entry) =>
    `${entry.event} ${entry.action ?? ""} ${entry.decision ?? ""} ${entry.reason ?? ""}`.toLowerCase().includes(filter.toLowerCase()),
  );

  return (
    <section className="activity-panel">
      <header className="permissions-header">
        <div><h1>Activity</h1><p>Recent action decisions and operator changes. Updates every five seconds.</p></div>
        <button className="secondary" onClick={() => void load()}>Refresh activity</button>
      </header>
      {error && <p className="scope-warning" role="alert">{error}</p>}
      {!audit && !error && <p>Loading activity…</p>}
      {audit && !audit.enabled && <p className="hint">{audit.message}</p>}
      {audit?.enabled && <>
        <input className="activity-filter" aria-label="Filter activity" placeholder="Filter actions, decisions, or reasons…" value={filter} onChange={(event) => setFilter(event.target.value)} />
        {entries.length === 0 ? <p className="hint">{filter ? "No matching activity." : "No activity recorded yet. Actions and operator changes will appear here."}</p> :
          <div className="activity-table-wrap"><table className="activity-table">
            <thead><tr><th>Time</th><th>Action / event</th><th>Result</th><th>Details</th></tr></thead>
            <tbody>{entries.map((entry, index) => <tr key={`${entry.time}-${index}`}>
              <td><time dateTime={entry.time}>{new Date(entry.time).toLocaleString()}</time></td>
              <td><code>{entry.action || entry.event}</code></td>
              <td>{entry.decision || (entry.paused === true ? "Paused" : entry.paused === false ? "Resumed" : "—")}</td>
              <td>{entry.reason || entry.source || "—"}</td>
            </tr>)}</tbody>
          </table></div>}
        {audit.truncated && <p className="hint">Showing the latest 100 entries.</p>}
      </>}
    </section>
  );
}
