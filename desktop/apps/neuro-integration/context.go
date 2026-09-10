package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

func getEnvInt(name string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvBool(name string, defaultValue bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return defaultValue
	}

	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func truncateList(input []string, max int) []string {
	if max <= 0 || len(input) <= max {
		return input
	}
	return input[:max]
}

func toStringSlice(raw interface{}) []string {
	values, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, text)
		}
	}
	return result
}

func toMap(raw interface{}) map[string]interface{} {
	value, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return value
}

func (n *NDIntegration) fetchDesktopStatus(captureScreenshot bool) (map[string]interface{}, error) {
	cmd := IPCCommand{
		Type: CmdGetStatus,
		Params: map[string]interface{}{
			"capture_screenshot": captureScreenshot,
			"max_open_windows":   15,
			"max_processes":      20,
			"max_actions":        20,
		},
		ExecuteNow: true,
		ClearAfter: false,
	}

	resp, err := n.sendToRust(cmd)
	if err != nil {
		return nil, fmt.Errorf("ipc error: %w", err)
	}
	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf(resp.Error)
		}
		return nil, fmt.Errorf("get_status failed")
	}

	if resp.Data == nil {
		return map[string]interface{}{}, nil
	}
	return resp.Data, nil
}

func buildDesktopContextMessage(status map[string]interface{}, visionSummary string) string {
	activeWindow, _ := status["active_window"].(string)
	openWindows := truncateList(toStringSlice(status["open_windows"]), 6)
	runningProcesses := truncateList(toStringSlice(status["running_processes"]), 8)

	screenInfo := "unknown"
	if screen := toMap(status["screen"]); screen != nil {
		width, _ := screen["width"]
		height, _ := screen["height"]
		if width != nil && height != nil {
			screenInfo = fmt.Sprintf("%vx%v", width, height)
		}
	}

	mouseInfo := "unknown"
	if mouse := toMap(status["mouse_position"]); mouse != nil {
		x, hasX := mouse["x"]
		y, hasY := mouse["y"]
		if hasX && hasY {
			mouseInfo = fmt.Sprintf("(%v, %v)", x, y)
		}
	}

	recentActionCount := 0
	if recentActions, ok := status["recent_actions"].([]interface{}); ok {
		recentActionCount = len(recentActions)
	}

	var lines []string
	lines = append(lines, "## Desktop context snapshot")
	if strings.TrimSpace(activeWindow) == "" {
		lines = append(lines, "- Active window: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("- Active window: %s", activeWindow))
	}
	lines = append(lines, fmt.Sprintf("- Screen: %s", screenInfo))
	lines = append(lines, fmt.Sprintf("- Mouse: %s", mouseInfo))
	lines = append(lines, fmt.Sprintf("- Recent action events tracked: %d", recentActionCount))

	if len(openWindows) > 0 {
		lines = append(lines, fmt.Sprintf("- Open windows sample: %s", strings.Join(openWindows, " | ")))
	}
	if len(runningProcesses) > 0 {
		lines = append(lines, fmt.Sprintf("- Running processes sample: %s", strings.Join(runningProcesses, ", ")))
	}

	if screenshotPath, ok := status["screenshot_path"].(string); ok && strings.TrimSpace(screenshotPath) != "" {
		lines = append(lines, fmt.Sprintf("- Screenshot path: %s", screenshotPath))
	}

	if strings.TrimSpace(visionSummary) != "" {
		lines = append(lines, fmt.Sprintf("- Vision summary: %s", visionSummary))
	}

	return strings.Join(lines, "\n")
}

func (n *NDIntegration) getDesktopContext(captureScreenshot bool) (string, error) {
	status, err := n.fetchDesktopStatus(captureScreenshot)
	if err != nil {
		return "", err
	}

	visionSummary := ""
	if visionServerURL := strings.TrimSpace(os.Getenv("NEURO_VISION_SERVER_URL")); visionServerURL != "" {
		if screenshotPath, ok := status["screenshot_path"].(string); ok && strings.TrimSpace(screenshotPath) != "" {
			summary, visionErr := summarizeWithVisionServer(visionServerURL, screenshotPath)
			if visionErr != nil {
				log.Printf("Vision summarize failed: %v", visionErr)
			} else {
				visionSummary = summary
			}
		}
	}

	return buildDesktopContextMessage(status, visionSummary), nil
}

func (n *NDIntegration) sendDesktopContext(captureScreenshot bool, silent bool) neuro.ExecutionResult {
	message, err := n.getDesktopContext(captureScreenshot)
	if err != nil {
		return neuro.NewFailureResult(fmt.Sprintf("Failed to collect desktop context: %v", err))
	}

	if err := n.client.SendContext(message, silent); err != nil {
		return neuro.NewFailureResult(fmt.Sprintf("Failed to send context: %v", err))
	}

	return neuro.NewSuccessResult("Desktop context sent")
}

func (n *NDIntegration) startContextLoop() {
	intervalSeconds := getEnvInt("NEURO_CONTEXT_POLL_SECONDS", 0)
	if intervalSeconds <= 0 {
		return
	}

	captureScreenshot := getEnvBool("NEURO_CONTEXT_CAPTURE_SCREENSHOT", false)

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				result := n.sendDesktopContext(captureScreenshot, true)
				if !result.Successful {
					log.Printf("Desktop context loop send failed: %s", result.Message)
				}
			case <-n.contextStopChan:
				return
			}
		}
	}()
}

func decodeJSONStringIfNeeded(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	var decoded string
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}

	return trimmed
}
