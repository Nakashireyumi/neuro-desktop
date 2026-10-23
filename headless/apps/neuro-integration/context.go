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
	lines := []string{"## Desktop context snapshot"}
	if platform, _ := status["platform"].(string); platform != "" {
		lines = append(lines, "- Platform: "+contextField(platform, 24))
	}
	if available, ok := status["input_available"].(bool); ok && !available {
		reason, _ := status["input_error"].(string)
		lines = append(lines, "- Desktop input: unavailable ("+contextField(reason, 144)+"). Use shell_command for approved command-line work.")
	}
	if capabilities := toMap(status["capabilities"]); capabilities != nil {
		if windows := toMap(capabilities["windows"]); windows != nil {
			if available, ok := windows["available"].(bool); ok && !available {
				reason, _ := windows["reason"].(string)
				if reason == "" {
					reason, _ = status["open_windows_error"].(string)
				}
				lines = append(lines, "- Window list: unavailable ("+contextField(reason, 120)+")")
			}
		}
	}
	if title := contextField(activeWindow, 160); title != "" {
		lines = append(lines, "- Active window: "+title)
	} else {
		lines = append(lines, "- Active window: unavailable")
	}
	if screen := toMap(status["screen"]); screen != nil {
		lines = append(lines, "- Screen: "+contextField(fmt.Sprintf("%vx%v", screen["width"], screen["height"]), 32))
	} else {
		lines = append(lines, "- Screen: unknown")
	}
	// Preserve the useful visual description before lower-priority samples when
	// the overall budget is reached. The model receives text, not image bytes.
	if summary := contextField(visionSummary, 768); summary != "" {
		lines = append(lines, "- Vision summary: "+summary)
	}
	if mouse := toMap(status["mouse_position"]); mouse != nil {
		lines = append(lines, "- Mouse: "+contextField(fmt.Sprintf("(%v, %v)", mouse["x"], mouse["y"]), 40))
	} else {
		lines = append(lines, "- Mouse: unknown")
	}
	for _, sample := range []struct {
		key, label        string
		count, fieldBytes int
	}{
		{"open_windows", "Open windows sample", 4, 64},
		{"running_processes", "Running processes sample", 6, 40},
	} {
		values := truncateList(toStringSlice(status[sample.key]), sample.count)
		parts := make([]string, 0, len(values))
		for _, value := range values {
			if field := contextField(value, sample.fieldBytes); field != "" {
				parts = append(parts, field)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, "- "+sample.label+": "+strings.Join(parts, " | "))
		}
	}
	if shot, ok, shotErr := screenshotPNG(status); shotErr != nil {
		lines = append(lines, "- Screenshot: unusable ("+contextField(shotErr.Error(), 120)+")")
	} else if ok {
		lines = append(lines, fmt.Sprintf("- Screenshot: captured (PNG, %d KB)", len(shot)/1024))
	} else if reason, _ := status["screenshot_error"].(string); reason != "" {
		lines = append(lines, "- Screenshot: unavailable ("+contextField(reason, 120)+")")
	}
	return boundedContextMessage(strings.Join(lines, "\n"))
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
	return n.deliverDesktopContext(captureScreenshot, silent, false)
}

func (n *NDIntegration) sendAutomaticDesktopContext(captureScreenshot bool) neuro.ExecutionResult {
	return n.deliverDesktopContext(captureScreenshot, true, true)
}

func (n *NDIntegration) deliverDesktopContext(captureScreenshot bool, silent bool, automatic bool) neuro.ExecutionResult {
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
	if automatic {
		sent, err := n.contextDelivery.sendAutomatic(message, permit.check, func(message string) error {
			return n.client.SendContext(message, true)
		})
		if err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to send context: %v", err))
		}
		if !sent {
			return neuro.NewSuccessResult("Desktop context unchanged")
		}
	} else if err := n.client.SendContext(boundedContextMessage(message), silent); err != nil {
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
				result := n.sendAutomaticDesktopContext(captureScreenshot)
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
