package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

func TestActionParametersDeclaredConstraints(t *testing.T) {
	spec := actionSpec{Name: "example", Schema: neuro.WrapSchema(map[string]interface{}{
		"visible": map[string]interface{}{"type": "boolean"},
		"button":  map[string]interface{}{"type": "string", "enum": []string{"left", "right", "middle"}},
		"count":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": float64(4)},
		"delay":   map[string]interface{}{"type": "number", "minimum": 0.25, "maximum": 2},
		"text":    map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 3},
		"keys":    map[string]interface{}{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]interface{}{"type": "string"}},
		"choice":  map[string]interface{}{"type": "integer", "enum": []interface{}{1, 2}},
	}, nil)}
	for _, test := range []struct {
		name   string
		params map[string]interface{}
		want   string
	}{
		{"false is a boolean", map[string]interface{}{"visible": false}, ""},
		{"string false is not a boolean", map[string]interface{}{"visible": "false"}, `"visible"`},
		{"null is not an optional boolean", map[string]interface{}{"visible": nil}, "boolean"},
		{"button enum", map[string]interface{}{"button": "back"}, `"button"`},
		{"button numeric type", map[string]interface{}{"button": float64(1)}, "JSON string"},
		{"integer lower bound", map[string]interface{}{"count": float64(0)}, "at least 1"},
		{"integer upper bound", map[string]interface{}{"count": float64(5)}, "at most 4"},
		{"fractional integer", map[string]interface{}{"count": 1.5}, "whole number"},
		{"integer boundaries", map[string]interface{}{"count": float64(4)}, ""},
		{"numeric string", map[string]interface{}{"delay": "1"}, "finite number"},
		{"number lower bound", map[string]interface{}{"delay": 0.1}, "at least 0.25"},
		{"number upper bound", map[string]interface{}{"delay": 2.1}, "at most 2"},
		{"nonfinite number", map[string]interface{}{"delay": math.Inf(1)}, "finite number"},
		{"empty bounded string", map[string]interface{}{"text": ""}, "at least 1 characters"},
		{"long string", map[string]interface{}{"text": "four"}, "at most 3 characters"},
		{"unicode counts characters", map[string]interface{}{"text": "猫猫猫"}, ""},
		{"empty array", map[string]interface{}{"keys": []interface{}{}}, "at least 1 items"},
		{"long array", map[string]interface{}{"keys": []interface{}{"a", "b", "c"}}, "at most 2 items"},
		{"typed array item", map[string]interface{}{"keys": []interface{}{"a", true}}, "keys[1]"},
		{"valid array", map[string]interface{}{"keys": []interface{}{"a", "b"}}, ""},
		{"numeric enum supports Go declarations", map[string]interface{}{"choice": float64(2)}, ""},
		{"numeric enum rejects other choices", map[string]interface{}{"choice": float64(3)}, "one of: 1, 2"},
		{"unknown properties remain compatible", map[string]interface{}{"unknown": map[string]interface{}{"private": "value"}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			reason := validateActionParameters(spec, test.params)
			if test.want == "" {
				if reason != "" {
					t.Fatal(reason)
				}
			} else if !strings.Contains(reason, test.want) || !strings.Contains(reason, "Retry") {
				t.Fatalf("expected an actionable correction containing %q, got %q", test.want, reason)
			}
		})
	}
}

func TestActionParametersRequiredNestedAndUnknownFeatures(t *testing.T) {
	spec := actionSpec{Name: "tool", Kind: "mcp", Schema: neuro.WrapSchema(map[string]interface{}{
		"options": map[string]interface{}{
			"type": "object", "required": []interface{}{"values"}, "additionalProperties": false,
			"properties": map[string]interface{}{"values": map[string]interface{}{
				"type": "array", "items": map[string]interface{}{"type": "integer", "minimum": 0},
			}},
		},
		"variant": map[string]interface{}{"oneOf": []interface{}{map[string]interface{}{"type": "string"}}},
	}, []string{"options"})}
	for _, test := range []struct {
		data, want string
	}{
		{`{}`, `"options" is required`},
		{`{"options":{}}`, `"options.values" is required`},
		{`{"options":{"values":[1,-1]}}`, `"options.values[1]" must be at least 0`},
		{`{"options":{"values":[1,2],"future":true},"variant":42}`, ""},
	} {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(test.data), &params); err != nil {
			t.Fatal(err)
		}
		reason := validateActionParameters(spec, params)
		if test.want == "" && reason != "" || test.want != "" && !strings.Contains(reason, test.want) {
			t.Fatalf("wanted correction %q, got %q", test.want, reason)
		}
	}
}

func TestActionParametersIPCFlagsAndSafetyCompatibility(t *testing.T) {
	parameterized := actionSpec{Name: CmdTypeText, Schema: neuro.WrapSchema(map[string]interface{}{"text": map[string]interface{}{"type": "string"}}, nil)}
	for _, key := range []string{"execute_now", "clear_after"} {
		if reason := validateActionParameters(parameterized, map[string]interface{}{key: "false"}); !strings.Contains(reason, key) || !strings.Contains(reason, "boolean") {
			t.Fatalf("mistyped %s was not corrected: %q", key, reason)
		}
		if reason := validateActionParameters(parameterized, map[string]interface{}{key: false}); reason != "" {
			t.Fatal(reason)
		}
	}
	for _, schema := range []*neuro.ActionSchema{nil, neuro.WrapSchema(nil, nil)} {
		if reason := validateActionParameters(actionSpec{Name: CmdResetControls, Schema: schema}, map[string]interface{}{"execute_now": "junk", "clear_after": []interface{}{true}}); reason != "" {
			t.Fatalf("no-parameter safety action must ignore extra fields: %s", reason)
		}
	}
	parameterized.Kind = "mcp"
	if reason := validateActionParameters(parameterized, map[string]interface{}{"execute_now": "tool-specific value"}); reason != "" {
		t.Fatalf("undeclared MCP fields must not gain IPC semantics: %s", reason)
	}
	parameterized.Schema.Properties["execute_now"] = map[string]interface{}{"type": "string"}
	if reason := validateActionParameters(parameterized, map[string]interface{}{"execute_now": "declared tool value"}); reason != "" {
		t.Fatalf("MCP fields follow their declared schema: %s", reason)
	}
}

func TestActionParametersMCPDeclaredFlagsFollowSchema(t *testing.T) {
	spec := actionSpec{Name: "mcp_fixture", Kind: "mcp", Schema: neuro.WrapSchema(map[string]interface{}{
		"execute_now": map[string]interface{}{"type": "string", "minLength": 1},
		"clear_after": map[string]interface{}{"type": "boolean"},
	}, []string{"execute_now", "clear_after"})}
	for _, test := range []struct {
		name, data, want string
	}{
		{"required false is present", `{"execute_now":"tool-owned value","clear_after":false}`, ""},
		{"declared string rejects bool", `{"execute_now":true,"clear_after":false}`, `"execute_now" must be a JSON string`},
		{"declared bool rejects string", `{"execute_now":"tool-owned value","clear_after":"false"}`, `"clear_after" must be a boolean`},
		{"required property missing", `{"execute_now":"tool-owned value"}`, `"clear_after" is required`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var params map[string]interface{}
			if err := json.Unmarshal([]byte(test.data), &params); err != nil {
				t.Fatal(err)
			}
			reason := validateActionParameters(spec, params)
			if test.want == "" && reason != "" || test.want != "" && !strings.Contains(reason, test.want) {
				t.Fatalf("wanted correction %q, got %q", test.want, reason)
			}
		})
	}
}

func TestActionParametersObjectAndPrimitiveTypes(t *testing.T) {
	spec := actionSpec{Name: "tool", Kind: "mcp", Schema: neuro.WrapSchema(map[string]interface{}{
		"object": map[string]interface{}{"type": "object"},
		"array":  map[string]interface{}{"type": "array"},
		"null":   map[string]interface{}{"type": "null"},
		// Union types and schema composition are intentionally left to the
		// external tool, rather than rejecting an otherwise compatible call.
		"future": map[string]interface{}{"type": []interface{}{"string", "null"}, "oneOf": []interface{}{map[string]interface{}{"type": "string"}}},
	}, nil)}
	for _, test := range []struct {
		data, want string
	}{
		{`{"object":[],"array":[],"null":null}`, `"object" must be a JSON object`},
		{`{"object":{},"array":{},"null":null}`, `"array" must be a JSON array`},
		{`{"object":{},"array":[],"null":false}`, `"null" must be null`},
		{`{"object":{},"array":[],"null":null,"future":42}`, ""},
	} {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(test.data), &params); err != nil {
			t.Fatal(err)
		}
		reason := validateActionParameters(spec, params)
		if test.want == "" && reason != "" || test.want != "" && !strings.Contains(reason, test.want) {
			t.Fatalf("wanted correction %q, got %q", test.want, reason)
		}
	}
}

func TestActionParametersCorrectionsNeverEchoSuppliedCredentials(t *testing.T) {
	spec := actionSpec{Name: "tool", Kind: "mcp", Schema: neuro.WrapSchema(map[string]interface{}{
		"credential": map[string]interface{}{"type": "string", "enum": []string{"internal-secret-choice"}},
	}, nil)}
	secret := "private-test-token-1234567890"
	reason := validateActionParameters(spec, map[string]interface{}{"credential": secret})
	if !strings.Contains(reason, "credential") || strings.Contains(reason, secret) || strings.Contains(reason, "internal-secret-choice") {
		t.Fatalf("correction should name the field without repeating its contents: %q", reason)
	}
}

func TestActionParametersEnumCorrectionsGiveBoundedDeclaredChoices(t *testing.T) {
	spec := actionSpec{Name: CmdMouseClick, Schema: neuro.WrapSchema(map[string]interface{}{
		"button": map[string]interface{}{"type": "string", "enum": []string{"left", "right", "middle"}},
	}, nil)}
	secret := "private-test-token-1234567890"
	reason := validateActionParameters(spec, map[string]interface{}{"button": secret})
	for _, choice := range []string{`"left"`, `"right"`, `"middle"`} {
		if !strings.Contains(reason, choice) {
			t.Fatalf("correction must show an available button, got %q", reason)
		}
	}
	if strings.Contains(reason, secret) {
		t.Fatal("enum correction repeated the invalid supplied value")
	}
	choices := make([]interface{}, 1000)
	for index := range choices {
		choices[index] = "choice-" + strings.Repeat("x", index%32)
	}
	reason = actionEnumCorrection("option", choices)
	if len(reason) > 200 || !strings.Contains(reason, "such as:") || !strings.Contains(reason, `"choice-"`) {
		t.Fatalf("large enum should give a short sample of valid choices: %q", reason)
	}
	for _, field := range []string{"credential", "password", "options.api_key", "accessToken", "private_key", "authorization"} {
		if reason := actionEnumCorrection(field, []interface{}{"internal-secret-choice"}); strings.Contains(reason, "internal-secret-choice") {
			t.Fatalf("sensitive field %q exposed a schema credential", field)
		}
	}
	for _, sensitiveSchema := range []map[string]interface{}{
		{"type": "string", "enum": []string{"internal-secret-choice"}, "format": "password"},
		{"type": "string", "enum": []string{"internal-secret-choice"}, "writeOnly": true},
	} {
		reason := validateActionValue("private-test-token", sensitiveSchema, "option", 0)
		if strings.Contains(reason, "internal-secret-choice") || strings.Contains(reason, "private-test-token") {
			t.Fatalf("sensitive schema annotation exposed a credential: %q", reason)
		}
	}
}

func TestActionParametersMCPNamesDoNotGainDesktopCorrections(t *testing.T) {
	spec := actionSpec{Name: CmdTypeText, Kind: "mcp", Schema: neuro.WrapSchema(map[string]interface{}{
		"text": map[string]interface{}{"type": "integer"},
	}, nil)}
	if reason := validateActionParameters(spec, map[string]interface{}{"text": 1}); reason != "" {
		t.Fatal(reason)
	}
	reason := validateActionParameters(spec, map[string]interface{}{"text": true})
	if !strings.Contains(reason, "whole number") || strings.Contains(reason, "non-empty") {
		t.Fatalf("MCP-owned parameters must follow their schema: %q", reason)
	}
}

func TestActionParametersRejectMalformedCallsBeforeExecutorIO(t *testing.T) {
	for _, test := range []struct{ name, data, want string }{
		{"type_text", `{"text":"hello","execute_now":"false"}`, "execute_now"},
		{"type_text", `{"text":"hello","clear_after":0}`, "clear_after"},
		{"mouse_click", `{"button":"private-test-token-1234567890"}`, "button"},
		{"set_viewer_visibility", `{"integration_id":"fixture","actor":"neuro","visible":false,"minutes":61}`, "minutes"},
	} {
		t.Run(test.name+test.want, func(t *testing.T) {
			spec, found := weakEvalSpec(test.name)
			if !found {
				t.Fatalf("missing action %s", test.name)
			}
			action := weakEvalAction(t, spec)
			if spec.Name == CmdViewerVisibility {
				policy, err := loadPermissionPolicy(writeTempPolicy(t, `{"scopes":{"extensions":true}}`))
				if err != nil {
					t.Fatal(err)
				}
				action.integration.setPolicy(policy)
			}
			action.integration.ipcFilePath = filepath.Join(t.TempDir(), "untouched-ipc.json")
			work, result := action.Validate(json.RawMessage(test.data))
			if result.Successful || work != nil || !strings.Contains(result.Message, test.want) {
				t.Fatalf("malformed action was not rejected before execution: work=%T result=%+v", work, result)
			}
			if strings.Contains(result.Message, "private-test-token") {
				t.Fatal("supplied credential leaked into a correction")
			}
			if _, err := os.Stat(action.integration.ipcFilePath); !os.IsNotExist(err) {
				t.Fatalf("validation touched the executor path: %v", err)
			}
		})
	}
}

func TestActionParametersExplicitFalsePreservesDeferredInput(t *testing.T) {
	spec, found := weakEvalSpec("type_text")
	if !found {
		t.Fatal("missing type_text action")
	}
	action := weakEvalAction(t, spec)
	work, result := action.Validate(json.RawMessage(`{"text":"hello","execute_now":false,"clear_after":false}`))
	pending, ok := work.(pendingWork)
	if !result.Successful || !ok || pending.cmd == nil {
		t.Fatalf("valid explicit false must be accepted: %s", result.Message)
	}
	if pending.cmd.ExecuteNow || pending.cmd.ClearAfter {
		t.Fatal("explicit false was replaced by immediate/clear defaults")
	}
}
