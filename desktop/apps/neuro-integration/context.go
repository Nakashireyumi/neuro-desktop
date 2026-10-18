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
	return n.fetchDesktopStatusGuarded(captureScreenshot)
}

func (n *NDIntegration) fetchDesktopStatusGuarded(captureScreenshot bool, guards ...func() error) (map[string]interface{}, error) {
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

	resp, err := n.sendToExecutor(cmd, guards...)
	if err != nil {
		return nil, fmt.Errorf("ipc error: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get_status failed: %s", nonEmptyOr(resp.Error, "executor returned no detail"))
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
	if available, ok := status["input_available"].(bool); ok && !available {
		reason, _ := status["input_error"].(string)
		lines = append(lines, "- Desktop input: unavailable ("+reason+"). Use shell_command for command-line work.")
	}
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

	if shot, ok, shotErr := screenshotPNG(status); shotErr != nil {
		lines = append(lines, fmt.Sprintf("- Screenshot: unusable (%v)", shotErr))
	} else if ok {
		lines = append(lines, fmt.Sprintf("- Screenshot: captured (PNG, %d KB)", len(shot)/1024))
	} else if reason, _ := status["screenshot_error"].(string); reason != "" {
		lines = append(lines, "- Screenshot: unavailable ("+reason+")")
	}

	if strings.TrimSpace(visionSummary) != "" {
		lines = append(lines, fmt.Sprintf("- Vision summary: %s", visionSummary))
	}

	return strings.Join(lines, "\n")
}

func (n *NDIntegration) getDesktopContext(captureScreenshot bool) (string, error) {
	permit, err := n.observationPermit(CmdGetDesktopContext)
	if err != nil {
		return "", err
	}
	return n.getDesktopContextGuarded(captureScreenshot, permit)
}

func (n *NDIntegration) getDesktopContextGuarded(captureScreenshot bool, permit *observationPermit) (string, error) {
	if captureScreenshot {
		if err := permit.requireVisionService(); err != nil {
			return "", err
		}
	}
	if err := permit.check(); err != nil {
		return "", err
	}
	status, err := n.fetchDesktopStatusGuarded(captureScreenshot, permit.check)
	if err != nil {
		return "", err
	}
	if err := permit.check(); err != nil {
		return "", err
	}

	visionSummary := ""
	if serverURL := visionServerURL(); captureScreenshot && serverURL != "" {
		shot, ok, shotErr := screenshotPNG(status)
		if shotErr != nil {
			log.Printf("Vision skipped: %v", shotErr)
		} else if ok {
			if err := permit.check(); err != nil {
				return "", err
			}
			summary, visionErr := summarizeWithVisionServerContext(permit.vision.ctx, serverURL, shot, "")
			if visionErr != nil {
				log.Printf("Vision summarize failed: %v", visionErr)
			} else {
				visionSummary = summary
			}
		}
	}

	if err := permit.check(); err != nil {
		return "", err
	}
	return buildDesktopContextMessage(status, visionSummary), nil
}

func (n *NDIntegration) sendDesktopContext(captureScreenshot bool, silent bool) neuro.ExecutionResult {
	permit, err := n.observationPermit(CmdSendDesktopContext)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	message, err := n.getDesktopContextGuarded(captureScreenshot, permit)
	if err != nil {
		return neuro.NewFailureResult(fmt.Sprintf("Failed to collect desktop context: %v", err))
	}

	if err := permit.check(); err != nil {
		return neuro.NewFailureResult(err.Error())
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
