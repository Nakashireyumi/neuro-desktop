package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func builtinServiceFixture(t *testing.T, persona string) (*NDIntegration, *AdminServer) {
	t.Helper()
	t.Setenv("NEURO_BUILTIN_SERVICES_FILE", filepath.Join(t.TempDir(), "builtin-services.json"))
	t.Setenv("NEURO_VISION_URL", "")
	t.Setenv("NEURO_VISION_SERVER_URL", "")
	n := testIntegration(t)
	n.persona = persona
	t.Cleanup(n.cancelVisionServiceRequests)
	return n, NewAdminServer(n, "", credentialFromToken(testDashboardToken, "test"))
}

func setBuiltinVision(t *testing.T, n *NDIntegration, enabled, neuro, evil bool) {
	t.Helper()
	if err := n.saveVisionServiceConfig(visionServiceConfig{Enabled: enabled, Actors: visionServiceActors{Neuro: neuro, Evil: evil}}); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinVisionDefaultAndPersonaMatrix(t *testing.T) {
	t.Setenv("NEURO_BUILTIN_SERVICES_FILE", "")
	t.Setenv("NEURO_EXTENSIONS_STATE_FILE", filepath.Join(t.TempDir(), "extensions-state.json"))
	if got, want := builtinServicesPath(), filepath.Join(filepath.Dir(extensionStatePath()), "builtin-services.json"); got != want {
		t.Fatalf("default path %q, want %q", got, want)
	}
	n := &NDIntegration{}
	initial := n.visionServiceSnapshot()
	if initial.Persona != "shared" || !initial.Enabled || !initial.Actors.Neuro || !initial.Actors.Evil || !initial.Effective {
		t.Fatalf("default config should preserve existing service access: %+v", initial)
	}
	t.Cleanup(n.cancelVisionServiceRequests)
	for _, persona := range []string{"neuro", "evil", "shared", "", "unknown"} {
		for _, actors := range []visionServiceActors{{true, true}, {true, false}, {false, true}, {false, false}} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t/%t", persona, enabled, actors.Neuro, actors.Evil), func(t *testing.T) {
					candidate, _ := builtinServiceFixture(t, persona)
					setBuiltinVision(t, candidate, enabled, actors.Neuro, actors.Evil)
					want := enabled && ((persona == "neuro" && actors.Neuro) || (persona == "evil" && actors.Evil) || ((persona == "shared" || persona == "") && actors.Neuro && actors.Evil))
					got := candidate.visionServiceSnapshot()
					if got.Effective != want || (!want && got.Reason == "") {
						t.Fatalf("effective = %t, want %t: %+v", got.Effective, want, got)
					}
				})
			}
		}
	}
	if _, err := NewNDIntegration(IntegrationOptions{Persona: "untrusted-action-persona"}); err == nil {
		t.Fatal("startup accepted an unrecognized operator persona")
	}
}

func TestBuiltinVisionSettingsPersistAndInvalidFileFailsClosed(t *testing.T) {
	n, a := builtinServiceFixture(t, "evil")
	setBuiltinVision(t, n, true, true, false)
	restarted := &NDIntegration{persona: "evil"}
	defer restarted.cancelVisionServiceRequests()
	got := restarted.visionServiceSnapshot()
	if got.Effective || !got.Enabled || got.Actors.Evil {
		t.Fatalf("restart lost the operator's choice: %+v", got)
	}
	if err := os.WriteFile(builtinServicesPath(), []byte(`{"version":1,"vision":{"enabled":true,"actors":{"neuro":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	broken := &NDIntegration{persona: "evil"}
	defer broken.cancelVisionServiceRequests()
	got = broken.visionServiceSnapshot()
	if got.Enabled || got.Effective || !strings.Contains(got.Reason, "Approve corrected settings") {
		t.Fatalf("invalid stored settings did not fail closed: %+v", got)
	}
	a.integration = broken
	code, _ := adapterRequest(t, a, http.MethodPut, "/api/services/vision", defaultVisionServiceConfig(), map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != http.StatusOK || !broken.visionServiceSnapshot().Effective {
		t.Fatal("operator could not repair invalid settings through the live manager")
	}
	for _, invalid := range []string{
		`{`, `null`, `{}`, `{"version":2,"vision":{"enabled":true,"actors":{"neuro":true,"evil":true}}}`,
		`{"version":1,"vision":{"enabled":true,"actors":{"neuro":true,"evil":true}},"unexpected":true}`,
		`{"version":1,"vision":{"enabled":null,"actors":{"neuro":true,"evil":true}}}`,
		`{"version":1,"vision":{"enabled":true,"actors":{"neuro":true,"evil":true}}} {}`,
		strings.Repeat("x", 16385),
	} {
		if err := os.WriteFile(builtinServicesPath(), []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadBuiltinVisionConfig(builtinServicesPath()); err == nil {
			t.Fatalf("invalid persisted settings were accepted: %.100s", invalid)
		}
	}
}

func TestBuiltinVisionSaveFailureDoesNotPublishOrInvalidatePermit(t *testing.T) {
	n, _ := builtinServiceFixture(t, "neuro")
	permit, err := n.visionServicePermit()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(builtinServicesPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := n.saveVisionServiceConfig(visionServiceConfig{}); err == nil {
		t.Fatal("saving over an existing directory should fail")
	}
	if !n.visionServiceSnapshot().Effective || permit.check() != nil {
		t.Fatal("failed persistence changed live settings or invalidated a permitted request")
	}
}

func TestBuiltinVisionAPIRequiresFreshOperatorAndRejectsIncompleteSettings(t *testing.T) {
	n, a := builtinServiceFixture(t, "shared")
	valid := defaultVisionServiceConfig()
	for _, headers := range []map[string]string{nil, {"X-ND-Token": testDashboardToken}, {"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": "wrong"}} {
		code, _ := adapterRequest(t, a, http.MethodPut, "/api/services/vision", valid, headers)
		if code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("settings changed without a freshly entered operator token: %d", code)
		}
	}
	server := httptest.NewServer(a.routes())
	defer server.Close()
	for _, body := range []string{`{}`, `null`, `{"enabled":true}`, `{"enabled":true,"actors":{"neuro":true}}`, `{"enabled":true,"actors":null}`, `{"enabled":true,"actors":{"neuro":true,"evil":null}}`, `{"enabled":"true","actors":{"neuro":true,"evil":true}}`, `{"enabled":true,"actors":{"neuro":true,"evil":true},"persona":"evil"}`, `{"enabled":true,"actors":{"neuro":true,"evil":true}} {}`, strings.Repeat(" ", 8200)} {
		code, _ := sendJSON(t, http.MethodPut, server.URL+"/api/services/vision", body, map[string]string{"X-ND-Operator-Token": testDashboardToken})
		if code != http.StatusBadRequest {
			t.Fatalf("invalid settings accepted (%d): %.100s", code, body)
		}
	}
	if _, err := os.Stat(builtinServicesPath()); !os.IsNotExist(err) {
		t.Fatal("rejected settings should not create or change a file")
	}
	code, _ := adapterRequest(t, a, http.MethodPut, "/api/services/vision", valid, map[string]string{"X-ND-Token": testDashboardToken, "X-ND-Operator-Token": testDashboardToken})
	if code != 200 || !n.visionServiceSnapshot().Effective {
		t.Fatalf("freshly approved valid settings failed: %d", code)
	}
	delegated, _, err := a.delegation.issue(1, "evil")
	if err != nil {
		t.Fatal(err)
	}
	code, body := adapterRequest(t, a, http.MethodGet, "/api/services/vision", nil, map[string]string{"X-ND-Token": delegated})
	if code != http.StatusOK || body["vision"].(map[string]interface{})["persona"] != "shared" {
		t.Fatalf("delegated persona overrode the trusted bridge configuration: %d %v", code, body)
	}
	code, _ = adapterRequest(t, a, http.MethodPut, "/api/services/vision", visionServiceConfig{}, map[string]string{"X-ND-Token": delegated, "X-ND-Operator-Token": testDashboardToken})
	if code != http.StatusForbidden || !n.visionServiceSnapshot().Effective {
		t.Fatal("delegated manager changed service permissions using an extra header")
	}
}

func TestBuiltinVisionViewsRedactEndpointAndNeverProbeWhenDisabled(t *testing.T) {
	n, a := builtinServiceFixture(t, "neuro")
	var calls atomic.Int32
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer service.Close()
	endpoint := strings.Replace(service.URL, "http://", "http://fixture:secret@", 1) + "/describe?token=private#secret"
	t.Setenv("NEURO_VISION_URL", endpoint)
	for _, disabled := range []bool{false, true} {
		setBuiltinVision(t, n, !disabled, true, true)
		paths := []string{"/api/services/vision", "/api/extensions"}
		if disabled {
			paths = append(paths, "/api/runtime")
		}
		for _, path := range paths {
			code, body := adapterRequest(t, a, http.MethodGet, path, nil, map[string]string{"X-ND-Token": testDashboardToken})
			if code != http.StatusOK {
				t.Fatalf("view %s: %d %v", path, code, body)
			}
			encoded, _ := json.Marshal(body)
			if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") {
				t.Fatalf("view %s disclosed endpoint credentials", path)
			}
			if path == "/api/runtime" && body["vision"].(map[string]interface{})["state"] != "disabled" {
				t.Fatal("disabled service was advertised as running")
			}
			if path == "/api/extensions" && body["built_in_services"].(map[string]interface{})["vision"] == nil {
				t.Fatal("empty catalog did not expose the built-in Vision Service")
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("reading settings or disabled runtime contacted the external process")
	}
}

func TestBuiltinVisionDisabledBlocksCaptureButAllowsMetadata(t *testing.T) {
	t.Setenv("NEURO_BUILTIN_SERVICES_FILE", filepath.Join(t.TempDir(), "builtin-services.json"))
	n, executor := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) { return observationResponse() })
	setBuiltinVision(t, n, false, true, true)
	for _, call := range []func() error{
		func() error { _, err := n.getDesktopContext(true); return err },
		func() error { _, err := n.buildGameObservation(true, ""); return err },
		func() error {
			_, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus, Params: map[string]interface{}{"capture_screenshot": true}})
			return err
		},
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "Vision Service is disabled") {
			t.Fatalf("disabled screenshot service was bypassed: %v", err)
		}
	}
	if len(executor.seenCommands()) != 0 {
		t.Fatal("disabled capture reached the controlled computer")
	}
	if _, err := n.getDesktopContext(false); err != nil {
		t.Fatal(err)
	}
	if _, err := n.buildGameObservation(false, ""); err != nil {
		t.Fatal(err)
	}
	response, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus, Params: map[string]interface{}{"capture_screenshot": false}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Data["screenshot_png_b64"] != nil {
		t.Fatal("metadata-only reply retained an unrequested frame")
	}
	for _, value := range []interface{}{"true", "false", 1, nil, []interface{}{}, map[string]interface{}{}} {
		if _, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus, Params: map[string]interface{}{"capture_screenshot": value}}); err == nil {
			t.Fatalf("ambiguous capture value reached the executor: %#v", value)
		}
	}
	if len(executor.seenCommands()) != 3 {
		t.Fatal("only the three explicit metadata calls should reach the executor")
	}
	refuseObservation(n)
	if _, err := n.getDesktopContext(false); err == nil {
		t.Fatal("built-in metadata exception bypassed the global Vision permission")
	}
}

func TestBuiltinVisionQueuedCaptureCannotSurviveDisableAndReenable(t *testing.T) {
	t.Setenv("NEURO_BUILTIN_SERVICES_FILE", filepath.Join(t.TempDir(), "builtin-services.json"))
	n, executor := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) { return observationResponse() })
	permit, err := n.observationPermit(CmdGameObserve)
	if err != nil {
		t.Fatal(err)
	}
	if err := permit.requireVisionService(); err != nil {
		t.Fatal(err)
	}
	n.ipcMu.Lock()
	finished := make(chan error, 1)
	go func() { _, err := n.fetchDesktopStatusGuarded(true, permit.check); finished <- err }()
	setBuiltinVision(t, n, false, true, true)
	setBuiltinVision(t, n, true, true, true)
	n.ipcMu.Unlock()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("an old queued capture reused new service approval")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued capture did not finish")
	}
	if len(executor.seenCommands()) != 0 {
		t.Fatal("revoked queued capture reached the executor")
	}
}

func TestBuiltinVisionDisablingDropsCapturedFrameAndCancelsInference(t *testing.T) {
	for _, phase := range []string{"capture", "inference", "raw-status"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("NEURO_BUILTIN_SERVICES_FILE", filepath.Join(t.TempDir(), "builtin-services.json"))
			entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			n, _ := observationFixture(t, func(IPCCommand) (*IPCResponse, bool) {
				if phase != "inference" {
					close(entered)
					<-release
				}
				return observationResponse()
			})
			var uploads atomic.Int32
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploads.Add(1)
				if phase == "inference" {
					// A real inference service consumes its JSON request before
					// processing; this also permits net/http to notice disconnects.
					_, _ = io.Copy(io.Discard, r.Body)
					close(entered)
					select {
					case <-r.Context().Done():
						close(cancelled)
					case <-release:
					}
				}
				_, _ = fmt.Fprint(w, `{"summary":"private fixture inference"}`)
			}))
			defer service.Close()
			t.Setenv("NEURO_VISION_URL", service.URL)
			finished := make(chan error, 1)
			go func() {
				if phase == "raw-status" {
					response, err := n.sendToExecutor(IPCCommand{Type: CmdGetStatus, Params: map[string]interface{}{"capture_screenshot": true}})
					if response != nil {
						finished <- fmt.Errorf("late raw status frame was returned")
						return
					}
					finished <- err
					return
				}
				_, err := n.buildGameObservation(true, "")
				finished <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("observation did not enter controlled phase")
			}
			setBuiltinVision(t, n, false, true, true)
			setBuiltinVision(t, n, true, true, true)
			if phase == "inference" {
				select {
				case <-cancelled:
				case <-time.After(5 * time.Second):
					close(release)
					t.Fatal("disabling did not cancel the pending inference request")
				}
			}
			close(release)
			select {
			case err := <-finished:
				if err == nil || !strings.Contains(err.Error(), "settings changed") {
					t.Fatalf("old result survived service configuration change: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("revoked observation did not finish")
			}
			if phase != "inference" && uploads.Load() != 0 {
				t.Fatal("a frame captured after disable was uploaded")
			}
		})
	}
}
