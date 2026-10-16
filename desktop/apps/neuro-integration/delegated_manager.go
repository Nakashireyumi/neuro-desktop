package main

// A delegated manager session is a revocable, in-memory credential for Neuro or
// Evil. It never carries the operator credential or authority to change policy.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const delegatedManagerDefaultMinutes = 60
const delegatedManagerMaximumMinutes = 24 * 60

type delegatedManagerStore struct {
	mu        sync.Mutex
	hash      [sha256.Size]byte
	expiresAt time.Time
	persona   string
}

type delegatedManagerContextKey struct{}
type delegatedManagerSession struct {
	expiresAt time.Time
	persona   string
}

func (s *delegatedManagerStore) issue(minutes int, persona string) (string, time.Time, error) {
	if minutes == 0 {
		minutes = delegatedManagerDefaultMinutes
	}
	if minutes < 1 || minutes > delegatedManagerMaximumMinutes {
		return "", time.Time{}, errors.New("minutes must be between 1 and 1440")
	}
	if persona == "" {
		persona = "neuro"
	}
	if persona != "neuro" && persona != "evil" {
		return "", time.Time{}, errors.New("persona must be neuro or evil")
	}
	random, err := generateDashboardToken()
	if err != nil {
		return "", time.Time{}, err
	}
	token := "ndd_" + random
	expires := time.Now().Add(time.Duration(minutes) * time.Minute)
	s.mu.Lock()
	s.hash = sha256.Sum256([]byte(token))
	s.expiresAt = expires
	s.persona = persona
	s.mu.Unlock()
	return token, expires, nil
}

func (s *delegatedManagerStore) session(token string) (bool, time.Time, string) {
	if token == "" {
		return false, time.Time{}, ""
	}
	sum := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	if !time.Now().Before(s.expiresAt) || subtle.ConstantTimeCompare(sum[:], s.hash[:]) != 1 {
		// Expired, revoked and unknown credentials are indistinguishable to callers.
		return false, time.Time{}, ""
	}
	return true, s.expiresAt, s.persona
}

func (s *delegatedManagerStore) revoke() {
	s.mu.Lock()
	s.hash = [sha256.Size]byte{}
	s.expiresAt = time.Time{}
	s.persona = ""
	s.mu.Unlock()
}

// managerSessionRole is used only after guard has authenticated the request.
// "neuro" denotes restricted authority; persona is presentation metadata only.
func (a *AdminServer) managerSessionRole(r *http.Request) (role string, expiresAt string) {
	if recognized, expires, _ := a.delegatedSession(r); recognized {
		return "neuro", expires.UTC().Format(time.RFC3339)
	}
	return "operator", ""
}

func (a *AdminServer) delegatedSession(r *http.Request) (bool, time.Time, string) {
	// Preserve the authority admitted by guard even if the token expires or is
	// revoked while this request is running. It must not appear to become an
	// operator session merely because its store entry no longer exists.
	if session, ok := r.Context().Value(delegatedManagerContextKey{}).(delegatedManagerSession); ok {
		return true, session.expiresAt, session.persona
	}
	return a.delegation.session(requestToken(r))
}

func (a *AdminServer) handleDelegatedSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Use GET /api/delegation/session.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	role, expires := a.managerSessionRole(r)
	_, _, persona := a.delegatedSession(r)
	writeJSON(w, map[string]interface{}{
		"ok": true, "role": role, "expires_at": expires, "persona": persona,
		"can_edit_permissions": role == "operator", "can_resume": role == "operator",
	})
}

// handleDelegationToken requires both an authenticated operator session and a
// fresh operator-secret entry. The UI must never fill this header from memory.
// Issuing rotates the sole active session; tokens are returned once, not stored.
func (a *AdminServer) handleDelegationToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if !a.credential.matches(requestToken(r)) || !a.credential.matches(strings.TrimSpace(r.Header.Get("X-ND-Operator-Token"))) {
		writeError(w, http.StatusForbidden, "re-enter your operator token to create or revoke a delegated manager session")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Minutes int    `json:"minutes"`
			Persona string `json:"persona"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "body must contain minutes and an optional neuro/evil persona")
			return
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			writeError(w, http.StatusBadRequest, "body must contain exactly one JSON object")
			return
		}
		token, expires, err := a.delegation.issue(body.Minutes, body.Persona)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_, _, persona := a.delegation.session(token)
		a.integration.audit.record("delegated_manager", map[string]interface{}{"decision": "issued", "persona": persona, "expires_at": expires.UTC().Format(time.RFC3339)})
		writeJSON(w, map[string]interface{}{"ok": true, "role": "neuro", "persona": persona, "token": token, "expires_at": expires.UTC().Format(time.RFC3339)})
	case http.MethodDelete:
		a.delegation.revoke()
		a.integration.audit.record("delegated_manager", map[string]interface{}{"decision": "revoked"})
		writeJSON(w, map[string]interface{}{"ok": true, "revoked": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Use POST to create or DELETE to revoke the delegated session.")
	}
}

// authorizeDelegated is called by guard before testing the operator credential.
// A recognized token must never fall through to another credential/header when
// its route is denied. Unknown/expired tokens continue to the normal 401 path.
func (a *AdminServer) authorizeDelegated(r *http.Request) (recognized bool, err error) {
	valid, expires, persona := a.delegation.session(requestToken(r))
	if !valid {
		return false, nil
	}
	*r = *r.WithContext(context.WithValue(r.Context(), delegatedManagerContextKey{}, delegatedManagerSession{expiresAt: expires, persona: persona}))
	err = a.authorizeDelegatedRoute(r)
	if err != nil {
		a.integration.audit.record("delegated_manager", map[string]interface{}{
			"decision": "denied", "method": r.Method, "path": r.URL.Path,
		})
	}
	return true, err
}

func (a *AdminServer) delegatedActionAllowed(action string, mutating bool) error {
	n := a.integration
	if n == nil {
		return fmt.Errorf("delegated manager action %s is denied by policy", action)
	}
	policy := n.policy()
	if policy == nil || !policy.IsAllowed(action) {
		return fmt.Errorf("delegated manager action %s is denied by policy", action)
	}
	if mutating {
		if reason := n.stop.blockReason(action); reason != "" {
			return errors.New(reason)
		}
	}
	scope, _ := scopeForAction(action)
	if limit := policy.ScopeRateLimit(scope); limit > 0 {
		if allowed, retry := n.rate.allow(scope, limit, time.Now()); !allowed {
			return errors.New(rateLimitDenial(scope, limit, retry))
		}
	}
	return nil
}

func (a *AdminServer) authorizeDelegatedRoute(r *http.Request) error {
	denied := errors.New("this operation is reserved for your operator session")
	path := r.URL.Path
	if r.Method == http.MethodGet {
		switch path {
		case "/api/session", "/api/delegation/session", "/api/status", "/api/control", "/api/config", "/api/actions", "/api/permissions", "/api/permissions/schema", "/api/relay":
			return nil
		case "/api/catalog":
			return a.delegatedActionAllowed(string(CmdListCatalogItems), false)
		case "/api/marketplace":
			return a.delegatedActionAllowed(string(CmdMarketplaceSearch), false)
		case "/api/extensions":
			// The catalog view remains browsable before management is delegated.
			return a.delegatedActionAllowed(string(CmdListCatalogItems), false)
		case "/api/games":
			if err := a.delegatedActionAllowed(string(CmdGameListProfiles), false); err != nil {
				return err
			}
			// This response also contains active-window/process detection.
			return a.delegatedActionAllowed(string(CmdGameDetect), false)
		}
		return denied
	}
	if r.Method == http.MethodPost {
		switch path {
		case "/api/games/release", "/api/input/release", "/api/control/pause":
			return nil // Recovery actions never depend on a grant or stop switch.
		case "/api/games/observe":
			return a.delegatedActionAllowed(string(CmdGameObserve), true)
		case "/api/games/session":
			if err := a.delegatedActionAllowed(string(CmdGameStartSession), true); err != nil {
				return err
			}
			// The operator route also offers launch=true. Gate that capability
			// separately and restore the bytes for the real handler to parse.
			if r.Body == nil {
				r.Body = http.NoBody
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err != nil || len(body) > 64<<10 {
				return errors.New("game session request exceeds the size limit")
			}
			var request struct {
				Launch bool `json:"launch"`
			}
			if len(bytes.TrimSpace(body)) != 0 {
				if err := json.Unmarshal(body, &request); err != nil {
					return errors.New("game session body must be a JSON object")
				}
			}
			if request.Launch {
				return a.delegatedActionAllowed(string(CmdGameLaunch), true)
			}
			return nil
		}
		for prefix, operations := range map[string]map[string]CommandType{
			"/api/extensions/":  {"install": CmdInstallExtension, "enable": CmdEnableExtension, "disable": CmdDisableExtension, "uninstall": CmdUninstallExtension},
			"/api/marketplace/": {"stage": CmdStageIntegration},
		} {
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
			if len(parts) != 2 || operations[parts[1]] == "" {
				return denied
			}
			id, err := normalizeExtensionID(parts[0])
			if err != nil || id != parts[0] {
				return errors.New("a canonical extension id is required")
			}
			if err := a.integration.checkExtensionManagement(id); err != nil {
				return err
			}
			return a.delegatedActionAllowed(string(operations[parts[1]]), true)
		}
	}
	if r.Method == http.MethodDelete && path == "/api/games/session" {
		return a.delegatedActionAllowed(string(CmdGameEndSession), true)
	}
	return denied
}
