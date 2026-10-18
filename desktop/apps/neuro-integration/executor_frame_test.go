package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestReadExecutorLineReadsOneFrameAtATime(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("first\nsecond\n"))

	line, err := readExecutorLine(r, 64)
	if err != nil || string(line) != "first\n" {
		t.Fatalf("first frame: got %q, %v", line, err)
	}
	line, err = readExecutorLine(r, 64)
	if err != nil || string(line) != "second\n" {
		t.Fatalf("second frame: got %q, %v", line, err)
	}
}

func TestReadExecutorLineKeepsALineLongerThanTheBuffer(t *testing.T) {
	// bufio's default buffer is 4 KiB, so this frame takes several reads.
	payload := strings.Repeat("x", 20000)
	r := bufio.NewReader(strings.NewReader(payload + "\n"))

	line, err := readExecutorLine(r, 1<<20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(line) != payload+"\n" {
		t.Fatalf("frame lost bytes: got %d bytes, want %d", len(line), len(payload)+1)
	}
}

func TestReadExecutorLineCountsTheNewlineAgainstTheLimit(t *testing.T) {
	atLimit := bufio.NewReader(strings.NewReader(strings.Repeat("x", 99) + "\n"))
	if line, err := readExecutorLine(atLimit, 100); err != nil || len(line) != 100 {
		t.Fatalf("a frame exactly at the limit: got %d bytes, %v", len(line), err)
	}

	overLimit := bufio.NewReader(strings.NewReader(strings.Repeat("x", 100) + "\n"))
	if _, err := readExecutorLine(overLimit, 100); !errors.Is(err, errExecutorLineTooLong) {
		t.Fatalf("a frame one byte over the limit: expected errExecutorLineTooLong, got %v", err)
	}
}

func TestReadExecutorLineStopsAtTheLimitWithoutANewline(t *testing.T) {
	// No newline ever arrives. The reader must give up once the limit is passed
	// instead of waiting for the frame to end.
	r := bufio.NewReader(strings.NewReader(strings.Repeat("x", 5000)))
	if _, err := readExecutorLine(r, 1024); !errors.Is(err, errExecutorLineTooLong) {
		t.Fatalf("expected errExecutorLineTooLong, got %v", err)
	}
}

func TestReadExecutorLineReturnsEOFForAnUnfinishedFrame(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("no newline here"))
	if _, err := readExecutorLine(reader, 1024); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF for a frame cut short, got %v", err)
	}
}

func TestExecutorHubDropsAnOversizedHelloBeforeAuth(t *testing.T) {
	hub := newHub(t, "a-token-that-the-test-does-not-send")

	conn, err := net.Dial("tcp", hub.Addr())
	if err != nil {
		t.Fatalf("dial hub: %v", err)
	}
	defer conn.Close()

	// Send a hello that is far too large and never ends the frame early. The
	// write may fail once the hub closes the socket; that is expected.
	_, _ = conn.Write(append(bytes.Repeat([]byte("a"), maxExecutorHelloBytes+1024), '\n'))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if n != 0 {
		t.Fatalf("the hub answered an oversized hello before auth: %q", buf[:n])
	}
	if err == nil {
		t.Fatal("expected the hub to close the connection")
	}
}

func TestExecutorHubAcceptsAScreenshotSizedStatusReply(t *testing.T) {
	hub := newHub(t, "")

	// About 3 MiB of PNG bytes, base64-encoded the way the client sends them.
	shot := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 3<<18))
	executor := startFakeExecutor(t, hub, "", func(cmd IPCCommand) (*IPCResponse, bool) {
		return &IPCResponse{Success: true, Data: map[string]interface{}{"screenshot_png_b64": shot}}, true
	})
	defer executor.conn.Close()

	response, err := hub.SendCommand(IPCCommand{Type: CmdGetStatus}, 10*time.Second)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	got, _ := response.Data["screenshot_png_b64"].(string)
	if got != shot {
		t.Fatalf("the screenshot reply was cut or altered: got %d bytes, want %d", len(got), len(shot))
	}
}
