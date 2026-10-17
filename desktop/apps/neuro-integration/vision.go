package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type visionRequest struct {
	// The image always travels as base64. The server never sends the vision
	// server a path, because the screenshot may come from another machine.
	ImageBase64 string                 `json:"image_base64"`
	Prompt      string                 `json:"prompt"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// maxVisionImageBytes matches nd-vision-server's limit, so a screenshot that fits
// is always sent inline (the server does not read paths unless it is told to).
const maxVisionImageBytes = 8 << 20

const maxVisionResponseBytes = 256 << 10

// visionServerURL returns the configured vision endpoint, if any. NEURO_VISION_URL
// is the canonical name; NEURO_VISION_SERVER_URL is still read so older setups keep
// working.
func visionServerURL() string {
	if url := strings.TrimSpace(os.Getenv("NEURO_VISION_URL")); url != "" {
		return url
	}
	return strings.TrimSpace(os.Getenv("NEURO_VISION_SERVER_URL"))
}

// visionToken is the optional bearer token the vision server expects.
func visionToken() string {
	return strings.TrimSpace(os.Getenv("NEURO_VISION_TOKEN"))
}

// visionStatus is what the dashboard shows about the vision server. It is a live
// probe, not a configuration echo.
type visionStatus struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url,omitempty"`
	Reachable  bool   `json:"reachable"`
	HTTPStatus int    `json:"http_status,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checked_at,omitempty"`
}

var (
	visionProbeMu    sync.Mutex
	visionProbeCache visionStatus
	visionProbeAt    time.Time
)

const visionProbeTTL = 10 * time.Second

// healthURLFor derives the health endpoint from the configured describe URL:
// http://host:8610/describe -> http://host:8610/health.
func healthURLFor(describeURL string) (string, error) {
	parsed, err := url.Parse(describeURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("NEURO_VISION_URL must be an http:// or https:// URL")
	}
	parsed.Path = "/health"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// probeVisionServer asks the configured vision server for its health. Results
// are cached briefly so a dashboard that polls does not hammer the server.
func probeVisionServer(force bool) visionStatus {
	visionProbeMu.Lock()
	defer visionProbeMu.Unlock()

	if !force && time.Since(visionProbeAt) < visionProbeTTL && visionProbeCache.CheckedAt != "" {
		return visionProbeCache
	}

	status := visionStatus{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	describeURL := visionServerURL()
	if describeURL == "" {
		visionProbeCache, visionProbeAt = status, time.Now()
		return status
	}
	status.Configured = true
	status.URL = describeURL

	healthURL, err := healthURLFor(describeURL)
	if err != nil {
		status.Error = err.Error()
		visionProbeCache, visionProbeAt = status, time.Now()
		return status
	}

	started := time.Now()
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodGet, healthURL, nil)
	if token := visionToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	status.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		status.Error = "not reachable: " + err.Error()
		visionProbeCache, visionProbeAt = status, time.Now()
		return status
	}
	defer resp.Body.Close()

	status.HTTPStatus = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusOK {
		status.Reachable = true
		var health struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal(body, &health) == nil {
			status.Backend = health.Backend
		}
	} else {
		status.Error = fmt.Sprintf("health check returned %d", resp.StatusCode)
	}
	visionProbeCache, visionProbeAt = status, time.Now()
	return status
}

// pngSignature is the first eight bytes of every PNG file.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// screenshotPNG returns the screenshot that a client sent with its status reply.
// The client sends PNG bytes and never a path. A screenshot_path field from an
// older client is ignored, so the server never opens a file that a client names.
// ok is false when there is no screenshot.
func screenshotPNG(status map[string]interface{}) (data []byte, ok bool, err error) {
	encoded, _ := status["screenshot_png_b64"].(string)
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, false, nil
	}
	data, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false, fmt.Errorf("the screenshot is not valid base64: %w", err)
	}
	if len(data) > maxVisionImageBytes {
		return nil, false, fmt.Errorf("the screenshot is %d bytes, over the %d byte limit", len(data), maxVisionImageBytes)
	}
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, false, fmt.Errorf("the screenshot is not a PNG")
	}
	return data, true, nil
}

// summarizeWithVisionServer sends PNG bytes to the vision server and returns its
// summary. The caller gets the bytes from screenshotPNG.
func summarizeWithVisionServer(serverURL string, png []byte, prompt string) (string, error) {
	if len(png) == 0 {
		return "", fmt.Errorf("there is no screenshot to summarize")
	}
	if len(png) > maxVisionImageBytes {
		return "", fmt.Errorf("the screenshot is %d bytes; the vision server takes at most %d", len(png), maxVisionImageBytes)
	}
	encodedImage := base64.StdEncoding.EncodeToString(png)

	if strings.TrimSpace(prompt) == "" {
		prompt = "Summarize what is happening on this desktop for Neuro. Keep it concise."
	}

	requestPayload := visionRequest{
		ImageBase64: encodedImage,
		Prompt:      prompt,
		Metadata: map[string]interface{}{
			"source": "neuro-desktop",
		},
	}

	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal vision payload: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("vision request could not be built: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := visionToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVisionResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read vision response: %w", err)
	}
	if len(body) > maxVisionResponseBytes {
		return "", fmt.Errorf("vision response exceeds %d bytes", maxVisionResponseBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision server returned %d: %s", resp.StatusCode, string(body))
	}

	summary := extractVisionSummary(body)
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("vision response did not include a summary")
	}

	return summary, nil
}

func extractVisionSummary(body []byte) string {
	var asMap map[string]interface{}
	if err := json.Unmarshal(body, &asMap); err != nil {
		return strings.TrimSpace(string(body))
	}

	candidates := []string{
		"summary",
		"description",
		"caption",
		"text",
		"result",
		"message",
	}

	for _, key := range candidates {
		if value, ok := asMap[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(decodeJSONStringIfNeeded(value))
		}
	}

	if nested, ok := asMap["data"].(map[string]interface{}); ok {
		for _, key := range candidates {
			if value, ok := nested[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(decodeJSONStringIfNeeded(value))
			}
		}
	}

	return ""
}
