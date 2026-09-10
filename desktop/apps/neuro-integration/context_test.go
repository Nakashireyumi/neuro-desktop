package main

import (
	"strings"
	"testing"
)

func TestBuildDesktopContextMessage(t *testing.T) {
	status := map[string]interface{}{
		"active_window": "Notepad",
		"open_windows":  []interface{}{"Notepad", "Explorer"},
		"running_processes": []interface{}{
			"explorer.exe",
			"notepad.exe",
		},
		"screen": map[string]interface{}{
			"width":  float64(1920),
			"height": float64(1080),
		},
		"mouse_position": map[string]interface{}{
			"x": float64(300),
			"y": float64(250),
		},
		"recent_actions": []interface{}{map[string]interface{}{"type": "TYPE"}},
	}

	msg := buildDesktopContextMessage(status, "A text editor is open")

	if !strings.Contains(msg, "Active window: Notepad") {
		t.Fatalf("missing active window info: %s", msg)
	}
	if !strings.Contains(msg, "Vision summary: A text editor is open") {
		t.Fatalf("missing vision summary: %s", msg)
	}
}
