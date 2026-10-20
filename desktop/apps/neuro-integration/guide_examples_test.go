package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Examples are copied into real run_script calls. Check the documented text
// blocks through the bridge's production grammar without executing OS input.
func TestDocumentedScriptExamplesParse(t *testing.T) {
	blocks := regexp.MustCompile("(?s)```text\\r?\\n(.*?)\\r?\\n```")
	paths := []string{
		filepath.Join("..", "..", "..", "docs", "action_script", "LANGUAGE_REFERENCE.md"),
		filepath.Join("integration-docs", "Action Script Documentation.md"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		examples := blocks.FindAllSubmatch(data, -1)
		if len(examples) == 0 {
			t.Fatalf("no examples in %s", path)
		}
		for index, example := range examples {
			if _, err := validateScript(string(example[1])); err != nil {
				t.Errorf("%s example %d is invalid: %v", path, index+1, err)
			}
		}
	}
}
