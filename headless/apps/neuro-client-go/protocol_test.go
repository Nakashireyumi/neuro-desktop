package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
)

// fakeHub listens on loopback and runs script on the first client it accepts.
func fakeHub(t *testing.T, script func(conn net.Conn, r *bufio.Reader)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		script(conn, bufio.NewReader(conn))
	}()
	return ln.Addr().String()
}

func writeLine(conn net.Conn, line string) {
	_, _ = conn.Write([]byte(line + "\n"))
}

// expectFrame reads one frame and checks it is an object with the given type.
func expectFrame(t *testing.T, r *bufio.Reader, wantType string) map[string]interface{} {
	t.Helper()
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Errorf("reading %s: %v", wantType, err)
		return map[string]interface{}{}
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(line, &frame); err != nil {
		t.Errorf("frame is not JSON: %v (%q)", err, line)
		return map[string]interface{}{}
	}
	if frame["type"] != wantType {
		t.Errorf("want a %s frame, got %v", wantType, frame["type"])
	}
	return frame
}

func TestSessionAnswersPingsCommandsAndShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell path is not verified on Windows")
	}
	t.Setenv("NEURO_SHELL_ALLOWLIST", "echo")
	t.Setenv("NEURO_HEADLESS", "1")
	const token = "a-test-token-for-the-session"

	addr := fakeHub(t, func(conn net.Conn, r *bufio.Reader) {
		hello := expectFrame(t, r, "hello")
		if hello["role"] != "executor" || hello["token"] != token || hello["version"] != protocolVersion {
			t.Errorf("hello = %v", hello)
		}
		writeLine(conn, `{"type":"hello_ack","role":"bridge","version":"2"}`)

		writeLine(conn, `{"type":"ping","id":"p1"}`)
		if pong := expectFrame(t, r, "pong"); pong["id"] != "p1" {
			t.Errorf("pong = %v", pong)
		}

		writeLine(conn, `{"type":"command","id":"c1","command":{"type":"get_status","params":{}}}`)
		if res := expectFrame(t, r, "result"); res["id"] != "c1" || res["success"] != true {
			t.Errorf("get_status result = %v", res)
		}

		writeLine(conn, `{"type":"command","id":"c2","command":{"type":"shell_command","params":{"command":"echo hi","allowlist":["echo"],"denylist":[]}}}`)
		res := expectFrame(t, r, "result")
		data, _ := res["data"].(map[string]interface{})
		if res["success"] != true || data["output"] != "exit code: 0\nstdout:\nhi" {
			t.Errorf("shell result = %v", res)
		}

		writeLine(conn, `{"type":"command","id":"c3","command":{"type":"move_mouse_to","params":{"x":1,"y":2}}}`)
		res = expectFrame(t, r, "result")
		if res["success"] != false || !strings.Contains(res["error"].(string), "display session") {
			t.Errorf("headless input must be refused: %v", res)
		}

		writeLine(conn, `{"type":"command","id":"c4","command":{"type":"shutdown_gracefully"}}`)
		res = expectFrame(t, r, "result")
		if d, _ := res["data"].(map[string]interface{}); d["shutdown"] != true {
			t.Errorf("shutdown result = %v", res)
		}
	})

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var out bytes.Buffer
	shutdown, err := serveSession(conn, NewAgent(), token, &out)
	if err != nil || !shutdown {
		t.Fatalf("serveSession: shutdown=%v err=%v", shutdown, err)
	}
	if !strings.Contains(out.String(), "shutdown requested by the hub") {
		t.Errorf("log = %q", out.String())
	}
}

func TestAHubRefusalIsNotRetried(t *testing.T) {
	addr := fakeHub(t, func(conn net.Conn, r *bufio.Reader) {
		expectFrame(t, r, "hello")
		writeLine(conn, `{"type":"hello_nack","error":"invalid or missing executor token"}`)
	})
	var out, errOut bytes.Buffer
	if code := connectRole(addr, "", false, NewAgent(), &out, &errOut); code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "not retrying") || !strings.Contains(errOut.String(), "invalid or missing executor token") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestAnOversizedFrameEndsTheSession(t *testing.T) {
	addr := fakeHub(t, func(conn net.Conn, r *bufio.Reader) {
		expectFrame(t, r, "hello")
		writeLine(conn, `{"type":"hello_ack","role":"bridge","version":"2"}`)
		// More than the 8 MiB limit, with a newline only at the end.
		_, _ = conn.Write(bytes.Repeat([]byte("a"), maxLineBytes+1024))
		_, _ = conn.Write([]byte("\n"))
	})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = serveSession(conn, NewAgent(), "", io.Discard)
	if !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("want errFrameTooLarge, got %v", err)
	}
}

func TestOnceGivesUpWhenTheHubIsUnreachable(t *testing.T) {
	var out, errOut bytes.Buffer
	code := connectRole("127.0.0.1:1", "", true, NewAgent(), &out, &errOut)
	if code != exitError || !strings.Contains(errOut.String(), "giving up") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestRunRejectsBadUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--ipc-file", "x"}, &out, &errOut); code != exitUsage {
		t.Errorf("--ipc-file: exit %d", code)
	}
	if code := run([]string{"extra"}, &out, &errOut); code != exitUsage {
		t.Errorf("a stray argument: exit %d", code)
	}
	if code := run([]string{"--help"}, &out, &errOut); code != exitOK {
		t.Errorf("--help: exit %d", code)
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	want := []int{2, 4, 8, 16, 30, 30}
	for i, seconds := range want {
		if got := backoff(i + 1).Seconds(); int(got) != seconds {
			t.Errorf("backoff(%d) = %v, want %ds", i+1, got, seconds)
		}
	}
}
