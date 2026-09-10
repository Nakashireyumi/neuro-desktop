package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type visionRequest struct {
	ImagePath   string                 `json:"image_path"`
	ImageBase64 string                 `json:"image_base64,omitempty"`
	Prompt      string                 `json:"prompt"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func summarizeWithVisionServer(serverURL string, screenshotPath string) (string, error) {
	fileBytes, err := os.ReadFile(screenshotPath)
	if err != nil {
		return "", fmt.Errorf("failed to read screenshot: %w", err)
	}

	encodedImage := ""
	if len(fileBytes) <= 2*1024*1024 {
		encodedImage = base64.StdEncoding.EncodeToString(fileBytes)
	}

	requestPayload := visionRequest{
		ImagePath:   screenshotPath,
		ImageBase64: encodedImage,
		Prompt:      "Summarize what is happening on this Windows desktop for Neuro. Keep it concise.",
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

	resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read vision response: %w", err)
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
