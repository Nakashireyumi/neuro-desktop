package main

// Dedicated integrations keep their existing Neuro API connection. This adapter
// supplies a scoped presence lease and an opt-in, curated viewer feed, without
// exposing the operator token or pretending to sandbox an external process.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const adapterLease = 45 * time.Second

type IntegrationManifest struct {
	SchemaVersion  int                              `json:"schema_version"`
	ID             string                           `json:"id"`
	Name           string                           `json:"name"`
	Version        string                           `json:"version"`
	Kind           string                           `json:"kind"`
	Permissions    []ExtensionPermissionDeclaration `json:"permissions"`
	SettingsSchema map[string]interface{}           `json:"settings_schema"`
	GameIDs        []string                         `json:"game_ids"`
	NeuroAPI       struct {
		GameName string `json:"game_name,omitempty"`
	} `json:"neuro_api"`
}

func (m *IntegrationManifest) normalise() {
	if m.Permissions == nil {
		m.Permissions = []ExtensionPermissionDeclaration{}
	}
	if m.GameIDs == nil {
		m.GameIDs = []string{}
	}
	if m.SettingsSchema == nil {
		m.SettingsSchema = map[string]interface{}{}
	}
}

func (m IntegrationManifest) validate() error {
	if m.SchemaVersion != 1 || !extensionIDPattern.MatchString(m.ID) || strings.TrimSpace(m.Name) == "" || !cleanActivityText(m.Name, 120) || m.Version == "" || !cleanActivityText(m.Version, 64) {
		return errors.New("manifest needs schema_version 1, a plain id, name and version")
	}
	switch m.Kind {
	case "integration", "extension", "game", "discord":
	default:
		return errors.New("unsupported integration kind")
	}
	if len(m.Permissions) > 32 || len(m.GameIDs) > 32 || !cleanActivityText(m.NeuroAPI.GameName, 120) {
		return errors.New("manifest exceeds its declaration limits")
	}
	seen := map[string]bool{}
	for _, p := range m.Permissions {
		if !extensionIDPattern.MatchString(p.ID) || seen[p.ID] || !cleanActivityText(p.Description, 500) {
			return errors.New("permission declarations need unique plain ids and bounded descriptions")
		}
		seen[p.ID] = true
	}
	for _, id := range m.GameIDs {
		if !extensionIDPattern.MatchString(id) {
			return errors.New("game_ids must be plain ids")
		}
	}
	return validateExtensionDeclarations(m.Permissions)
}

type adapterConfig struct {
	Manifest   IntegrationManifest `json:"manifest"`
	TokenHash  string              `json:"token_hash,omitempty"`
	ViewerHash string              `json:"viewer_hash,omitempty"`
}
type adapterConfigFile struct {
	Version      int                      `json:"version"`
	Integrations map[string]adapterConfig `json:"integrations"`
}
type viewerActivity struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
}
type adapterPresence struct {
	State        string           `json:"state"`
	Message      string           `json:"message,omitempty"`
	LastSeen     time.Time        `json:"last_seen"`
	VisibleUntil time.Time        `json:"visible_until,omitempty"`
	Actor        string           `json:"actor,omitempty"`
	Items        []viewerActivity `json:"-"`
	NextSeq      uint64           `json:"-"`
	LastEvent    time.Time        `json:"-"`
}
type adapterRuntime struct {
	mu       sync.Mutex
	presence map[string]*adapterPresence
}

func (n *NDIntegration) adapterRuntime() *adapterRuntime {
	n.adaptersMu.Lock()
	defer n.adaptersMu.Unlock()
	if n.adapters == nil {
		n.adapters = &adapterRuntime{presence: map[string]*adapterPresence{}}
	}
	return n.adapters
}

var adapterConfigMu sync.Mutex

func adapterConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("NEURO_INTEGRATIONS_FILE")); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(extensionStatePath()), "integrations.json")
}
func loadAdapterConfig() (adapterConfigFile, error) {
	f := adapterConfigFile{Version: 1, Integrations: map[string]adapterConfig{}}
	data, err := os.ReadFile(adapterConfigPath())
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if len(data) > 2<<20 {
		return f, errors.New("integration configuration is too large")
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, errors.New("invalid integration configuration")
	}
	if f.Version != 1 || f.Integrations == nil {
		return f, errors.New("unsupported integration configuration")
	}
	for id, cfg := range f.Integrations {
		cfg.Manifest.normalise()
		if cfg.Manifest.ID != id || cfg.Manifest.validate() != nil {
			return f, errors.New("invalid configured integration manifest")
		}
		f.Integrations[id] = cfg
	}
	return f, nil
}
func saveAdapterConfig(f adapterConfigFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("integration configuration exceeds 2 MiB")
	}
	return atomicWriteFile(adapterConfigPath(), append(data, '\n'))
}
func (n *NDIntegration) adapterDeclarations(id string) ([]ExtensionPermissionDeclaration, map[string]interface{}, error) {
	f, err := loadAdapterConfig()
	if err != nil {
		return nil, nil, err
	}
	cfg, ok := f.Integrations[id]
	if !ok {
		return nil, nil, errors.New("integration not configured")
	}
	return cfg.Manifest.Permissions, cfg.Manifest.SettingsSchema, nil
}
func decodeAdapterBody(r *http.Request, out interface{}) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return errors.New("request exceeds 64 KiB")
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		data = []byte("{}")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}
func adapterTokenMatches(token, encoded string) bool {
	if token == "" || encoded == "" {
		return false
	}
	want, err := hex.DecodeString(encoded)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}
func newAdapterToken() (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}
func cleanActivityText(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func (n *NDIntegration) adapterSnapshot() []map[string]interface{} {
	f, err := loadAdapterConfig()
	if err != nil {
		return []map[string]interface{}{}
	}
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	ids := make([]string, 0, len(f.Integrations))
	for id := range f.Integrations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	views := []map[string]interface{}{}
	for _, id := range ids {
		cfg := f.Integrations[id]
		p := rt.presence[id]
		view := map[string]interface{}{"id": id, "manifest": cfg.Manifest, "connected": false, "state": "offline", "token_configured": cfg.TokenHash != "", "viewer_token_configured": cfg.ViewerHash != ""}
		if p != nil {
			live := p.State != "offline" && time.Since(p.LastSeen) < adapterLease && n.extensionRuntimeAllowed(id, cfg.Manifest.Permissions) == nil
			view["connected"], view["state"], view["last_seen"], view["message"] = live, p.State, p.LastSeen, p.Message
			if !live {
				view["state"] = "offline"
			}
			view["viewer_visible"] = live && time.Now().Before(p.VisibleUntil)
		}
		views = append(views, view)
	}
	return views
}
func (n *NDIntegration) adapterOwnsGame(profile *GameProfile) bool {
	for _, view := range n.adapterSnapshot() {
		if view["connected"] != true {
			continue
		}
		m := view["manifest"].(IntegrationManifest)
		for _, id := range m.GameIDs {
			if id == profile.ID {
				return true
			}
		}
		owner := strings.TrimSpace(profile.Control.ExternalName)
		if owner != "" && (strings.EqualFold(owner, m.ID) || strings.EqualFold(owner, m.NeuroAPI.GameName)) {
			return true
		}
	}
	return false
}

func (a *AdminServer) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	a.guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, 405, "method not allowed")
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "integrations": a.integration.adapterSnapshot(), "lease_seconds": 45})
	})(w, r)
}
func (a *AdminServer) handleIntegrationAdapter(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/integrations/"), "/")
	if len(parts) > 2 || !extensionIDPattern.MatchString(parts[0]) {
		writeError(w, 400, "invalid integration route")
		return
	}
	id, action := parts[0], ""
	if len(parts) == 2 {
		action = parts[1]
	}
	if action == "heartbeat" || action == "activity" || action == "hide" || action == "config" {
		a.handleScopedAdapter(w, r, id, action)
		return
	}
	a.guard(func(w http.ResponseWriter, r *http.Request) {
		// Importing manifests and minting credentials are always operator-only.
		if !a.credential.matches(strings.TrimSpace(r.Header.Get("X-ND-Operator-Token"))) {
			writeError(w, 403, "Re-enter your operator token to configure an integration.")
			return
		}
		adapterConfigMu.Lock()
		defer adapterConfigMu.Unlock()
		f, err := loadAdapterConfig()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		cfg, exists := f.Integrations[id]
		if action == "" && r.Method == http.MethodPut {
			if !exists && len(f.Integrations) >= 128 {
				writeError(w, 409, "at most 128 integrations can be configured")
				return
			}
			var body struct {
				Manifest IntegrationManifest `json:"manifest"`
			}
			if err := decodeAdapterBody(r, &body); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			if body.Manifest.ID != id {
				writeError(w, 400, "manifest id must match the route")
				return
			}
			body.Manifest.normalise()
			if err := body.Manifest.validate(); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			if index, err := loadCatalogIndex(catalogFilePath()); err == nil && findCatalogItemByID(index, id) != nil {
				writeError(w, 409, "this id belongs to a managed catalog extension; choose another integration id")
				return
			}
			cfg.Manifest = body.Manifest
			f.Integrations[id] = cfg
			if err := saveAdapterConfig(f); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			a.writeAudit("integration manifest configured: "+id, false)
			writeJSON(w, map[string]interface{}{"ok": true, "id": id, "message": "Integration registered. Approve its per-extension permissions before connecting it."})
			return
		}
		if !exists {
			writeError(w, 404, "integration not configured")
			return
		}
		if action == "visibility" && r.Method == http.MethodPost {
			var body struct {
				Visible bool   `json:"visible"`
				Actor   string `json:"actor"`
				Minutes int    `json:"minutes"`
			}
			if err := decodeAdapterBody(r, &body); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			if err := a.integration.setViewerVisibility(id, body.Actor, body.Visible, body.Minutes); err != nil {
				writeError(w, 409, err.Error())
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "visible": body.Visible})
			return
		}
		if action == "token" && r.Method == http.MethodPost {
			var body struct {
				Kind string `json:"kind"`
			}
			if err := decodeAdapterBody(r, &body); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			if body.Kind != "integration" && body.Kind != "viewer" {
				writeError(w, 400, "kind must be integration or viewer")
				return
			}
			token, hash, err := newAdapterToken()
			if err != nil {
				writeError(w, 500, "token generation failed")
				return
			}
			if body.Kind == "integration" {
				cfg.TokenHash = hash
			} else {
				cfg.ViewerHash = hash
			}
			f.Integrations[id] = cfg
			if err := saveAdapterConfig(f); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			a.integration.hideAdapter(id)
			if body.Kind == "integration" {
				rt := a.integration.adapterRuntime()
				rt.mu.Lock()
				delete(rt.presence, id)
				rt.mu.Unlock()
			}
			a.writeAudit("integration credential rotated: "+id+" "+body.Kind, false)
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, map[string]interface{}{"ok": true, "id": id, "kind": body.Kind, "token": token})
			return
		}
		if action == "" && r.Method == http.MethodDelete {
			delete(f.Integrations, id)
			if err := saveAdapterConfig(f); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			a.integration.hideAdapter(id)
			rt := a.integration.adapterRuntime()
			rt.mu.Lock()
			delete(rt.presence, id)
			rt.mu.Unlock()
			a.writeAudit("integration removed: "+id, false)
			writeJSON(w, map[string]interface{}{"ok": true})
			return
		}
		writeError(w, 405, "method not allowed")
	})(w, r)
}
func (n *NDIntegration) hideAdapter(id string) {
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if p := rt.presence[id]; p != nil {
		p.VisibleUntil = time.Time{}
		p.Items = nil
	}
}
func (a *AdminServer) handleScopedAdapter(w http.ResponseWriter, r *http.Request, id, action string) {
	if (action == "config" && r.Method != http.MethodGet) || (action != "config" && r.Method != http.MethodPost) {
		writeError(w, 405, "method not allowed")
		return
	}
	if r.Header.Get("Origin") != "" {
		writeError(w, 403, "integration credentials are for native adapters")
		return
	}
	f, err := loadAdapterConfig()
	if err != nil {
		writeError(w, 500, "cannot read integration configuration")
		return
	}
	cfg, ok := f.Integrations[id]
	if !ok || !adapterTokenMatches(r.Header.Get("X-ND-Integration-Token"), cfg.TokenHash) {
		writeError(w, 401, "invalid integration credential")
		return
	}
	if action == "hide" {
		a.integration.hideAdapter(id)
		writeJSON(w, map[string]interface{}{"ok": true})
		return
	}
	if err := a.integration.extensionRuntimeAllowed(id, cfg.Manifest.Permissions); err != nil {
		a.integration.hideAdapter(id)
		writeError(w, 403, err.Error())
		return
	}
	if reason := a.integration.stop.blockReason("integration_" + action); reason != "" {
		a.integration.hideAdapter(id)
		writeError(w, 409, reason)
		return
	}
	configuration, err := a.integration.adapterConfiguration(cfg)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if action == "config" {
		writeJSON(w, configuration)
		return
	}
	rt := a.integration.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	p := rt.presence[id]
	if p == nil {
		p = &adapterPresence{State: "offline"}
		rt.presence[id] = p
	}
	if action == "heartbeat" {
		var body struct {
			State   string `json:"state"`
			Message string `json:"message"`
		}
		if err := decodeAdapterBody(r, &body); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if (body.State != "ready" && body.State != "busy" && body.State != "offline") || !cleanActivityText(body.Message, 500) {
			writeError(w, 400, "invalid heartbeat state or message")
			return
		}
		if time.Since(p.LastSeen) >= adapterLease || body.State == "offline" {
			p.Items = nil
			p.VisibleUntil = time.Time{}
		}
		p.State, p.Message, p.LastSeen = body.State, body.Message, time.Now()
		configuration["lease_seconds"] = 45
		writeJSON(w, configuration)
		return
	}
	var body struct {
		Actor   string `json:"actor"`
		Kind    string `json:"kind"`
		Summary string `json:"summary"`
	}
	if err := decodeAdapterBody(r, &body); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if (body.Actor != "neuro" && body.Actor != "evil") || !cleanActivityText(body.Summary, 500) || strings.TrimSpace(body.Summary) == "" {
		writeError(w, 400, "invalid curated activity")
		return
	}
	switch body.Kind {
	case "status", "discord.channel", "discord.message", "discord.voice":
	default:
		writeError(w, 400, "unsupported activity kind")
		return
	}
	if !a.integration.viewerGrantAllowed(id, cfg.Manifest.Permissions) || p.State == "offline" || time.Since(p.LastSeen) >= adapterLease || !time.Now().Before(p.VisibleUntil) || p.Actor != body.Actor {
		writeError(w, 403, "Viewer visibility is off. Enable it for this actor before publishing.")
		return
	}
	if time.Since(p.LastEvent) < 250*time.Millisecond {
		writeError(w, 429, "activity rate limit")
		return
	}
	p.NextSeq++
	p.LastEvent = time.Now()
	p.Items = append(p.Items, viewerActivity{p.NextSeq, p.LastEvent, body.Actor, body.Kind, body.Summary})
	if len(p.Items) > 50 {
		p.Items = append([]viewerActivity(nil), p.Items[len(p.Items)-50:]...)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "seq": p.NextSeq})
}

func (n *NDIntegration) adapterConfiguration(cfg adapterConfig) (map[string]interface{}, error) {
	policy, _, err := n.extensionPermissionSnapshot(cfg.Manifest.ID)
	if err != nil {
		return nil, err
	}
	grants := map[string]bool{}
	global := n.policy()
	for _, declaration := range cfg.Manifest.Permissions {
		allowed := policy.Grants[declaration.ID]
		if _, known := scopeDescriptions[PermissionScope(declaration.ID)]; known {
			allowed = allowed && global != nil && global.ScopeAllowed(PermissionScope(declaration.ID))
		}
		grants[declaration.ID] = allowed
	}
	return map[string]interface{}{"ok": true, "id": cfg.Manifest.ID, "manifest": cfg.Manifest, "settings": policy.Settings, "grants": grants, "run_allowed": policy.RunAllowed}, nil
}
func (n *NDIntegration) viewerGrantAllowed(id string, decls []ExtensionPermissionDeclaration) bool {
	declared := false
	for _, d := range decls {
		if d.ID == "discord.viewer" {
			declared = true
		}
	}
	if !declared {
		return false
	}
	// Requiring this declaration for the visibility check also handles an
	// optional discord.viewer declaration that has not been approved.
	required := append([]ExtensionPermissionDeclaration(nil), decls...)
	for i := range required {
		if required[i].ID == "discord.viewer" {
			required[i].Required = true
		}
	}
	return n.extensionRuntimeAllowed(id, required) == nil
}
func (n *NDIntegration) setViewerVisibility(id, actor string, visible bool, minutes int) error {
	if _, err := normalizeExtensionID(id); err != nil {
		return err
	}
	if !visible {
		n.hideAdapter(id)
		return nil
	}
	if actor != "neuro" && actor != "evil" {
		return errors.New("actor must be neuro or evil")
	}
	if minutes < 1 || minutes > 60 {
		return errors.New("visibility duration must be between 1 and 60 minutes")
	}
	f, err := loadAdapterConfig()
	if err != nil {
		return err
	}
	cfg, ok := f.Integrations[id]
	if !ok {
		return errors.New("integration is not configured")
	}
	if !n.viewerGrantAllowed(id, cfg.Manifest.Permissions) {
		return errors.New("Viewer visibility needs your explicit discord.viewer approval for this integration.")
	}
	rt := n.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	p := rt.presence[id]
	if p == nil || p.State == "offline" || time.Since(p.LastSeen) >= adapterLease {
		return errors.New("integration is offline; connect it before enabling viewer visibility")
	}
	p.Items = nil
	p.Actor = actor
	p.VisibleUntil = time.Now().Add(time.Duration(minutes) * time.Minute)
	n.audit.record("viewer_visibility", map[string]interface{}{"integration": id, "visible": true, "actor": actor, "minutes": minutes})
	return nil
}
func (a *AdminServer) handleViewerFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/viewer/")
	if !extensionIDPattern.MatchString(id) {
		writeError(w, 400, "invalid integration id")
		return
	}
	f, err := loadAdapterConfig()
	if err != nil {
		writeError(w, 500, "cannot read integration configuration")
		return
	}
	cfg, ok := f.Integrations[id]
	if !ok || !adapterTokenMatches(r.Header.Get("X-ND-Viewer-Token"), cfg.ViewerHash) {
		writeError(w, 401, "invalid viewer credential")
		return
	}
	rt := a.integration.adapterRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	p := rt.presence[id]
	visible := p != nil && p.State != "offline" && time.Since(p.LastSeen) < adapterLease && time.Now().Before(p.VisibleUntil) && a.integration.viewerGrantAllowed(id, cfg.Manifest.Permissions) && a.integration.stop.blockReason("viewer_feed") == ""
	items := []viewerActivity{}
	actor := ""
	if visible {
		items = append(items, p.Items...)
		actor = p.Actor
	} else if p != nil {
		p.Items = nil
		p.VisibleUntil = time.Time{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "visible": visible, "actor": actor, "items": items})
}

func (a *AdminServer) handleViewerOverlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/viewer/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+viewerScriptHash()+"'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'self'")
	_, _ = fmt.Fprint(w, viewerOverlayHTML())
}
