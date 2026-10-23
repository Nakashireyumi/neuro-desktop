package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Small-model evaluation for CI.
//
// Weak models make the same handful of mistakes: they leave a required field
// out, send a number as a string, use the wrong field name, or send a bare
// string where an object belongs. testdata/weak_model_cases.json records those
// calls as they were actually produced, with the verdict the bridge must give.
// A rejection must also say what to do next, so the model can correct itself
// on the next turn. The cases run without a model, so the check is cheap and
// deterministic in CI.

type weakModelCase struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Data     string `json:"data"`
	Accepted bool   `json:"accepted"`
	Contains string `json:"contains,omitempty"`
}

// weakEvalSpec finds an action the way registration does, so the cases test the
// real parameter schemas and the real corrective text.
func weakEvalSpec(name string) (actionSpec, bool) {
	all := append([]actionSpec{}, HLActionSpecs...)
	all = append(all, LLActionSpecs...)
	all = append(all, alwaysRegisteredSpecs()...)
	for _, spec := range all {
		if string(spec.Name) == name {
			return spec, true
		}
	}
	return actionSpec{}, false
}

// weakEvalAction is a permissive but real policy: input and vision are on,
// the shell is not (it needs explicit consent), and nothing is stopped.
func weakEvalAction(t *testing.T, spec actionSpec) *IPCProxyAction {
	t.Helper()
	integration := &NDIntegration{
		stats: newBridgeStats(),
		relay: newRelayState(RelayConfig{}),
		stop:  newStopSwitch(),
		audit: newAuditor(),
	}
	policy, err := loadPermissionPolicy(writeTempPolicy(t, `{
	  "default_allow": true,
	  "scopes": {"input": true, "vision": true}
	}`))
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	integration.setPolicy(policy)
	return &IPCProxyAction{integration: integration, spec: spec}
}

func TestWeakModelCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/weak_model_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []weakModelCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("the fixture file is not valid JSON: %v", err)
	}
	if len(cases) < 10 {
		t.Fatalf("the eval needs more cases than %d", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			spec, ok := weakEvalSpec(tc.Action)
			if !ok {
				t.Fatalf("no action named %q is registered", tc.Action)
			}
			action := weakEvalAction(t, spec)
			_, result := action.Validate(json.RawMessage(tc.Data))

			if result.Successful != tc.Accepted {
				t.Fatalf("accepted = %t, want %t; reply: %q", result.Successful, tc.Accepted, result.Message)
			}
			if !tc.Accepted {
				if strings.TrimSpace(result.Message) == "" {
					t.Fatal("a refusal must say why")
				}
				if tc.Contains != "" && !strings.Contains(result.Message, tc.Contains) {
					t.Fatalf("reply %q should contain %q", result.Message, tc.Contains)
				}
			}
		})
	}
}

// Every refusal the bridge gives a model must name a next step: the action to
// retry, the parameter to fix, or desktop_guide. A bare "invalid" leaves a small
// model guessing.
func TestRefusalsNameANextStep(t *testing.T) {
	raw, err := os.ReadFile("testdata/weak_model_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []weakModelCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if tc.Accepted {
			continue
		}
		spec, ok := weakEvalSpec(tc.Action)
		if !ok {
			continue
		}
		_, result := weakEvalAction(t, spec).Validate(json.RawMessage(tc.Data))
		msg := result.Message
		if !strings.Contains(msg, tc.Action) && !strings.Contains(msg, "desktop_guide") && !strings.Contains(msg, "\"") {
			t.Errorf("%s: refusal %q names neither the action, a parameter, nor desktop_guide", tc.Name, msg)
		}
	}
}

// The escape hatch is only an escape hatch if a restrictive policy cannot lock
// it, and if it still runs while the bridge is stopped.
func TestEscapeHatchSurvivesADenyAllPolicy(t *testing.T) {
	policy, err := loadPermissionPolicy(writeTempPolicy(t, `{
	  "default_allow": false,
	  "scopes": {"input": false, "vision": false, "shell": false}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reset_controls", "desktop_guide", "request_permission"} {
		if !policy.IsAllowed(name) {
			t.Errorf("%s must stay allowed under a deny-all policy", name)
		}
	}
	if policy.IsAllowed("move_mouse_to") {
		t.Fatal("the deny-all policy must still refuse mouse movement")
	}
}

func TestEscapeHatchRunsWhileStopped(t *testing.T) {
	if !safeDuringStop[string(CmdResetControls)] {
		t.Fatal("reset_controls must run while the bridge is paused or killed")
	}
}

func TestEscapeHatchTakesNoParameters(t *testing.T) {
	spec, ok := weakEvalSpec("reset_controls")
	if !ok {
		t.Fatal("reset_controls is not registered")
	}
	if spec.Schema != nil && len(spec.Schema.Properties) > 0 {
		t.Fatal("the escape hatch must take no parameters; a small model should not be able to get it wrong")
	}
	if !strings.Contains(spec.Description, "Example") {
		t.Fatal("the escape hatch description needs a worked example")
	}
}

func TestResetOutcomeSaysWhatWorkedAndWhatToDoNext(t *testing.T) {
	ok := combineResetResults(neuro.NewSuccessResult("a"), neuro.NewSuccessResult("b"))
	if !ok.Successful || !strings.Contains(ok.Message, "Controls reset") || !strings.Contains(ok.Message, "Next:") {
		t.Fatalf("success reply = %+v", ok)
	}

	partial := combineResetResults(neuro.NewSuccessResult("a"), neuro.NewFailureResult("Could not release input: executor offline"))
	if partial.Successful {
		t.Fatal("a failed release must not be reported as a success")
	}
	if !strings.Contains(partial.Message, "may still be down") || !strings.Contains(partial.Message, "executor offline") {
		t.Fatalf("partial reply should say what may still be held: %q", partial.Message)
	}
}
