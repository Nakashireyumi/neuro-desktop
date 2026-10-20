package main

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// validateActionParameters checks the schema subset used by action definitions.
// It returns one correction, never a supplied value. Unknown properties and
// schema keywords remain compatible with external tools. Proxy actions perform
// their more specific missing/empty-parameter correction before calling this.
func validateActionParameters(spec actionSpec, params map[string]interface{}) string {
	if spec.Schema == nil {
		// Safety actions intentionally accept and ignore extra parameters.
		return ""
	}
	if reason := validateActionObject(params, spec.Schema.Properties, spec.Schema.Required, "", 0); reason != "" {
		// Keep the practical examples used for the two most common desktop
		// shape mistakes. These corrections describe the expected value only;
		// external MCP tools continue to follow their own schema and names.
		if spec.Kind != "mcp" {
			switch spec.Name {
			case CmdTypeText:
				if value, present := params["text"]; present {
					if _, correctType := value.(string); !correctType {
						return `type_text needs a non-empty "text" string, for example {"text":"hello"}. Retry with corrected parameters.`
					}
				}
			case CmdMouseMove:
				for _, key := range []string{"x", "y"} {
					if value, present := params[key]; present {
						if _, numeric := actionSchemaNumber(value); !numeric {
							return `move_mouse_to needs numeric x and y in screen pixels, for example {"x":640,"y":360}. Retry with corrected parameters.`
						}
					}
				}
			}
		}
		return fmt.Sprintf("%s: %s Retry with corrected parameters.", spec.Name, reason)
	}
	// These IPC flags are optional even on definitions that omit them. They
	// must never silently turn a mistyped "false" string into immediate input.
	// MCP tools own their parameter names and do not use these IPC defaults.
	if spec.Kind != "mcp" && len(spec.Schema.Properties) > 0 {
		for _, key := range []string{"execute_now", "clear_after"} {
			if _, declared := spec.Schema.Properties[key]; declared {
				continue
			}
			if value, present := params[key]; present {
				if _, ok := value.(bool); !ok {
					return fmt.Sprintf("%s needs %q to be a boolean (true or false). Retry with corrected parameters.", spec.Name, key)
				}
			}
		}
	}
	return ""
}

func validateActionObject(values map[string]interface{}, properties map[string]interface{}, required []string, prefix string, depth int) string {
	for _, key := range required {
		if _, present := values[key]; !present {
			return fmt.Sprintf("%q is required.", prefix+key)
		}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys) // Multiple mistakes should produce a stable first correction.
	for _, key := range keys {
		value, present := values[key]
		schema, known := properties[key].(map[string]interface{})
		if !present || !known {
			continue
		}
		if reason := validateActionValue(value, schema, prefix+key, depth); reason != "" {
			return reason
		}
	}
	return ""
}

func validateActionValue(value interface{}, schema map[string]interface{}, path string, depth int) string {
	if depth > 16 {
		return fmt.Sprintf("%q is nested too deeply; use a simpler value.", path)
	}
	typeName, _ := schema["type"].(string)
	number, numeric := actionSchemaNumber(value)
	typeOK := true
	switch typeName {
	case "boolean":
		_, typeOK = value.(bool)
	case "string":
		_, typeOK = value.(string)
	case "number":
		typeOK = numeric
	case "integer":
		typeOK = numeric && math.Trunc(number) == number
	case "array":
		_, typeOK = value.([]interface{})
	case "object":
		_, typeOK = value.(map[string]interface{})
	case "null":
		typeOK = value == nil
	}
	if !typeOK {
		return fmt.Sprintf("%q must be %s.", path, actionTypeHint(typeName))
	}
	if choices, ok := actionSchemaChoices(schema["enum"]); ok {
		found := false
		for _, choice := range choices {
			other, otherNumeric := actionSchemaNumber(choice)
			if (numeric && otherNumeric && number == other) || reflect.DeepEqual(choice, value) {
				found = true
				break
			}
		}
		if !found {
			if schema["writeOnly"] == true || schema["format"] == "password" {
				return fmt.Sprintf("%q must use one of its declared enum values.", path)
			}
			return actionEnumCorrection(path, choices)
		}
	}
	if numeric {
		if minimum, ok := actionSchemaNumber(schema["minimum"]); ok && number < minimum {
			return fmt.Sprintf("%q must be at least %s.", path, strconv.FormatFloat(minimum, 'g', -1, 64))
		}
		if maximum, ok := actionSchemaNumber(schema["maximum"]); ok && number > maximum {
			return fmt.Sprintf("%q must be at most %s.", path, strconv.FormatFloat(maximum, 'g', -1, 64))
		}
	}
	switch typed := value.(type) {
	case string:
		length := float64(utf8.RuneCountInString(typed))
		if minimum, ok := actionSchemaNumber(schema["minLength"]); ok && length < minimum {
			return fmt.Sprintf("%q must contain at least %s characters.", path, strconv.FormatFloat(minimum, 'g', -1, 64))
		}
		if maximum, ok := actionSchemaNumber(schema["maxLength"]); ok && length > maximum {
			return fmt.Sprintf("%q must contain at most %s characters.", path, strconv.FormatFloat(maximum, 'g', -1, 64))
		}
	case []interface{}:
		length := float64(len(typed))
		if minimum, ok := actionSchemaNumber(schema["minItems"]); ok && length < minimum {
			return fmt.Sprintf("%q must contain at least %s items.", path, strconv.FormatFloat(minimum, 'g', -1, 64))
		}
		if maximum, ok := actionSchemaNumber(schema["maxItems"]); ok && length > maximum {
			return fmt.Sprintf("%q must contain at most %s items.", path, strconv.FormatFloat(maximum, 'g', -1, 64))
		}
		if items, ok := schema["items"].(map[string]interface{}); ok {
			for index, item := range typed {
				if reason := validateActionValue(item, items, fmt.Sprintf("%s[%d]", path, index), depth+1); reason != "" {
					return reason
				}
			}
		}
	case map[string]interface{}:
		properties, _ := schema["properties"].(map[string]interface{})
		required, _ := actionSchemaChoices(schema["required"])
		var keys []string
		for _, raw := range required {
			if key, ok := raw.(string); ok {
				keys = append(keys, key)
			}
		}
		return validateActionObject(typed, properties, keys, path+".", depth+1)
	}
	return ""
}

func actionEnumCorrection(path string, choices []interface{}) string {
	fallback := fmt.Sprintf("%q must use one of its declared enum values.", path)
	// Tool schemas can contain credential choices. Do not repeat them in
	// action results, which may be displayed or retained as model context.
	field := strings.ToLower(path)
	for _, sensitive := range []string{"credential", "password", "secret", "token", "auth", "api_key", "apikey", "access_key", "accesskey", "private_key", "privatekey"} {
		if strings.Contains(field, sensitive) {
			return fallback
		}
	}
	const maxChoices, maxHintBytes = 6, 128
	hints := make([]string, 0, min(len(choices), maxChoices))
	complete := true
	bytesUsed := 0
	for _, choice := range choices {
		if len(hints) == maxChoices {
			complete = false
			break
		}
		var hint string
		switch typed := choice.(type) {
		case string:
			if utf8.RuneCountInString(typed) <= 32 {
				hint = strconv.Quote(typed)
			}
		case bool:
			hint = strconv.FormatBool(typed)
		case nil:
			hint = "null"
		default:
			if number, numeric := actionSchemaNumber(choice); numeric {
				hint = strconv.FormatFloat(number, 'g', -1, 64)
			}
		}
		if hint == "" || len(hint) > 64 {
			complete = false
			continue
		}
		if bytesUsed+len(hint)+2 > maxHintBytes {
			complete = false
			break
		}
		hints = append(hints, hint)
		bytesUsed += len(hint) + 2
	}
	if len(hints) == 0 {
		return fallback
	}
	if complete {
		return fmt.Sprintf("%q must be one of: %s.", path, strings.Join(hints, ", "))
	}
	return fmt.Sprintf("%q must use a declared value, such as: %s.", path, strings.Join(hints, ", "))
}

func actionSchemaNumber(value interface{}) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case json.Number:
		var err error
		number, err = typed.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func actionSchemaChoices(value interface{}) ([]interface{}, bool) {
	switch typed := value.(type) {
	case []interface{}:
		return typed, true
	case []string:
		choices := make([]interface{}, len(typed))
		for index, choice := range typed {
			choices[index] = choice
		}
		return choices, true
	default:
		return nil, false
	}
}

func actionTypeHint(typeName string) string {
	switch typeName {
	case "boolean":
		return "a boolean (true or false)"
	case "integer":
		return "an integer (a whole number)"
	case "number":
		return "a finite number"
	case "null":
		return "null"
	default:
		return "a JSON " + strings.TrimSpace(typeName)
	}
}
