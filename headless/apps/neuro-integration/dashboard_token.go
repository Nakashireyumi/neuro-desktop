package main

// The dashboard token is the one secret an operator types into the dashboard.
//
// `neuro-integration setup` generates it and prints it once. The server keeps
// only its SHA-256 hash, in a 0600 file, so a copy of the directory does not
// reveal the token. The token travels in the X-ND-Token header (or a Bearer
// header) and is checked on every API route. There is no exception for loopback:
// a local process, a browser on this machine, or a proxy that connects from
// 127.0.0.1 has no more right to the API than anyone else.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	dashboardTokenBytes       = 32
	dashboardTokenMinLength   = 16
	dashboardTokenFileDefault = "./dashboard-token"
	dashboardTokenFileEnv     = "NEURO_DASHBOARD_TOKEN_FILE"
	dashboardTokenEnv         = "NEURO_ADMIN_TOKEN"
)

// dashboardNotSetUpMessage is what a guarded route says before setup has run.
const dashboardNotSetUpMessage = "the dashboard has no token yet: run `neuro-integration setup` on the server, then sign in with the token it prints"

// dashboardCredential is what a request's token is compared against. The zero
// value means "not configured", and every guarded route refuses.
type dashboardCredential struct {
	hash   [sha256.Size]byte
	source string // "env", "file", or "" when not configured
}

func (c dashboardCredential) configured() bool { return c.source != "" }

// matches compares hashes in constant time, so the comparison does not reveal
// how much of a guessed token was right.
func (c dashboardCredential) matches(token string) bool {
	if !c.configured() || token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], c.hash[:]) == 1
}

// label describes where the token came from, for logs and the status payload.
func (c dashboardCredential) label() string {
	switch c.source {
	case "env":
		return "from " + dashboardTokenEnv
	case "file":
		return "from the token file"
	}
	return "NOT SET (run neuro-integration setup)"
}

func hashDashboardToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

// credentialFromToken builds a credential from a plain token, such as the value
// of NEURO_ADMIN_TOKEN. The caller must have validated it.
func credentialFromToken(token, source string) dashboardCredential {
	return dashboardCredential{hash: hashDashboardToken(token), source: source}
}

// generateDashboardToken returns a new random token: 32 bytes, base64url, no padding.
func generateDashboardToken() (string, error) {
	raw := make([]byte, dashboardTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validateDashboardToken(token string) error {
	if len(token) < dashboardTokenMinLength {
		return fmt.Errorf("the dashboard token must be at least %d characters", dashboardTokenMinLength)
	}
	return nil
}

// dashboardTokenFilePath is where the hash lives. It is relative to the working
// directory unless NEURO_DASHBOARD_TOKEN_FILE says otherwise.
func dashboardTokenFilePath() string {
	return envOr(dashboardTokenFileEnv, dashboardTokenFileDefault)
}

// readDashboardTokenHash reads the stored hash. A missing file is reported as
// os.ErrNotExist; a malformed one is an error, never silently replaced.
func readDashboardTokenHash(path string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	raw, decodeErr := hex.DecodeString(strings.TrimSpace(string(data)))
	if decodeErr != nil || len(raw) != sha256.Size {
		return out, fmt.Errorf("%s does not hold a dashboard token hash; run `neuro-integration setup --rotate-dashboard`", path)
	}
	copy(out[:], raw)
	return out, nil
}

// writeDashboardTokenHash stores the hash of token, mode 0600.
func writeDashboardTokenHash(path, token string) error {
	sum := hashDashboardToken(token)
	return writeSecretFile(path, hex.EncodeToString(sum[:])+"\n")
}

// writeSecretFile writes content to path with mode 0600. It writes a temporary
// file in the same directory and renames it, so an interrupted write never
// leaves a truncated secret behind.
func writeSecretFile(path, content string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // harmless once the rename has succeeded

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// loadDashboardCredential works out what the server should accept. When
// NEURO_ADMIN_TOKEN is set and a stored hash exists, the two must match: a
// silent disagreement would leave the operator locked out, or running on a
// token they did not expect.
func loadDashboardCredential(envToken, file string) (dashboardCredential, error) {
	envToken = strings.TrimSpace(envToken)

	stored, err := readDashboardTokenHash(file)
	haveFile := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return dashboardCredential{}, err
	}

	if envToken != "" {
		if err := validateDashboardToken(envToken); err != nil {
			return dashboardCredential{}, fmt.Errorf("%s: %w", dashboardTokenEnv, err)
		}
		cred := credentialFromToken(envToken, "env")
		if haveFile && subtle.ConstantTimeCompare(cred.hash[:], stored[:]) != 1 {
			return dashboardCredential{}, fmt.Errorf(
				"%s does not match the dashboard token in %s; unset %s, or run `neuro-integration setup --rotate-dashboard` and use the new token",
				dashboardTokenEnv, file, dashboardTokenEnv)
		}
		return cred, nil
	}

	if haveFile {
		return dashboardCredential{hash: stored, source: "file"}, nil
	}
	return dashboardCredential{}, nil
}
