package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// The wire format is the one in docs/EXECUTOR_PROTOCOL.md: newline-delimited JSON,
// protocol version "2". The limits match the server's (executor_hub.go).
const (
	protocolVersion = "2"
	maxLineBytes    = 8 << 20
	dialTimeout     = 10 * time.Second
)

var (
	// errRejected means the hub refused this client's hello. It is never retried:
	// the same token only gets the same refusal.
	errRejected = errors.New("the hub rejected this client")
	// errFrameTooLarge means a frame passed maxLineBytes. The stream cannot be
	// resynchronised after that, so the session ends.
	errFrameTooLarge = errors.New("frame too large")
)

// readFrame returns the next frame without its newline. At a clean end of stream
// it returns io.EOF. A frame cut off by the end of the stream is returned as it is.
func readFrame(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > maxLineBytes {
			return nil, errFrameTooLarge
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return bytes.TrimSuffix(line, []byte("\n")), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return line, nil
		default:
			return nil, err
		}
	}
}

func writeFrame(w io.Writer, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// serveSession runs one session with the hub. It returns true when the hub asked
// this client to shut down, and false when the hub closed the session normally.
func serveSession(conn net.Conn, agent *Agent, token string, out io.Writer) (bool, error) {
	reader := bufio.NewReader(conn)

	hello := map[string]interface{}{
		"type":    "hello",
		"role":    "executor",
		"version": protocolVersion,
	}
	if token != "" {
		hello["token"] = token
	}
	if err := writeFrame(conn, hello); err != nil {
		return false, err
	}

	first, err := readFrame(reader)
	if errors.Is(err, io.EOF) {
		return false, errors.New("the hub closed the connection during the handshake")
	}
	if err != nil {
		return false, err
	}
	var ack map[string]interface{}
	if err := json.Unmarshal(first, &ack); err != nil {
		return false, fmt.Errorf("the hub's handshake reply is not JSON: %w", err)
	}
	switch ack["type"] {
	case "hello_nack":
		return false, fmt.Errorf("%w: %v", errRejected, valueOr(ack["error"], "unspecified"))
	case "hello_ack":
	default:
		return false, fmt.Errorf("expected hello_ack, got %v", ack["type"])
	}
	fmt.Fprintf(out, "[agent] connected to %s (protocol %v) — waiting for commands\n",
		conn.RemoteAddr(), valueOr(ack["version"], "?"))

	for {
		line, err := readFrame(reader)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal(line, &envelope); err != nil {
			fmt.Fprintf(out, "[agent] ignoring malformed frame: %v\n", err)
			continue
		}
		id := valueOr(envelope["id"], "")
		switch envelope["type"] {
		case "ping":
			if err := writeFrame(conn, map[string]interface{}{"type": "pong", "id": id}); err != nil {
				return false, err
			}
			continue
		case "command":
		default:
			continue
		}

		command, _ := envelope["command"].(map[string]interface{})
		result := agent.Execute(command)
		reply := map[string]interface{}{"type": "result", "id": id, "success": result.Success}
		if result.Data != nil {
			reply["data"] = result.Data
		}
		if result.Error != "" {
			reply["error"] = result.Error
		}
		if err := writeFrame(conn, reply); err != nil {
			return false, err
		}

		if shutdown, _ := result.Data["shutdown"].(bool); shutdown {
			fmt.Fprintln(out, "[agent] shutdown requested by the hub")
			return true, nil
		}
	}
}

// connectRole keeps a session open to the hub and reconnects with backoff, as the
// Python client does. It returns the process exit code.
func connectRole(bridge, token string, once bool, agent *Agent, out, errOut io.Writer) int {
	attempt := 0
	for {
		conn, err := net.DialTimeout("tcp", bridge, dialTimeout)
		if err != nil {
			attempt++
			if once {
				fmt.Fprintf(errOut, "[agent] giving up: %v\n", err)
				return exitError
			}
			delay := backoff(attempt)
			fmt.Fprintf(out, "[agent] could not reach %s (%v); retrying in %s\n", bridge, err, delay)
			time.Sleep(delay)
			continue
		}

		shutdown, err := serveSession(conn, agent, token, out)
		_ = conn.Close()
		switch {
		case shutdown:
			return exitOK
		case errors.Is(err, errRejected):
			fmt.Fprintf(errOut, "[agent] not retrying: %v\n", err)
			return exitError
		case err == nil:
			fmt.Fprintln(out, "[agent] the hub closed the connection")
			if once {
				return exitOK
			}
			attempt = 0
			time.Sleep(2 * time.Second)
		default:
			attempt++
			if once {
				fmt.Fprintf(errOut, "[agent] giving up: %v\n", err)
				return exitError
			}
			delay := backoff(attempt)
			fmt.Fprintf(out, "[agent] session failed (%v); retrying in %s\n", err, delay)
			time.Sleep(delay)
		}
	}
}

// backoff is 2^attempt seconds, capped at 32 and then at 30, like the Python client.
func backoff(attempt int) time.Duration {
	exp := attempt
	if exp > 5 {
		exp = 5
	}
	delay := time.Duration(1<<exp) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

func valueOr(value interface{}, fallback string) interface{} {
	if value == nil {
		return fallback
	}
	if s, ok := value.(string); ok && s == "" {
		return fallback
	}
	return value
}
