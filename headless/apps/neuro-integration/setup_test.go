package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// setupPaths gives a test its own relay and dashboard files, and clears the
// variables that would otherwise leak in from the developer's shell.
func setupPaths(t *testing.T) (relayFile, dashboardFile string, args []string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("NEURO_RELAY_TOKEN", "")
	t.Setenv("NEURO_RELAY_AUTH_TOKEN", "")
	t.Setenv("NEURO_ADMIN_TOKEN", "")
	relayFile = filepath.Join(dir, "relay-token")
	dashboardFile = filepath.Join(dir, "dashboard-token")
	args = []string{"--relay-token-file", relayFile, "--dashboard-token-file", dashboardFile}
	return relayFile, dashboardFile, args
}

func runSetup(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runSetupCommand(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// printedTokens returns the lines that look like a generated dashboard token.
var tokenLine = regexp.MustCompile(`^\s{8}([A-Za-z0-9_-]{43})$`)

func printedTokens(output string) []string {
	var tokens []string
	for _, line := range strings.Split(output, "\n") {
		if m := tokenLine.FindStringSubmatch(line); m != nil {
			tokens = append(tokens, m[1])
		}
	}
	return tokens
}

func TestSetupFirstRunWritesBothSecretsInRelayFirstOrder(t *testing.T) {
	relayFile, dashboardFile, args := setupPaths(t)

	code, out, errOut := runSetup(t, args)
	if code != 0 {
		t.Fatalf("first run should succeed, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	relayAt := strings.Index(out, "[1/3]")
	dashboardAt := strings.Index(out, "[2/3]")
	checkAt := strings.Index(out, "[3/3]")
	if !(relayAt >= 0 && relayAt < dashboardAt && dashboardAt < checkAt) {
		t.Fatalf("the steps must run relay first, then the dashboard token, then the check:\n%s", out)
	}

	tokens := printedTokens(out)
	if len(tokens) != 1 {
		t.Fatalf("the dashboard token must be printed exactly once, found %d:\n%s", len(tokens), out)
	}
	cred, err := loadDashboardCredential("", dashboardFile)
	if err != nil || !cred.matches(tokens[0]) {
		t.Fatalf("the printed token must be the one the server accepts: %v", err)
	}
	if _, err := readRelayTokenFile(relayFile); err != nil {
		t.Fatalf("the relay token file should be valid after setup: %v", err)
	}
	if !strings.Contains(out, "Setup is complete") {
		t.Fatalf("a finished setup should say so:\n%s", out)
	}
}

func TestSetupRunTwiceKeepsEverythingAndPrintsNoToken(t *testing.T) {
	relayFile, dashboardFile, args := setupPaths(t)
	if code, out, _ := runSetup(t, args); code != 0 {
		t.Fatalf("first run failed: %d\n%s", code, out)
	}
	relayBefore, _ := os.ReadFile(relayFile)
	hashBefore, _ := os.ReadFile(dashboardFile)

	code, out, errOut := runSetup(t, args)
	if code != 0 {
		t.Fatalf("second run should succeed, got %d\nstderr: %s", code, errOut)
	}
	if len(printedTokens(out)) != 0 {
		t.Fatalf("a stored dashboard token must never be printed again:\n%s", out)
	}
	relayAfter, _ := os.ReadFile(relayFile)
	hashAfter, _ := os.ReadFile(dashboardFile)
	if !bytes.Equal(relayBefore, relayAfter) || !bytes.Equal(hashBefore, hashAfter) {
		t.Fatal("a second run must not rotate anything")
	}
}

func TestSetupRotateReplacesOnlyTheDashboardToken(t *testing.T) {
	relayFile, dashboardFile, args := setupPaths(t)
	_, first, _ := runSetup(t, args)
	oldToken := printedTokens(first)[0]
	relayBefore, _ := os.ReadFile(relayFile)

	code, out, _ := runSetup(t, append([]string{"--rotate-dashboard"}, args...))
	if code != 0 {
		t.Fatalf("rotation should succeed, got %d:\n%s", code, out)
	}
	newTokens := printedTokens(out)
	if len(newTokens) != 1 || newTokens[0] == oldToken {
		t.Fatalf("rotation should print one new token, got %v", newTokens)
	}
	cred, err := loadDashboardCredential("", dashboardFile)
	if err != nil {
		t.Fatal(err)
	}
	if cred.matches(oldToken) || !cred.matches(newTokens[0]) {
		t.Fatal("after rotation only the new token may work")
	}
	relayAfter, _ := os.ReadFile(relayFile)
	if !bytes.Equal(relayBefore, relayAfter) {
		t.Fatal("rotating the dashboard token must not touch the relay token")
	}
	if !strings.Contains(out, "restarted") {
		t.Fatalf("the operator should be told to restart the server:\n%s", out)
	}
}

func TestSetupCheckChangesNothing(t *testing.T) {
	relayFile, dashboardFile, args := setupPaths(t)

	code, out, _ := runSetup(t, append([]string{"--check"}, args...))
	if code != 1 {
		t.Fatalf("an empty setup should fail the check, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "run `neuro-integration setup`") {
		t.Fatalf("the failure should say what to run:\n%s", out)
	}
	for _, path := range []string{relayFile, dashboardFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("--check must not create %s", path)
		}
	}

	if code, out, _ := runSetup(t, args); code != 0 {
		t.Fatalf("setup should succeed: %d\n%s", code, out)
	}
	if code, out, _ := runSetup(t, append([]string{"--check"}, args...)); code != 0 {
		t.Fatalf("the check should pass after setup, got %d:\n%s", code, out)
	}
}

func TestSetupCheckFailsWhenTheRelayTokensDisagree(t *testing.T) {
	_, _, args := setupPaths(t)
	if code, out, _ := runSetup(t, args); code != 0 {
		t.Fatalf("setup should succeed: %d\n%s", code, out)
	}

	// The server's copy differs from the file: the relay would refuse it.
	t.Setenv("NEURO_RELAY_TOKEN", "0123456789abcdef0123456789abcdef")
	code, out, _ := runSetup(t, append([]string{"--check"}, args...))
	if code != 1 || !strings.Contains(out, "NEURO_RELAY_TOKEN differs") {
		t.Fatalf("a disagreeing NEURO_RELAY_TOKEN must fail the check, got %d:\n%s", code, out)
	}

	// The relay host's copy differs from the file: the relay would use the wrong value.
	t.Setenv("NEURO_RELAY_TOKEN", "")
	t.Setenv("NEURO_RELAY_AUTH_TOKEN", "fedcba9876543210fedcba9876543210")
	code, out, _ = runSetup(t, append([]string{"--check"}, args...))
	if code != 1 || !strings.Contains(out, "NEURO_RELAY_AUTH_TOKEN differs") {
		t.Fatalf("a disagreeing NEURO_RELAY_AUTH_TOKEN must fail the check, got %d:\n%s", code, out)
	}
}

func TestSetupCheckPassesWhenTheEnvironmentAgrees(t *testing.T) {
	relayFile, _, args := setupPaths(t)
	if code, out, _ := runSetup(t, args); code != 0 {
		t.Fatalf("setup should succeed: %d\n%s", code, out)
	}
	token, err := readRelayTokenFile(relayFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEURO_RELAY_TOKEN", token)
	t.Setenv("NEURO_RELAY_AUTH_TOKEN", token)
	if code, out, _ := runSetup(t, append([]string{"--check"}, args...)); code != 0 {
		t.Fatalf("agreeing variables should pass, got %d:\n%s", code, out)
	}
}

func TestSetupCheckFailsOnAWrongAdminToken(t *testing.T) {
	_, _, args := setupPaths(t)
	if code, out, _ := runSetup(t, args); code != 0 {
		t.Fatalf("setup should succeed: %d\n%s", code, out)
	}
	t.Setenv("NEURO_ADMIN_TOKEN", "this-is-not-the-stored-token")
	code, out, _ := runSetup(t, append([]string{"--check"}, args...))
	if code != 1 || !strings.Contains(out, "NEURO_ADMIN_TOKEN differs") {
		t.Fatalf("a wrong NEURO_ADMIN_TOKEN must fail the check, got %d:\n%s", code, out)
	}
}

func TestSetupRefusesToReplaceAMalformedDashboardFile(t *testing.T) {
	_, dashboardFile, args := setupPaths(t)
	if err := os.WriteFile(dashboardFile, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runSetup(t, args)
	if code != 1 || !strings.Contains(errOut, "rotate-dashboard") {
		t.Fatalf("a malformed hash should stop setup and say how to fix it, got %d (%s)", code, errOut)
	}
	data, _ := os.ReadFile(dashboardFile)
	if string(data) != "garbage\n" {
		t.Fatalf("the malformed file was changed without --rotate-dashboard: %q (%s)", data, out)
	}
}

func TestSetupRejectsConflictingFlags(t *testing.T) {
	_, _, args := setupPaths(t)
	if code, _, _ := runSetup(t, append([]string{"--check", "--rotate-dashboard"}, args...)); code != 2 {
		t.Fatalf("--check and --rotate-dashboard together are a usage error, got %d", code)
	}
}
