// neuro-client-go is the Go client for the PC Neuro controls. It is the first slice
// of a port of desktop/backend/python/controller. This slice has the executor link
// (hello, ping, commands, shutdown, reconnects), the headless rules, get_status, the
// shell command with the same firewall and limits, and the lifecycle commands.
//
// Mouse, keyboard, screen, window and script commands are NOT ported yet. They are
// refused with a message that says so, and the Python client still runs them. The
// shipped client is still the Python one (neuro-client). See docs/ARCHITECTURE.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses the flags, connects to the hub, and runs until the session ends for good.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("neuro-client-go", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bridge := flags.String("bridge", envOr("NEURO_AGENT_BRIDGE", "127.0.0.1:9876"),
		"host:port of the server's executor hub")
	token := flags.String("token", os.Getenv("NEURO_EXECUTOR_TOKEN"),
		"shared secret the hub requires (NEURO_EXECUTOR_TOKEN)")
	once := flags.Bool("once", false, "exit after the first session instead of reconnecting")
	ipcFile := flags.String("ipc-file", "",
		"not in this client yet: use the Python agent for file IPC")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "neuro-client-go: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}
	if *ipcFile != "" {
		fmt.Fprintln(stderr, "neuro-client-go: file IPC is not in the Go client yet; run the Python agent (python3 -m controller.agent --ipc-file ...) for it")
		return exitUsage
	}
	if strings.TrimSpace(*bridge) == "" {
		fmt.Fprintln(stderr, "neuro-client-go: --bridge needs host:port")
		return exitUsage
	}

	agent := NewAgent()
	fmt.Fprintln(stdout, "[agent] role: client (connecting out); the bridge owns the socket")
	if agent.headless {
		fmt.Fprintf(stdout, "[agent] headless: %s\n", agent.headlessReason)
	}
	return connectRole(*bridge, *token, *once, agent, stdout, stderr)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
