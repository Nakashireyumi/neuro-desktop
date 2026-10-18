package main

// `neuro-integration setup` is the first-run step. It replaces copying tokens by
// hand between the relay host, the server, and the dashboard:
//
//	1. Relay token      Created if missing. The relay host and the server both
//	                    read this one file, so there is no second copy to keep
//	                    in step. The relay comes first on purpose.
//	2. Dashboard token  Generated and printed once. Only its SHA-256 hash is stored.
//	3. Check            Each side's environment variable is either unset or equal
//	                    to the file, and the dashboard token is stored.
//
// Running it again is safe. Nothing is rotated unless --rotate-dashboard is
// given, and a stored dashboard token is never printed again.

import (
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// setupFinding is one line of the check: "ok", "warn", or "fail".
type setupFinding struct {
	level string
	text  string
}

// runSetupCommand implements `neuro-integration setup`. Exit codes: 0 when the
// setup is complete, 1 when something needs fixing, 2 for bad usage.
func runSetupCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkOnly := fs.Bool("check", false, "check the setup and change nothing")
	rotate := fs.Bool("rotate-dashboard", false, "replace the dashboard token; the old one stops working")
	relayFileFlag := fs.String("relay-token-file", relayTokenFilePath(), "the relay token file, shared by the relay host and the server")
	dashboardFileFlag := fs.String("dashboard-token-file", dashboardTokenFilePath(), "the dashboard token hash file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *checkOnly && *rotate {
		fmt.Fprintln(stderr, "setup: --check and --rotate-dashboard cannot be used together")
		return 2
	}

	relayPath := absolutePath(*relayFileFlag)
	dashboardPath := absolutePath(*dashboardFileFlag)

	fmt.Fprintln(stdout, "Neuro Desktop setup")
	fmt.Fprintln(stdout)

	if !*checkOnly {
		if code := runSetupSteps(relayPath, dashboardPath, *rotate, stdout, stderr); code != 0 {
			return code
		}
	}

	fmt.Fprintln(stdout, "[3/3] Check")
	failed := false
	for _, finding := range checkSetup(relayPath, dashboardPath, os.Getenv) {
		fmt.Fprintf(stdout, "  %-5s %s\n", strings.ToUpper(finding.level), finding.text)
		if finding.level == "fail" {
			failed = true
		}
	}
	fmt.Fprintln(stdout)
	if failed {
		fmt.Fprintln(stdout, "Setup is not complete. Fix the lines marked FAIL, then run: neuro-integration setup --check")
		return 1
	}
	fmt.Fprintf(stdout, "Setup is complete. Start the server with ./neuro-integration, open http://%s/ui/ and sign in with the dashboard token.\n",
		envOr("NEURO_ADMIN_LISTEN", "127.0.0.1:8300"))
	return 0
}

// runSetupSteps writes the two secrets. It returns non-zero only when a write
// fails or a stored value is malformed, and it never prints the token twice.
func runSetupSteps(relayPath, dashboardPath string, rotate bool, stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "[1/3] Relay token (shared by the relay host and the server)")
	_, created, err := ensureRelayTokenFile(relayPath)
	if err != nil {
		fmt.Fprintf(stderr, "setup: %v\n", err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "      created %s (mode 0600)\n", relayPath)
	} else {
		fmt.Fprintf(stdout, "      kept %s\n", relayPath)
	}
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "[2/3] Dashboard token")
	_, hashErr := readDashboardTokenHash(dashboardPath)
	switch {
	case hashErr == nil && !rotate:
		fmt.Fprintf(stdout, "      kept the token already stored in %s (run with --rotate-dashboard to replace it)\n", dashboardPath)
	case hashErr != nil && !errors.Is(hashErr, os.ErrNotExist) && !rotate:
		fmt.Fprintf(stderr, "setup: %v\n", hashErr)
		return 1
	default:
		token, err := generateDashboardToken()
		if err != nil {
			fmt.Fprintf(stderr, "setup: %v\n", err)
			return 1
		}
		if err := writeDashboardTokenHash(dashboardPath, token); err != nil {
			fmt.Fprintf(stderr, "setup: could not store the dashboard token in %s: %v\n", dashboardPath, err)
			return 1
		}
		fmt.Fprintln(stdout, "      generated. Copy it now; it is not shown again:")
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "        %s\n", token)
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "      only its SHA-256 hash is stored, in %s (mode 0600)\n", dashboardPath)
		if rotate {
			fmt.Fprintln(stdout, "      a running server keeps the old token until it is restarted")
		}
	}
	fmt.Fprintln(stdout)
	return 0
}

// checkSetup inspects the setup and changes nothing. getenv is a parameter so
// the tests can supply the environment.
func checkSetup(relayPath, dashboardPath string, getenv func(string) string) []setupFinding {
	var out []setupFinding
	add := func(level, text string) { out = append(out, setupFinding{level: level, text: text}) }

	// The relay token: the one file that both sides read.
	relayToken, err := readRelayTokenFile(relayPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("fail", fmt.Sprintf("no relay token file at %s: run `neuro-integration setup`", relayPath))
	case err != nil:
		add("fail", err.Error())
	default:
		add("ok", fmt.Sprintf("relay token file %s holds a valid token", relayPath))
		if runtime.GOOS != "windows" {
			if info, statErr := os.Stat(relayPath); statErr == nil && info.Mode().Perm()&0o077 != 0 {
				add("warn", fmt.Sprintf("%s is readable by other users; make it private (chmod 600)", relayPath))
			}
		}
	}

	// Each side may still have its own variable set. It must be unset, or equal
	// to the file. Anything else means the two sides disagree.
	for _, side := range []struct{ name, reader string }{
		{"NEURO_RELAY_TOKEN", "the server"},
		{"NEURO_RELAY_AUTH_TOKEN", "the relay host"},
	} {
		value := strings.TrimSpace(getenv(side.name))
		switch {
		case value == "":
			add("ok", fmt.Sprintf("%s is unset, so %s reads the file", side.name, side.reader))
		case relayToken == "":
			add("fail", fmt.Sprintf("%s is set, but there is no valid relay token file to check it against", side.name))
		case subtle.ConstantTimeCompare([]byte(value), []byte(relayToken)) == 1:
			add("ok", fmt.Sprintf("%s matches the relay token file", side.name))
		default:
			add("fail", fmt.Sprintf("%s differs from the relay token file, so %s and the other side would disagree; unset %s", side.name, side.reader, side.name))
		}
	}

	// The dashboard token: the stored hash, and NEURO_ADMIN_TOKEN if it is set.
	stored, hashErr := readDashboardTokenHash(dashboardPath)
	switch {
	case errors.Is(hashErr, os.ErrNotExist):
		add("fail", fmt.Sprintf("no dashboard token at %s: run `neuro-integration setup`", dashboardPath))
	case hashErr != nil:
		add("fail", hashErr.Error())
	default:
		add("ok", fmt.Sprintf("dashboard token hash stored in %s", dashboardPath))
	}

	env := strings.TrimSpace(getenv(dashboardTokenEnv))
	switch {
	case env == "":
		add("ok", fmt.Sprintf("%s is unset, so the server uses the stored dashboard token", dashboardTokenEnv))
	case validateDashboardToken(env) != nil:
		add("fail", fmt.Sprintf("%s is shorter than %d characters", dashboardTokenEnv, dashboardTokenMinLength))
	case hashErr != nil:
		add("fail", fmt.Sprintf("%s is set, but there is no stored dashboard token to check it against", dashboardTokenEnv))
	default:
		envHash := hashDashboardToken(env)
		if subtle.ConstantTimeCompare(envHash[:], stored[:]) == 1 {
			add("ok", fmt.Sprintf("%s matches the stored dashboard token", dashboardTokenEnv))
		} else {
			add("fail", fmt.Sprintf("%s differs from the stored dashboard token; unset it, or run `neuro-integration setup --rotate-dashboard` and use the new token", dashboardTokenEnv))
		}
	}
	return out
}

func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
