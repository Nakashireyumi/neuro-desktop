import { useCallback, useEffect, useMemo, useState } from "react";
import ExtensionPolicyEditor from "./ExtensionPolicyEditor";
import { api, safeExternalURL, type MarketplacePayload } from "./api";

export default function MarketplacePanel({ operator }: { operator: boolean }) {
  const [payload, setPayload] = useState<MarketplacePayload | null>(null);
  const [selectedId, setSelectedId] = useState("");
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const [staging, setStaging] = useState(false);
  const [policyId, setPolicyId] = useState("");

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError("");
    try {
      const next = await api.marketplace(signal);
      if (signal?.aborted) return;
      setPayload(next);
      setSelectedId((current) => next.items.some((item) => item.id === current) ? current : next.items[0]?.id ?? "");
    } catch (failure) {
      if (!signal?.aborted) setError(failure instanceof Error ? failure.message : "Could not read the marketplace.");
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (payload?.items ?? []).filter((item) => !needle || [item.id, item.name, item.description, ...item.game_ids].join(" ").toLowerCase().includes(needle));
  }, [payload, query]);
  const selected = filtered.find((item) => item.id === selectedId) ?? filtered[0];
  useEffect(() => setPolicyId(""), [selected?.id]);

  async function stageSource(id: string) {
    setStaging(true);
    setError("");
    setNotice("");
    try {
      const result = await api.stageMarketplaceSource(id);
      setNotice(result.message || "Pinned source downloaded. Follow the upstream setup instructions before running it.");
      await refresh();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not download this source.");
    } finally {
      setStaging(false);
    }
  }

  return (
    <main className="manager-main manager-main--extensions">
      <aside className="manager-sidebar">
        <h2>Marketplace</h2>
        <label htmlFor="marketplace-search">Find an integration or game</label>
        <input id="marketplace-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} style={{ width: "100%", boxSizing: "border-box", margin: "0.6rem 0" }} />
        {filtered.map((item) => <button key={item.id} className={`sidebar-item ${selected?.id === item.id ? "selected" : ""}`}
          onClick={() => { setSelectedId(item.id); setPolicyId(""); setNotice(""); }}>
          <strong>{item.name}</strong><small>{item.game_ids.join(", ") || item.kind.replace(/-/g, " ")}</small>
        </button>)}
        {!filtered.length && <p className="hint">{loading ? "Reading the index…" : "No matching source listings."}</p>}
      </aside>
      <section className="manager-panel">
        <div className="panel-header"><div><h1>Extension marketplace</h1><p>Choose dedicated integrations for your games and apps. Source downloads require upstream setup.</p></div>
          <button className="secondary" onClick={() => void refresh()} disabled={loading || staging}>{loading ? "Refreshing…" : "Refresh"}</button></div>
        {error && <p className="scope-warning" role="alert">{error}</p>}
        {notice && <p className="save-notice" role="status">{notice}</p>}
        {selected && <article style={{ marginTop: "1.5rem" }}>
          <h2>{selected.name}</h2><p>{selected.description}</p>
          <div className="status-row">
            <span className={`pill ${selected.source_staged ? "ok" : "neutral"}`}>{selected.source_staged ? "Source downloaded" : "Upstream source"}</span>
            <span className={`pill ${selected.can_stage_source ? "neutral" : "warn"}`}>{selected.install.readiness.replace(/-/g, " ")}</span>
            <span className="pill neutral">{selected.license.spdx} · {selected.license.status}</span>
          </div>
          <p><strong>Game profiles:</strong> {selected.game_ids.join(", ") || "General application integration"}</p>
          <p><strong>Reviewed revision:</strong> <code style={{ overflowWrap: "anywhere" }}>{selected.source_commit}</code> ({selected.source_ref})</p>
          <p className="hint">{selected.license.notes}</p>
          <h3>Prerequisites</h3><ul>{selected.install.requirements.map((requirement) => <li key={requirement}>{requirement}</li>)}</ul>
          <ul>{selected.install.notes.map((note) => <li key={note}>{note}</li>)}</ul>
          <p><strong>Documented platforms:</strong> {selected.platforms.documented.join(", ") || "Not documented"}. <strong>Verified here:</strong> {selected.platforms.tested.join(", ") || "None yet"}.</p>
          <p className="hint">{selected.platforms.notes}</p>
          <div className="button-row">
            <button className="primary" disabled={staging || !selected.can_stage_source || selected.source_staged} onClick={() => void stageSource(selected.id)}>{staging ? "Downloading source…" : selected.source_staged ? "Source already downloaded" : "Download pinned source"}</button>
            {safeExternalURL(selected.install.instructions_url) && <a className="secondary" href={safeExternalURL(selected.install.instructions_url)} target="_blank" rel="noreferrer">Upstream setup ↗</a>}
            {safeExternalURL(selected.repository) && <a href={safeExternalURL(selected.repository)} target="_blank" rel="noreferrer">Repository ↗</a>}
            {operator && <button className="secondary" onClick={() => setPolicyId(policyId === selected.id ? "" : selected.id)}>Permissions and settings</button>}
          </div>
          <p className="hint">Downloading needs the global Extensions permission and this item's management grant. A source download is not an installed or running integration.</p>
          {!selected.can_stage_source && <p className="scope-warning">Managed source download is unavailable while the license needs review. You can inspect the upstream repository.</p>}
          <h3>Capabilities to review</h3><ul>{selected.permissions.capabilities.map((permission) => <li key={permission.id}><strong>{permission.id}:</strong> {permission.reason}</li>)}</ul>
          <p className="hint">These are source review notes. Separately installed programs enforce their own permissions.</p>
        </article>}
        {operator && policyId && <ExtensionPolicyEditor key={policyId} id={policyId} onClose={() => setPolicyId("")} onSaved={() => void refresh()} />}
        <section style={{ marginTop: "2rem", borderTop: "1px solid var(--line)", paddingTop: "1rem" }}>
          <h2>Your configured integrations</h2>
          <p className="hint">SDK adapters keep their own Neuro API actions and connect to the game profiles shown here. Refresh to read their latest state.</p>
          {(payload?.integrations ?? []).map((integration) => <article key={integration.id} style={{ marginBottom: "1.2rem" }}>
            <h3>{integration.manifest.name} <small>{integration.manifest.version}</small></h3>
            <p><span className={`pill ${integration.connected ? "ok" : "warn"}`}>{integration.state}</span> · {integration.manifest.game_ids?.join(", ") || "No game profile"}</p>
            {integration.manifest.neuro_api?.game_name && <p>Neuro API name: {integration.manifest.neuro_api.game_name}</p>}
            {integration.message && <p>{integration.message}</p>}
            {integration.last_seen && <p className="hint">Last seen: {new Date(integration.last_seen).toLocaleString()}</p>}
            {operator && <button className="secondary" onClick={() => setPolicyId(policyId === integration.id ? "" : integration.id)}>Permissions and settings</button>}
          </article>)}
          {payload && !payload.integrations?.length && <p>No SDK adapters are configured yet. Import a local manifest when you are ready to connect your existing setup.</p>}
        </section>
      </section>
    </main>
  );
}
