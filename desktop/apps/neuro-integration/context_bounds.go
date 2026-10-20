package main

import (
	"crypto/sha256"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const contextMessageMaxBytes = 2 * 1024

// Context budgets are bytes, not model-specific token estimates. Preserve valid
// UTF-8 and reserve room for a visible marker rather than cutting inside a rune.
func boundedText(message string, maxBytes int) string {
	message = strings.ToValidUTF8(message, "�")
	if len(message) <= maxBytes {
		return message
	}
	const marker = "\n[truncated]"
	if maxBytes < len(marker) {
		return ""
	}
	end := maxBytes - len(marker)
	for end > 0 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + marker
}

func boundedContextMessage(message string) string {
	return boundedText(message, contextMessageMaxBytes)
}

// Window titles, process names and external summaries are observed data. Keep
// each value on one line, without control characters or injected Markdown rows.
func contextField(value string, maxBytes int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
	value = strings.Join(strings.Fields(value), " ")
	return strings.ReplaceAll(boundedText(value, maxBytes), "\n", " ")
}

type contextDeliveryState struct {
	mu        sync.Mutex
	last      [sha256.Size]byte
	hasLast   bool
	briefSent bool
}

// A polling update should describe a material change, not an endless stream of
// pointer positions, timestamps, action counters or screenshot sizes. Explicit
// observations bypass this filter and still answer a repeated request.
func automaticContextMessage(message string) string {
	lines := strings.Split(message, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "- Mouse:") ||
			strings.HasPrefix(line, "- Recent action events tracked:") ||
			strings.HasPrefix(line, "- Screenshot: captured") {
			continue
		}
		filtered = append(filtered, line)
	}
	return boundedContextMessage(strings.Join(filtered, "\n"))
}

// Only a successful delivery advances the fingerprint. The guard is checked
// after acquiring the delivery lock so queued private metadata cannot outlive
// its approval. The cache holds a digest, never window/process text.
func (s *contextDeliveryState) sendAutomatic(message string, guard func() error, send func(string) error) (bool, error) {
	message = automaticContextMessage(message)
	digest := sha256.Sum256([]byte(message))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := guard(); err != nil {
		return false, err
	}
	if s.hasLast && s.last == digest {
		return false, nil
	}
	if err := send(message); err != nil {
		return false, err
	}
	s.last, s.hasLast = digest, true
	return true, nil
}
