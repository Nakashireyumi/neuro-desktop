package main

// Built-in services have no catalog entry or install step. Their operator-owned
// settings are separate from extension state, permissions and signed metadata.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type visionServiceActors struct {
	Neuro bool `json:"neuro"`
	Evil  bool `json:"evil"`
}

type visionServiceConfig struct {
	Enabled bool                `json:"enabled"`
	Actors  visionServiceActors `json:"actors"`
}

type visionServiceSnapshot struct {
	visionServiceConfig
	Persona    string `json:"persona"`
	Effective  bool   `json:"effective"`
	Reason     string `json:"reason,omitempty"`
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint,omitempty"`
}

type builtinServiceRuntime struct {
	mu         sync.Mutex
	loaded     bool
	path       string
	vision     visionServiceConfig
	problem    string
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
}

// A permit belongs to the settings generation that admitted the capture. A
// disable followed by re-enabling cannot resurrect its screenshot or analysis.
type visionServicePermit struct {
	integration *NDIntegration
	generation  uint64
	ctx         context.Context
}

func builtinServicesPath() string {
	if path := strings.TrimSpace(os.Getenv("NEURO_BUILTIN_SERVICES_FILE")); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(extensionStatePath()), "builtin-services.json")
}

func normalizedPersona(persona string) (string, error) {
	persona = strings.ToLower(strings.TrimSpace(persona))
	if persona == "" {
		return "shared", nil
	}
	switch persona {
	case "neuro", "evil", "shared":
		return persona, nil
	default:
		return "", errors.New("persona must be neuro, evil or shared; set --persona or NEURO_PERSONA for this bridge connection")
	}
}

func defaultVisionServiceConfig() visionServiceConfig {
	return visionServiceConfig{Enabled: true, Actors: visionServiceActors{Neuro: true, Evil: true}}
}

// Pointers distinguish an explicit false from a missing field or null value.
type visionServiceInput struct {
	Enabled *bool `json:"enabled"`
	Actors  *struct {
		Neuro *bool `json:"neuro"`
		Evil  *bool `json:"evil"`
	} `json:"actors"`
}

func (input visionServiceInput) config() (visionServiceConfig, error) {
	if input.Enabled == nil || input.Actors == nil || input.Actors.Neuro == nil || input.Actors.Evil == nil {
		return visionServiceConfig{}, errors.New("enabled, actors.neuro and actors.evil must all be booleans")
	}
	return visionServiceConfig{Enabled: *input.Enabled, Actors: visionServiceActors{Neuro: *input.Actors.Neuro, Evil: *input.Actors.Evil}}, nil
}

func decodeVisionServiceInput(reader io.Reader) (visionServiceConfig, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var input visionServiceInput
	if err := decoder.Decode(&input); err != nil {
		return visionServiceConfig{}, fmt.Errorf("use a JSON object containing enabled and actors.neuro/evil booleans: %w", err)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return visionServiceConfig{}, errors.New("use exactly one JSON object")
	}
	return input.config()
}

func loadBuiltinVisionConfig(path string) (visionServiceConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultVisionServiceConfig(), nil
	}
	if err != nil {
		return visionServiceConfig{}, errors.New("cannot read built-in service settings")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return visionServiceConfig{}, errors.New("built-in service settings are unreadable or exceed 16 KiB")
	}
	var stored struct {
		Version *int                `json:"version"`
		Vision  *visionServiceInput `json:"vision"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil || stored.Version == nil || *stored.Version != 1 || stored.Vision == nil {
		return visionServiceConfig{}, errors.New("built-in service settings have an invalid format or unsupported version")
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return visionServiceConfig{}, errors.New("built-in service settings must contain exactly one JSON object")
	}
	return stored.Vision.config()
}

func (n *NDIntegration) loadBuiltinServicesLocked() {
	s := &n.builtinServices
	if s.loaded {
		return
	}
	s.loaded, s.path, s.generation = true, builtinServicesPath(), 1
	s.vision, s.problem = defaultVisionServiceConfig(), ""
	if config, err := loadBuiltinVisionConfig(s.path); err != nil {
		s.vision = visionServiceConfig{}
		s.problem = fmt.Sprintf("Vision Service is disabled: %v. Approve corrected settings in Extensions, or repair NEURO_BUILTIN_SERVICES_FILE and restart.", err)
	} else {
		s.vision = config
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (n *NDIntegration) visionServiceSnapshotLocked() visionServiceSnapshot {
	s := &n.builtinServices
	persona, err := normalizedPersona(n.persona)
	snapshot := visionServiceSnapshot{visionServiceConfig: s.vision, Persona: persona,
		Configured: visionServerURL() != "", Endpoint: redactURL(visionServerURL())}
	switch {
	case s.problem != "":
		snapshot.Reason = s.problem
	case err != nil:
		snapshot.Persona = "shared"
		snapshot.Reason = err.Error()
	case !s.vision.Enabled:
		snapshot.Reason = "Vision Service is disabled. Enable it in Extensions with your operator token to capture or process screenshots."
	case persona == "neuro" && !s.vision.Actors.Neuro:
		snapshot.Reason = "Vision Service is disabled for Neuro on this bridge connection."
	case persona == "evil" && !s.vision.Actors.Evil:
		snapshot.Reason = "Vision Service is disabled for Evil on this bridge connection."
	case persona == "shared" && (!s.vision.Actors.Neuro || !s.vision.Actors.Evil):
		snapshot.Reason = "This shared bridge needs Vision Service enabled for both Neuro and Evil. Use a separate --persona neuro or --persona evil connection for individual settings."
	default:
		snapshot.Effective = true
	}
	return snapshot
}

func (n *NDIntegration) visionServiceSnapshot() visionServiceSnapshot {
	n.builtinServices.mu.Lock()
	defer n.builtinServices.mu.Unlock()
	n.loadBuiltinServicesLocked()
	return n.visionServiceSnapshotLocked()
}

func (n *NDIntegration) visionServicePermit() (*visionServicePermit, error) {
	n.builtinServices.mu.Lock()
	defer n.builtinServices.mu.Unlock()
	n.loadBuiltinServicesLocked()
	snapshot := n.visionServiceSnapshotLocked()
	if !snapshot.Effective {
		return nil, errors.New(snapshot.Reason)
	}
	s := &n.builtinServices
	return &visionServicePermit{integration: n, generation: s.generation, ctx: s.ctx}, nil
}

func (permit *visionServicePermit) check() error {
	n := permit.integration
	n.builtinServices.mu.Lock()
	defer n.builtinServices.mu.Unlock()
	if n.builtinServices.generation != permit.generation || permit.ctx.Err() != nil {
		return errors.New("Vision Service settings changed; request a new observation")
	}
	snapshot := n.visionServiceSnapshotLocked()
	if !snapshot.Effective {
		return errors.New(snapshot.Reason)
	}
	return nil
}

func (n *NDIntegration) saveVisionServiceConfig(config visionServiceConfig) error {
	s := &n.builtinServices
	s.mu.Lock()
	defer s.mu.Unlock()
	n.loadBuiltinServicesLocked()
	data, err := json.MarshalIndent(struct {
		Version int                 `json:"version"`
		Vision  visionServiceConfig `json:"vision"`
	}{Version: 1, Vision: config}, "", "  ")
	if err != nil {
		return err
	}
	// Publish only after the complete configuration is durably replaced. A
	// failed write must not silently enable a service or invalidate approvals.
	if err := atomicWriteFile(s.path, append(data, '\n')); err != nil {
		return errors.New("cannot save built-in service settings; check the permissions and path of NEURO_BUILTIN_SERVICES_FILE")
	}
	s.cancel()
	s.generation++
	s.vision, s.problem = config, ""
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return nil
}

func (n *NDIntegration) cancelVisionServiceRequests() {
	s := &n.builtinServices
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (a *AdminServer) handleVisionService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if !a.requireOperatorReentry(w, r) {
			return
		}
		config, err := decodeVisionServiceInput(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := a.integration.saveVisionServiceConfig(config); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.integration.audit.record("builtin_service", map[string]interface{}{
			"service": "vision", "decision": "updated", "enabled": config.Enabled,
			"neuro": config.Actors.Neuro, "evil": config.Actors.Evil,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Use GET or PUT /api/services/vision.")
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "vision": a.integration.visionServiceSnapshot()})
}
