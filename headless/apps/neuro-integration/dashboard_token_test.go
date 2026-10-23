package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGeneratedDashboardTokensAreLongAndDistinct(t *testing.T) {
	first, err := generateDashboardToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateDashboardToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 40 || first == second {
		t.Fatalf("tokens should be long and unique: %q %q", first, second)
	}
	if strings.ContainsAny(first, "+/= ") {
		t.Fatalf("the token should be URL-safe with no padding: %q", first)
	}
}

func TestDashboardCredentialMatchesOnlyTheToken(t *testing.T) {
	cred := credentialFromToken("correct-horse-battery", "env")
	if !cred.configured() || !cred.matches("correct-horse-battery") {
		t.Fatal("the right token must match")
	}
	for _, wrong := range []string{"", "correct-horse", "correct-horse-battery ", "CORRECT-HORSE-BATTERY"} {
		if cred.matches(wrong) {
			t.Errorf("%q must not match", wrong)
		}
	}
	var unset dashboardCredential
	if unset.configured() || unset.matches("") || unset.matches("anything") {
		t.Fatal("an unconfigured credential must refuse everything, including the empty token")
	}
}

func TestDashboardTokenHashIsStoredNotTheToken(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dashboard-token")
	const token = "store-me-not-in-plain-text-0001"

	if err := writeDashboardTokenHash(file, token); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("the token itself must never be written to disk")
	}
	stored, err := readDashboardTokenHash(file)
	if err != nil {
		t.Fatal(err)
	}
	cred := dashboardCredential{hash: stored, source: "file"}
	if !cred.matches(token) || cred.matches(token+"x") {
		t.Fatal("the stored hash must verify the token and nothing else")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("the hash file mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestMalformedDashboardTokenFileIsRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dashboard-token")
	if err := os.WriteFile(file, []byte("not a hash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDashboardTokenHash(file); err == nil {
		t.Fatal("a malformed hash file must be an error, not an empty credential")
	}
	if _, err := loadDashboardCredential("", file); err == nil {
		t.Fatal("the server must not start with a malformed token file")
	}
}

func TestLoadDashboardCredentialPrecedence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dashboard-token")

	// Nothing configured: not set up.
	cred, err := loadDashboardCredential("", file)
	if err != nil || cred.configured() {
		t.Fatalf("no token should mean not configured, got %+v, %v", cred, err)
	}

	// Stored hash only.
	const stored = "stored-dashboard-token-0001"
	if err := writeDashboardTokenHash(file, stored); err != nil {
		t.Fatal(err)
	}
	cred, err = loadDashboardCredential("", file)
	if err != nil || cred.source != "file" || !cred.matches(stored) {
		t.Fatalf("the stored token should load, got %+v, %v", cred, err)
	}

	// NEURO_ADMIN_TOKEN agreeing with the file is fine.
	cred, err = loadDashboardCredential(stored, file)
	if err != nil || cred.source != "env" || !cred.matches(stored) {
		t.Fatalf("a matching env token should load, got %+v, %v", cred, err)
	}

	// NEURO_ADMIN_TOKEN that disagrees is refused, so the operator is never
	// quietly running on a token they did not expect.
	if _, err := loadDashboardCredential("a-different-token-0002", file); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a disagreeing env token must be refused, got %v", err)
	}

	// A short env token is refused even with no file.
	if _, err := loadDashboardCredential("short", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a short NEURO_ADMIN_TOKEN must be refused")
	}
}
