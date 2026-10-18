package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// AdminServer is Vedal's operator surface: bridge status, live permission
// policy, the plugin (extension) manager, the game registry, and the relay
// link. It also serves the compiled dashboard, so the UI is same-origin with
// the API instead of being a file:// page that cannot call the bridge.
//
// Every /api route except /health needs the dashboard token. Nothing is trusted
// because of where the request comes from (see guard).
type AdminServer struct {
	integration *NDIntegration
	addr        string
	credential  dashboardCredential
	delegation  delegatedManagerStore
	// credentialProblem says why the dashboard token could not be loaded (for
	// example, NEURO_ADMIN_TOKEN disagrees with the stored one). The 503 answer
	// shows it, so the sign-in page can say what to fix.
	credentialProblem string

	mu       sync.Mutex
	listener net.Listener
	requests int
	lastErr  string
}

func NewAdminServer(integration *NDIntegration, addr string, credential dashboardCredential) *AdminServer {
	if addr == "" {
		addr = "127.0.0.1:8300"
	}
	return &AdminServer{integration: integration, addr: addr, credential: credential}
}

// routes wires the dashboard API. It is separate from Start so tests can serve
// the same handlers with httptest.
func (a *AdminServer) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Public: liveness and the version only. Every other /api route is guarded.
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/api/session", a.guard(a.handleSession))
	mux.HandleFunc("/api/delegation/token", a.guard(a.handleDelegationToken))
	mux.HandleFunc("/api/delegation/session", a.guard(a.handleDelegatedSession))
	mux.HandleFunc("/api/status", a.guard(a.handleStatus))
	mux.HandleFunc("/api/runtime", a.guard(a.handleRuntime))
	mux.HandleFunc("/api/permissions", a.guard(a.handlePermissions))
	mux.HandleFunc("/api/permissions/schema", a.guard(a.handlePermissionSchema))
	mux.HandleFunc("/api/actions", a.guard(a.handleActions))
	mux.HandleFunc("/api/catalog", a.guard(a.handleCatalog))
	mux.HandleFunc("/api/extensions", a.guard(a.handleExtensions))
	mux.HandleFunc("/api/services/vision", a.guard(a.handleVisionService))
	mux.HandleFunc("/api/extensions/", a.guard(a.handleExtensionAction))
	mux.HandleFunc("/api/integrations", a.handleIntegrations)
	mux.HandleFunc("/api/integrations/", a.handleIntegrationAdapter)
	mux.HandleFunc("/api/marketplace", a.guard(a.handleMarketplace))
	mux.HandleFunc("/api/marketplace/", a.guard(a.handleMarketplaceAction))
	mux.HandleFunc("/api/viewer/", a.handleViewerFeed)
	mux.HandleFunc("/viewer/", a.handleViewerOverlay)
	mux.HandleFunc("/api/permission-requests", a.guard(a.handlePermissionRequests))
	mux.HandleFunc("/api/permission-requests/", a.guard(a.handlePermissionRequestAction))
	mux.HandleFunc("/api/permissions/grants/", a.guard(a.handlePermissionGrant))
	mux.HandleFunc("/api/games", a.guard(a.handleGames))
	mux.HandleFunc("/api/games/session", a.guard(a.handleGameSession))
	mux.HandleFunc("/api/games/release", a.guard(a.handleGameRelease))
	mux.HandleFunc("/api/games/observe", a.guard(a.handleGameObserve))
	mux.HandleFunc("/api/relay", a.guard(a.handleRelay))
	// Operator brake: pause/resume and the kill switch, plus the audit tail.
	mux.HandleFunc("/api/control", a.guard(a.handleControl))
	mux.HandleFunc("/api/control/pause", a.guard(a.handleControlPause))
	mux.HandleFunc("/api/control/resume", a.guard(a.handleControlResume))
	mux.HandleFunc("/api/audit", a.guard(a.handleAudit))
	mux.HandleFunc("/api/config", a.guard(a.handleConfig))
	mux.HandleFunc("/", a.handleUI)
	return mux
}

// Addr returns the address the dashboard is actually listening on, which
// differs from the configured one when port 0 was requested.
func (a *AdminServer) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.listener != nil {
		return a.listener.Addr().String()
	}
	return a.addr
}

func (a *AdminServer) Start() error {
	listener, err := net.Listen("tcp", a.addr)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.listener = listener
	a.mu.Unlock()

	if !a.credential.configured() {
		log.Printf("WARNING: the dashboard has no token; every API call answers 503 until you run `neuro-integration setup`")
	}

	log.Printf("Operator dashboard on http://%s/ui/", a.Addr())

	go func() {
		server := &http.Server{
			Handler:           a.routes(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      150 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    16 << 10,
		}
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Admin server stopped: %v", err)
		}
	}()

	return nil
}

// guard admits a request only when it carries the dashboard token. There is no
// exception for loopback. A local process, a browser on this machine, or a proxy
// that connects from 127.0.0.1 (a live preview, for example) is checked the same
// way as anyone else. A dashboard that is not set up refuses with 503.
func (a *AdminServer) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.credential.configured() {
			message := dashboardNotSetUpMessage
			if a.credentialProblem != "" {
				message = a.credentialProblem
			}
			writeError(w, http.StatusServiceUnavailable, message)
			return
		}
		if recognized, err := a.authorizeDelegated(r); recognized {
			if err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			next(w, r)
			return
		}
		if !a.credential.matches(requestToken(r)) {
			a.writeAudit("rejected: bad dashboard token", true)
			a.integration.audit.record("dashboard_auth", map[string]interface{}{
				"decision": "denied", "reason": "invalid or missing dashboard token",
				"method": r.Method, "path": r.URL.Path,
			})
			writeError(w, http.StatusUnauthorized, "invalid or missing dashboard token")
			return
		}
		next(w, r)
	}
}

// requestToken reads the token from X-ND-Token or an Authorization: Bearer
// header. A query parameter is deliberately not read: URLs end up in logs,
// history, and referrers.
func requestToken(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get("X-ND-Token")); token != "" {
		return token
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "bearer "
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		return strings.TrimSpace(header[len(bearer):])
	}
	return ""
}

// handleSession lets the dashboard check a token before it shows anything else.
func (a *AdminServer) handleSession(w http.ResponseWriter, r *http.Request) {
	role, expiresAt := a.managerSessionRole(r)
	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"version":      Version,
		"token_source": a.credential.source,
		"role":         role,
		"expires_at":   expiresAt,
	})
}

func (a *AdminServer) writeAudit(message string, isError bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests++
	if isError {
		a.lastErr = message
	}
}

func (a *AdminServer) audits() (int, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests, a.lastErr
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": message})
}

func (a *AdminServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "version": Version})
}

func (a *AdminServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	integration := a.integration

	executor := ExecutorInfo{}
	if integration.executorHub != nil {
		executor = integration.executorHub.Info()
	}

	session := integration.games.Session()
	var sessionPayload interface{}
	if session != nil {
		sessionPayload = session
	}

	requests, lastErr := a.audits()

	status := map[string]interface{}{
		"ok":          true,
		"version":     Version,
		"integration": "Neuro Desktop",
		"uptime":      int(time.Since(integration.startedAt).Seconds()),
		"started_at":  integration.startedAt.UTC().Format(time.RFC3339),
		"executor":    executor,
		"neuro":       integration.neuroConnection.snapshot(),
		"negotiation": map[string]interface{}{
			"executor_connected": executor.Connected,
			"file_ipc_path":      integration.ipcFilePath,
			"protocol_version":   bridgeProtocolVersion,
		},
		"permissions_path": integration.permissionsPath,
		"game": map[string]interface{}{
			"registry_source": integration.games.RegistrySource(),
			"profiles":        len(integration.games.Profiles()),
			"session":         sessionPayload,
		},
		"relay":   integration.relay.status(),
		"actions": integration.stats.snapshot(),
		"control": integration.stop.status(),
		"audit":   integration.audit.status(),
		"safety":  integration.safetySnapshot(),
		"admin": map[string]interface{}{
			"listen":           a.addr,
			"token_required":   true,
			"token_configured": a.credential.configured(),
			"token_source":     a.credential.source,
			"requests":         requests,
			"last_error":       lastErr,
		},
	}

	writeJSON(w, status)
}

// handlePermissionSchema describes the policy model so the dashboard does not
// have to hard-code scopes and action names.
func (a *AdminServer) handlePermissionSchema(w http.ResponseWriter, _ *http.Request) {
	actions := make([]map[string]interface{}, 0)
	for _, spec := range allActionSpecs() {
		action := map[string]interface{}{
			"name":        string(spec.Name),
			"description": spec.Description,
			"scope":       actionScopeName(string(spec.Name)),
			"kind":        spec.Kind,
		}
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i, j int) bool {
		return actions[i]["name"].(string) < actions[j]["name"].(string)
	})

	policy := a.integration.policy()
	scopes := []map[string]interface{}{}
	for _, scope := range allScopes() {
		scopes = append(scopes, map[string]interface{}{
			"name":             string(scope),
			"default":          defaultPermissionPolicy().scopes[scope].Allowed,
			"description":      scopeDescriptions[scope],
			"explicit_consent": scopeRequiresExplicitConsent(scope),
			"requestable":      policy.ScopeRequestable(scope),
		})
	}

	effective := map[string]bool{}
	for _, spec := range allActionSpecs() {
		effective[string(spec.Name)] = policy == nil || policy.IsAllowed(string(spec.Name))
	}

	writeJSON(w, map[string]interface{}{
		"scopes":    scopes,
		"actions":   actions,
		"effective": effective,
		"defaults": map[string]interface{}{
			"default_allow": policy != nil && policy.DefaultAllow,
		},
	})
}

func allActionSpecs() []actionSpec {
	specs := make([]actionSpec, 0, 64)
	specs = append(specs, HLActionSpecs...)
	specs = append(specs, LLActionSpecs...)
	specs = append(specs, gameActionSpecs()...)
	// The shell and the self-documentation guide are part of the surface the
	// dashboard can grant, so they must show up here too.
	specs = append(specs, alwaysRegisteredSpecs()...)
	return specs
}

func allScopes() []PermissionScope {
	return []PermissionScope{
		ScopeInput, ScopeGame, ScopeShell, ScopeFilesystem, ScopeProcess, ScopeNetwork, ScopeSystem, ScopeVision, ScopeExtensions,
	}
}

// actionScopeName names the switch an action sits behind. "always" marks the
// actions that no switch can turn off (request_permission): the operator
// controls them through the deny list and NEURO_DENY_ACTIONS instead.
func actionScopeName(action string) string {
	if alwaysAllowed[action] {
		return "always"
	}
	if scope, ok := scopeForAction(action); ok {
		return string(scope)
	}
	return "unknown"
}

// describeSchema renders an action schema as one line, e.g.
// `direction (string, required: forward|back|left|right), seconds (number)`.
// It is deliberately plain text: the dashboard shows it, and it is what a
// small model can be handed without teaching it JSON Schema.
func describeSchema(schema *neuro.ActionSchema) string {
	if schema == nil || len(schema.Properties) == 0 {
		return "none"
	}

	required := make(map[string]bool, len(schema.Required))
	for _, key := range schema.Required {
		required[key] = true
	}

	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		property, _ := schema.Properties[key].(map[string]interface{})
		typeName, _ := property["type"].(string)
		if typeName == "" {
			typeName = "value"
		}

		description, _ := property["description"].(string)
		piece := key + " (" + typeName
		if required[key] {
			piece += ", required"
		}
		if values := enumValues(property["enum"]); len(values) > 0 {
			piece += ": " + strings.Join(values, "|")
		}
		if description != "" {
			piece += " — " + description
		}
		piece += ")"
		parts = append(parts, piece)
	}
	return strings.Join(parts, ", ")
}

// enumValues renders an "enum" entry as strings, ignoring other shapes.
func enumValues(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		if strings, ok := raw.([]string); ok {
			return strings
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func (a *AdminServer) handleActions(w http.ResponseWriter, _ *http.Request) {
	policy := a.integration.policy()

	// The dashboard edits permissions before Neuro has connected, so list every
	// action this build can register and mark the live ones, instead of only
	// reporting what has already been sent to Neuro.
	registered := a.integration.registeredActionNames()
	isRegistered := make(map[string]bool, len(registered))
	for _, name := range registered {
		isRegistered[name] = true
	}

	reserved := a.integration.reservedActionList()
	isReserved := make(map[string]bool, len(reserved))
	for _, name := range reserved {
		isReserved[name] = true
	}

	out := make([]map[string]interface{}, 0, len(allActionSpecs()))
	for _, spec := range allActionSpecs() {
		name := string(spec.Name)
		entry := map[string]interface{}{
			"name":        name,
			"scope":       actionScopeName(name),
			"description": spec.Description,
			"allowed":     policy == nil || policy.IsAllowed(name),
			"registered":  isRegistered[name],
			"reserved":    isReserved[name],
		}
		if spec.Kind == actionKindGame {
			entry["kind"] = "game"
		}
		// Expose the parameter shape. A weak model (or the operator writing a
		// prompt) otherwise has no way to know what "game_move" expects until a
		// call fails. `params` is the human/LLM-readable rendering of the same
		// schema that is registered with Neuro.
		if spec.Schema != nil {
			entry["schema"] = spec.Schema
			entry["params"] = describeSchema(spec.Schema)
			entry["required"] = append([]string{}, spec.Schema.Required...)
		} else {
			entry["params"] = "none"
		}
		out = append(out, entry)
	}

	writeJSON(w, map[string]interface{}{
		"actions":          out,
		"registered":       registered,
		"reserved":         reserved,
		"registered_count": len(registered),
		"reserved_count":   len(reserved),
	})
}

// handlePermissions reads and writes the policy. A PUT applies immediately in
// the running bridge (the previous behaviour required exporting a file and
// restarting), and rejects a policy that would not be loadable.
func (a *AdminServer) handlePermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path := a.integration.permissionsPath
		data, err := os.ReadFile(path)
		if err != nil {
			policy := a.integration.policy()
			payload := map[string]interface{}{
				"note":             "policy file missing; showing runtime defaults",
				"permissions_path": path,
				"default_allow":    policy != nil && policy.DefaultAllow,
				"scopes":           scopeConfigMap(policy),
			}
			writeJSON(w, payload)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)

	case http.MethodPut, http.MethodPost:
		if !a.requireOperatorReentry(w, r) {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read request body")
			return
		}

		// Validate before touching the disk: a dashboard typo must not leave the
		// bridge with an unparseable policy file.
		var parsed permissionPolicyFile
		if err := json.Unmarshal(body, &parsed); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid policy JSON: %v", err))
			return
		}
		// An unknown scope name is a typo that would silently drop a switch.
		for scope := range parsed.Scopes {
			if !knownScope(scope) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown permission scope %q (use one of: %s)", scope, scopeNameList(allScopes())))
				return
			}
		}
		normalized, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not normalise policy")
			return
		}

		path := a.integration.permissionsPath
		if path == "" {
			writeError(w, http.StatusBadRequest, "no permissions path configured (NEURO_PERMISSIONS_FILE)")
			return
		}
		if err := atomicWriteFile(path, normalized); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not write policy: %v", err))
			return
		}

		policy, err := loadPermissionPolicy(path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.integration.setPolicy(policy)
		go a.integration.syncMCPServers()
		a.writeAudit("permissions updated", false)
		log.Printf("Operator updated the permission policy (%s)", path)

		writeJSON(w, map[string]interface{}{
			"ok":              true,
			"saved":           path,
			"applied_live":    true,
			"default_allow":   policy.DefaultAllow,
			"allowed_actions": len(policy.allowed),
			"denied_actions":  len(policy.denied),
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func scopeConfigMap(policy *PermissionPolicy) map[string]bool {
	out := map[string]bool{}
	for _, scope := range allScopes() {
		out[string(scope)] = false
	}
	if policy == nil {
		return out
	}
	for scope, cfg := range policy.scopes {
		out[string(scope)] = cfg.Allowed
	}
	return out
}

func (a *AdminServer) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	index, err := loadCatalogIndex(catalogFilePath())
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}
	publishers := loadPublishersOrEmpty()
	views := make([]map[string]interface{}, 0, len(index.Items))
	for _, item := range index.Items {
		views = append(views, catalogItemView(item, publishers))
	}
	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"items":        views,
		"path":         catalogFilePath(),
		"publishers":   len(publishers.Publishers),
		"install_mode": extensionInstallMode(),
	})
}

func (a *AdminServer) handleExtensions(w http.ResponseWriter, _ *http.Request) {
	state, err := loadExtensionState(extensionStatePath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := []CatalogItem{}
	if index, err := loadCatalogIndex(catalogFilePath()); err == nil {
		items = index.Items
	}
	publishers := loadPublishersOrEmpty()

	installed := make([]map[string]interface{}, 0, len(state.Installed))
	for id, install := range state.Installed {
		entry := map[string]interface{}{
			"id":           id,
			"enabled":      install.Enabled,
			"installed_at": install.InstalledAt,
			"source":       install.Source,
			"path":         install.Path,
		}
		entry["trust"] = install.Trust
		entry["publisher"] = install.Publisher
		entry["commit"] = install.Commit
		if item := findCatalogItemByID(CatalogIndex{Items: items}, id); item != nil {
			entry["name"] = item.Name
			entry["description"] = catalogPresentationItem(*item, true).Description
			entry["type"] = item.Type
			entry["repository"] = item.Repository
			entry["signature_state"] = verifyCatalogItem(*item, publishers).State
		}
		installed = append(installed, entry)
	}
	sort.Slice(installed, func(i, j int) bool {
		return installed[i]["id"].(string) < installed[j]["id"].(string)
	})

	catalog := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		catalog = append(catalog, catalogItemView(item, publishers))
	}

	writeJSON(w, map[string]interface{}{
		"ok":            true,
		"installed":     installed,
		"catalog":       catalog,
		"install_mode":  extensionInstallMode(),
		"extension_dir": extensionRootPath(),
		"state_file":    extensionStatePath(),
		"unsigned_ok":   extensionAllowsUnsigned(),
		"built_in_services": map[string]interface{}{
			"vision": a.integration.visionServiceSnapshot(),
		},
	})
}

// handleExtensionAction implements POST /api/extensions/{id}/{action}
func (a *AdminServer) handleExtensionAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/extensions/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "expected /api/extensions/{id}/{install|enable|disable|uninstall}")
		return
	}

	id := strings.TrimSpace(parts[0])
	action := strings.ToLower(strings.TrimSpace(parts[1]))
	if action == "permissions" {
		a.handleExtensionPermissions(w, r, id)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "extension id is required")
		return
	}
	if !a.credential.matches(strings.TrimSpace(r.Header.Get("X-ND-Operator-Token"))) {
		if err := a.integration.checkExtensionManagement(id); err != nil {
			writeError(w, http.StatusForbidden, err.Error()+"; re-enter your operator token to manage it directly")
			return
		}
	}

	var result permissiveResult
	switch action {
	case "install":
		result = wrapExecutionResult(a.integration.installExtension(id))
	case "uninstall":
		result = wrapExecutionResult(a.integration.uninstallExtension(id))
	case "enable":
		result = wrapExecutionResult(a.integration.setExtensionEnabled(id, true))
	case "disable":
		result = wrapExecutionResult(a.integration.setExtensionEnabled(id, false))
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown extension action %q", action))
		return
	}

	status := http.StatusOK
	if !result.OK {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
	a.writeAudit(fmt.Sprintf("extension %s %s", id, action), !result.OK)
}

type permissiveResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	ID      string `json:"id,omitempty"`
	Action  string `json:"action,omitempty"`
}

func wrapExecutionResult(result neuro.ExecutionResult) permissiveResult {
	return permissiveResult{OK: result.Successful, Message: result.Message}
}

// handleGames lists profiles and reports which one matches right now.
func (a *AdminServer) handleGames(w http.ResponseWriter, _ *http.Request) {
	profiles := a.integration.games.Profiles()
	out := make([]map[string]interface{}, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]interface{}{
			"id":          profile.ID,
			"name":        profile.Name,
			"description": profile.Description,
			"mode":        profile.Control.Mode,
			"external":    profile.Control.ExternalName,
			"keys":        profile.Keys,
			"mouse_look":  profile.Control.MouseLook.Enabled,
			"vision":      profile.Vision.Recommended,
			"launchable":  len(profile.Launch.Commands) > 0,
			"tags":        profile.Tags,
		})
	}

	var detected *GameDetected
	permit, permitErr := a.integration.observationPermit(CmdGameDetect)
	if permitErr == nil && a.integration.executorHub != nil && a.integration.executorHub.HasClient() {
		value, err := a.integration.detectGameGuarded(true, permit)
		if err == nil {
			detected = &value
		}
	}
	integrations := a.integration.adapterSnapshot()
	if detected != nil && permit.check() != nil {
		detected = nil
	}

	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"profiles":        out,
		"registry_source": a.integration.games.RegistrySource(),
		"detected":        detected,
		"session":         a.integration.games.Session(),
		"integrations":    integrations,
	})
}

func (a *AdminServer) handleGameSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			ProfileID string `json:"profile_id"`
			Launch    bool   `json:"launch"`
			Mode      string `json:"mode"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
		}

		session := a.integration.games.Session()
		if session != nil {
			a.integration.endGameSession("replaced by operator")
		}

		result := a.integration.startGameSession(
			strings.TrimSpace(body.ProfileID), body.Launch, strings.TrimSpace(body.Mode))
		if !result.Successful {
			writeError(w, http.StatusConflict, result.Message)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": result.Message, "session": a.integration.games.Session()})

	case http.MethodDelete:
		result := a.integration.endGameSession("stopped by operator")
		writeJSON(w, map[string]interface{}{"ok": result.Successful, "message": result.Message})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *AdminServer) handleGameRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result := a.integration.releaseAllInput()
	status := http.StatusOK
	if !result.Successful {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": result.Successful, "message": result.Message})
}

// handleGameObserve answers "what would Neuro see right now?" for the
// dashboard. It is the same capture + vision path game_observe uses, minus the
// context message.
func (a *AdminServer) handleGameObserve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		Vision *bool  `json:"vision"`
		Prompt string `json:"prompt"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "Use a JSON object with vision (boolean) and optional prompt.")
			return
		}
	}
	useVision := visionServerURL() != ""
	if body.Vision != nil {
		useVision = *body.Vision
	}

	message, err := a.integration.buildGameObservation(useVision, strings.TrimSpace(body.Prompt))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.writeAudit("game observe", false)
	writeJSON(w, map[string]interface{}{"ok": true, "observation": message})
}

func (a *AdminServer) handleRelay(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{
		"ok":    true,
		"relay": a.integration.relay.status(),
	})
}

func (a *AdminServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"version": Version,
		"paths": map[string]interface{}{
			"permissions":   a.integration.permissionsPath,
			"ipc":           a.integration.ipcFilePath,
			"catalog":       catalogFilePath(),
			"extensions":    extensionStatePath(),
			"game_profiles": a.integration.games.RegistrySource(),
			"extension_dir": extensionRootPath(),
			"install_mode":  extensionInstallMode(),
		},
		"features": map[string]interface{}{
			"game_actions":   RegisterGameActionsOnStartup,
			"relay_enabled":  a.integration.relay.status().Enabled,
			"vision_enabled": visionServerURL() != "",
			"executor_token": a.integration.executorHub != nil && a.integration.executorHub.tokenRequired(),
			"dev_build":      extensionsDevBuild,
			// Only a dev build (-tags neurodev) can ever answer true.
			"unsigned_extensions_allowed": extensionAllowsUnsigned(),
		},
	})
}

// ---------------------------------------------------------------
// Static dashboard hosting
// ---------------------------------------------------------------

func (a *AdminServer) handleUI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}

	if !strings.HasPrefix(path, "ui") && path != "favicon.ico" {
		http.NotFound(w, r)
		return
	}

	root, err := resolveFrontendRoot()
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Neuro Desktop dashboard assets not found: %v\n\nBuild the UI with `npm run build` in desktop/frontend, or use the bundled release.", err)
		return
	}

	relative := strings.TrimPrefix(strings.TrimPrefix(path, "ui"), "/")
	if relative == "" {
		relative = "index.html"
	}

	candidate := filepath.Join(root, filepath.Clean("/"+relative))
	if info, err := os.Stat(candidate); err != nil || info.IsDir() {
		// Single-page app: unknown paths fall back to the shell.
		candidate = filepath.Join(root, "index.html")
	}

	if _, err := os.Stat(candidate); err != nil {
		http.NotFound(w, r)
		return
	}

	if filepath.Base(candidate) == "index.html" {
		a.serveDashboardShell(w, r, candidate)
		return
	}

	http.ServeFile(w, r, candidate)
}

// serveDashboardShell serves index.html. It carries no token: the operator types
// the dashboard token into the sign-in page, so a page served to any caller,
// local or not, never contains a secret.
func (a *AdminServer) serveDashboardShell(w http.ResponseWriter, r *http.Request, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The shell is regenerated per request (and a rebuilt UI must not be served
	// from the browser cache), so hashed assets aside, do not cache it.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func resolveFrontendRoot() (string, error) {
	candidates := []string{}

	// An explicit override wins: packaging puts the UI somewhere specific, and
	// it makes the search deterministic for tests and for a custom build.
	if override := strings.TrimSpace(os.Getenv("NEURO_UI_DIR")); override != "" {
		if info, err := os.Stat(filepath.Join(override, "index.html")); err == nil && !info.IsDir() {
			return override, nil
		}
		return "", fmt.Errorf("NEURO_UI_DIR=%s does not contain index.html", override)
	}

	// An explicit override wins: useful for a custom build or a packaged UI
	// placed outside the executable's directory.
	if override := strings.TrimSpace(os.Getenv("NEURO_UI_DIR")); override != "" {
		candidates = append(candidates, override)
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "frontend"),
			filepath.Join(exeDir, "ui"),
		)
	}

	candidates = append(candidates,
		filepath.Join("frontend", "dist"),
		filepath.Join("desktop", "frontend", "dist"),
		filepath.Join("..", "..", "frontend", "dist"),
	)

	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no frontend/dist/index.html in any of: %s", strings.Join(candidates, ", "))
}

// ---------------------------------------------------------------
// Operator brake
// ---------------------------------------------------------------

func (a *AdminServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Use GET /api/control.")
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"control": a.integration.stop.status(),
		"audit":   a.integration.audit.status(),
	})
}

func (a *AdminServer) handleControlPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Use POST /api/control/pause.")
		return
	}
	a.integration.stop.setPaused(true)
	a.integration.hideAllAdapterActivity()
	a.integration.audit.record("control", map[string]interface{}{"paused": true, "source": "dashboard"})
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"paused":  true,
		"message": "Paused: every action except input release and status is refused until you resume.",
	})
}

func (a *AdminServer) handleControlResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Use POST /api/control/resume.")
		return
	}
	a.integration.stop.setPaused(false)
	a.integration.audit.record("control", map[string]interface{}{"paused": false, "source": "dashboard"})
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"paused":  false,
		"message": "Resumed.",
	})
}

// handleAudit returns the tail of the audit log so the dashboard can show what
// happened without shell access to the file.
func (a *AdminServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Use GET /api/audit.")
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	path := strings.TrimSpace(os.Getenv("NEURO_AUDIT_LOG"))
	if path == "" {
		writeJSON(w, map[string]interface{}{
			"ok":      true,
			"enabled": false,
			"entries": []interface{}{},
			"message": "Set NEURO_AUDIT_LOG to record action decisions.",
		})
		return
	}

	entries, truncated, err := tailJSONLines(path, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":        true,
		"enabled":   true,
		"path":      path,
		"truncated": truncated,
		"entries":   entries,
	})
}

// tailJSONLines reads at most the last `limit` lines of a file and parses them
// as JSON objects, returning the newest last.
func tailJSONLines(path string, limit int) ([]map[string]interface{}, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []map[string]interface{}{}, false, nil
		}
		return nil, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ring := make([]map[string]interface{}, 0, limit)
	truncated := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entry := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if len(ring) == limit {
			ring = ring[1:]
			truncated = true
		}
		ring = append(ring, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, truncated, err
	}
	return ring, truncated, nil
}
