package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

func setGuideTestMode(t *testing.T, high, low bool) {
	t.Helper()
	actionModeMu.Lock()
	oldHigh, oldLow, oldGames := RegisterHLActionsOnStartup, RegisterLLActionsOnStartup, RegisterGameActionsOnStartup
	RegisterHLActionsOnStartup, RegisterLLActionsOnStartup, RegisterGameActionsOnStartup = high, low, true
	actionModeMu.Unlock()
	t.Cleanup(func() {
		actionModeMu.Lock()
		RegisterHLActionsOnStartup, RegisterLLActionsOnStartup, RegisterGameActionsOnStartup = oldHigh, oldLow, oldGames
		actionModeMu.Unlock()
	})
}

// Exercise the real SDK/WebSocket boundary: a help unit test alone cannot prove
// that registration arrives before the brief, or that later registration does
// not resend context Neuro already remembers.
func TestStartupContextIsCompactRegisteredFirstAndSentOnce(t *testing.T) {
	for _, mode := range []struct {
		name      string
		high, low bool
	}{{"low-level", false, true}, {"high-level", true, false}} {
		t.Run(mode.name, func(t *testing.T) {
			setGuideTestMode(t, mode.high, mode.low)
			t.Setenv("NEURO_PROMPT_ASSIST", "true")
			t.Setenv("NEURO_CONTEXT_POLL_SECONDS", "0")
			h := newBridgeHarness(t, allowEverythingPolicy)
			brief := h.backend.waitForContextContaining("Neuro Desktop connected", time.Second)
			if len(brief) < 600 || len(brief) > 900 || !utf8.ValidString(brief) {
				t.Fatalf("startup brief must stay within the small-context budget: %d bytes", len(brief))
			}
			if err := h.integration.registerActions(); err != nil {
				t.Fatal(err)
			}
			h.integration.sendStartupGuide()
			const barrier = "startup test barrier"
			if err := h.integration.client.SendContext(barrier, true); err != nil {
				t.Fatal(err)
			}
			h.backend.waitForContextContaining(barrier, time.Second)
			h.backend.mu.Lock()
			messages := append([]map[string]interface{}{}, h.backend.inbox...)
			h.backend.mu.Unlock()
			registrations, briefs, contextActions := 0, 0, 0
			for _, message := range messages {
				data, _ := message["data"].(map[string]interface{})
				switch message["command"] {
				case "actions/register":
					registrations++
					if registrations == 1 {
						for _, item := range data["actions"].([]interface{}) {
							action := item.(map[string]interface{})
							if action["name"] == string(CmdGetDesktopContext) {
								contextActions++
							}
						}
					}
				case "context":
					body, _ := data["message"].(string)
					if body == barrier {
						continue
					}
					briefs++
					if registrations == 0 || body != brief {
						t.Fatalf("unexpected context before registration or a full/manual repeat: %q", body)
					}
				}
			}
			if briefs != 1 || contextActions != 1 || registrations != 2 {
				t.Fatalf("briefs=%d, first registration get_desktop_context=%d, registrations=%d", briefs, contextActions, registrations)
			}
		})
	}
}

func TestPromptAssistOptOutSendsNoStartupManualOrBrief(t *testing.T) {
	t.Setenv("NEURO_PROMPT_ASSIST", "false")
	t.Setenv("NEURO_CONTEXT_POLL_SECONDS", "0")
	h := newBridgeHarness(t, allowEverythingPolicy)
	const barrier = "opt out test barrier"
	if err := h.integration.client.SendContext(barrier, true); err != nil {
		t.Fatal(err)
	}
	h.backend.waitForContextContaining(barrier, time.Second)
	h.backend.mu.Lock()
	defer h.backend.mu.Unlock()
	for _, message := range h.backend.inbox {
		if message["command"] == "context" {
			data := message["data"].(map[string]interface{})
			if data["message"] != barrier {
				t.Fatalf("prompt assistance was disabled, but a context was sent: %v", data)
			}
		}
	}
}

func TestStartupBriefFailedSendCanRetryAfterConnect(t *testing.T) {
	t.Setenv("NEURO_PROMPT_ASSIST", "true")
	backend := startFakeNeuro(t)
	client, err := neuro.NewClient(neuro.ClientConfig{Game: "brief retry test", WebsocketURL: backend.url()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	n := &NDIntegration{client: client}
	n.sendStartupGuide()
	if n.contextDelivery.briefSent {
		t.Fatal("a disconnected send must not consume the startup brief")
	}
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	n.sendStartupGuide()
	backend.waitForContextContaining("Neuro Desktop connected", time.Second)
	if !n.contextDelivery.briefSent {
		t.Fatal("a successful retry was not remembered")
	}
}

func TestExplicitContextRemainsFreshAfterAutomaticDeduplication(t *testing.T) {
	t.Setenv("NEURO_CONTEXT_POLL_SECONDS", "0")
	h := newBridgeHarness(t, allowEverythingPolicy)
	for _, automatic := range []bool{true, true, false, false} {
		result := h.integration.deliverDesktopContext(false, true, automatic)
		if !result.Successful {
			t.Fatalf("context failed: %+v", result)
		}
	}
	const barrier = "fresh observation test barrier"
	if err := h.integration.client.SendContext(barrier, true); err != nil {
		t.Fatal(err)
	}
	h.backend.waitForContextContaining(barrier, time.Second)
	h.backend.mu.Lock()
	defer h.backend.mu.Unlock()
	contexts := 0
	for _, message := range h.backend.inbox {
		if message["command"] == "context" {
			data := message["data"].(map[string]interface{})
			if strings.HasPrefix(data["message"].(string), "## Desktop context snapshot") {
				contexts++
			}
		}
	}
	commands := 0
	for _, command := range h.executor.seenCommands() {
		if command.Type == CmdGetStatus {
			commands++
		}
	}
	if contexts != 3 || commands != 4 {
		t.Fatalf("automatic duplicates should be suppressed, but every explicit request must collect and answer: contexts=%d captures=%d", contexts, commands)
	}
}

func TestAutomaticContextDeduplicatesOnlyDeliveredMeaningfulState(t *testing.T) {
	state := &contextDeliveryState{}
	guardErr, sendErr := errors.New("permission replaced"), errors.New("disconnected")
	checks, sends := 0, 0
	guard := func() error { checks++; return nil }
	send := func(string) error { sends++; return sendErr }
	first := "## Desktop context snapshot\n- Active window: Editor\n- Mouse: (1, 2)\n- Recent action events tracked: 4\n- Screenshot: captured (PNG, 30 KB)"
	if sent, err := state.sendAutomatic(first, guard, send); sent || err != sendErr || state.hasLast {
		t.Fatalf("failed send advanced the fingerprint: sent=%v err=%v", sent, err)
	}
	sendErr = nil
	if sent, err := state.sendAutomatic(first, guard, send); !sent || err != nil {
		t.Fatalf("retry was lost: sent=%v err=%v", sent, err)
	}
	volatileOnly := "## Desktop context snapshot\n- Active window: Editor\n- Mouse: (50, 60)\n- Recent action events tracked: 40\n- Screenshot: captured (PNG, 99 KB)"
	if sent, err := state.sendAutomatic(volatileOnly, guard, send); sent || err != nil {
		t.Fatalf("pointer/size/action churn must not generate context: sent=%v err=%v", sent, err)
	}
	changed := strings.Replace(volatileOnly, "Editor", "Browser", 1)
	if sent, err := state.sendAutomatic(changed, func() error { checks++; return guardErr }, send); sent || err != guardErr {
		t.Fatalf("revocation must block even a cached context: sent=%v err=%v", sent, err)
	}
	if sent, err := state.sendAutomatic(changed, guard, send); !sent || err != nil {
		t.Fatalf("blocked context must be available after a new approval: sent=%v err=%v", sent, err)
	}
	if checks != 5 || sends != 3 {
		t.Fatalf("guards must run even on duplicates: checks=%d sends=%d", checks, sends)
	}
}

func TestContextBoundsAndObservedFieldsPreserveUTF8(t *testing.T) {
	for _, input := range []string{strings.Repeat("🧠漢é", 400), strings.Repeat("x", 3000), "bad\xffUTF8"} {
		for limit := 0; limit <= contextMessageMaxBytes; limit++ {
			got := boundedText(input, limit)
			if len(got) > limit || !utf8.ValidString(got) {
				t.Fatalf("invalid UTF-8 or byte limit exceeded: limit=%d len=%d", limit, len(got))
			}
		}
	}
	field := contextField("window\r\n## injected\trow\x00\x1b\u202e\u200b "+strings.Repeat("漢", 100), 80)
	if len(field) > 80 || !utf8.ValidString(field) || strings.ContainsAny(field, "\r\n\t\x00\x1b\u202e\u200b") {
		t.Fatalf("observed field escaped its single-line byte budget: %q", field)
	}
	message := buildDesktopContextMessage(map[string]interface{}{
		"platform": strings.Repeat("x", 80), "active_window": "title\n- Fake row: yes",
		"open_windows":      []interface{}{strings.Repeat("漢", 1000)},
		"running_processes": []interface{}{strings.Repeat("🧠", 1000)},
	}, strings.Repeat("é", 5000))
	if len(message) > contextMessageMaxBytes || !utf8.ValidString(message) || strings.Contains(message, "\n- Fake row:") {
		t.Fatalf("desktop context violated message/field bounds: %q", message)
	}
}

func TestDesktopGuideTopicsUsePublicModeAwareNames(t *testing.T) {
	nonPublic := regexp.MustCompile(`\b(get_status|key_combo|desktop_context|game_list)\b`)
	for _, low := range []bool{true, false} {
		setGuideTestMode(t, !low, low)
		// nil previews use the mode's actual specs rather than stale registration
		// left by a different connection or an earlier test.
		var n *NDIntegration
		for _, topic := range []string{"", "overview", "desktop", "script", "games", "shell", "safety", "all"} {
			result := n.desktopGuide(topic)
			if !result.Successful || len(result.Message) > contextMessageMaxBytes || !utf8.ValidString(result.Message) {
				t.Fatalf("low=%v topic=%q: invalid bounded help: %+v", low, topic, result)
			}
			if obsolete := nonPublic.FindString(result.Message); obsolete != "" {
				t.Fatalf("guide advertised non-public action %q: %s", obsolete, result.Message)
			}
			if topic == "all" {
				for _, section := range []string{"## Desktop controls", "## Script syntax", "## Games", "## Shell commands", "## Permissions and recovery"} {
					if !strings.Contains(result.Message, section) {
						t.Fatalf("all omitted %q: %s", section, result.Message)
					}
				}
			}
			if topic == "script" && low && !strings.Contains(result.Message, "COMBO ctrl c") {
				t.Fatal("script help did not explain the public way to combine keys")
			}
			if topic == "script" && !low && !strings.Contains(result.Message, "not registered") {
				t.Fatal("high-level help advertised low-level scripts")
			}
		}
		if got := n.desktopGuide("").Message; got != n.startupGuide() {
			t.Fatal("the default topic should be the compact overview")
		}
	}
}
