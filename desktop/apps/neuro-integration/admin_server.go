package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

// AdminServer exposes a small HTTP API for Vedal / operators:
// status, permissions get/set, connected executor info.
type AdminServer struct {
	integration *NDIntegration
	addr        string
	mu          sync.Mutex
}

func NewAdminServer(integration *NDIntegration, addr string) *AdminServer {
	if addr == "" {
		addr = "127.0.0.1:8300"
	}
	return &AdminServer{integration: integration, addr: addr}
}

func (a *AdminServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/permissions", a.handlePermissions)
	mux.HandleFunc("/", a.handleUI)

	go func() {
		log.Printf("Admin dashboard API on http://%s/", a.addr)
		if err := http.ListenAndServe(a.addr, mux); err != nil {
			log.Printf("Admin server stopped: %v", err)
		}
	}()
	return nil
}

func (a *AdminServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
}

func (a *AdminServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	execConnected := false
	if a.integration.executorHub != nil {
		execConnected = a.integration.executorHub.HasClient()
	}
	writeJSON(w, map[string]any{
		"ok":                 true,
		"game":               "Neuro Desktop",
		"executor_connected": execConnected,
		"permissions_path":   a.integration.permissionsPath,
	})
}

func (a *AdminServer) handlePermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path := a.integration.permissionsPath
		data, err := os.ReadFile(path)
		if err != nil {
			// Return in-memory defaults as JSON shape
			writeJSON(w, map[string]any{
				"default_allow":    a.integration.permissions.DefaultAllow,
				"note":             "file missing; showing runtime defaults",
				"permissions_path": path,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	case http.MethodPut, http.MethodPost:
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		path := a.integration.permissionsPath
		if err := os.WriteFile(path, body, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		policy, err := loadPermissionPolicy(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.integration.permissions = policy
		writeJSON(w, map[string]any{"ok": true, "saved": path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *AdminServer) handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Neuro Desktop Admin</title>
<style>body{font-family:system-ui;max-width:720px;margin:2rem auto;padding:0 1rem}
pre{background:#111;color:#d6ffd6;padding:1rem;overflow:auto}</style></head>
<body>
<h1>Neuro Desktop — Operator</h1>
<p>Bridge status &amp; permissions. Full UI lives in <code>desktop/frontend</code>; export policies here or via PUT <code>/api/permissions</code>.</p>
<pre id="s">loading…</pre>
<script>
async function refresh(){
  const r=await fetch('/api/status');
  document.getElementById('s').textContent=JSON.stringify(await r.json(),null,2);
}
refresh(); setInterval(refresh,3000);
</script>
</body></html>`)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
