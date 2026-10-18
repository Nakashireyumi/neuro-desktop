package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer stands in for neuro-integration. It records what the dashboard sent
// it, and answers with whatever the test sets.
type fakeServer struct {
	t        *testing.T
	seenPath string
	seenHdr  http.Header
	calls    int
	status   int
	body     string
	header   http.Header
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	fake := &fakeServer{t: t, status: http.StatusOK, body: `{"ok":true}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.calls++
		fake.seenPath = r.URL.Path
		fake.seenHdr = r.Header.Clone()
		for key, values := range fake.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(fake.status)
		_, _ = io.WriteString(w, fake.body)
	}))
	t.Cleanup(server.Close)
	return fake, server
}

// dashboardFor builds the dashboard's handler against a fake server and a UI folder
// that the test controls.
func dashboardFor(t *testing.T, serverURL string, uiRoot string) http.Handler {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return newHandler(config{listen: "127.0.0.1:8310", server: target, uiDir: uiRoot})
}

func makeUI(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "frontend")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>the shell</html>"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log('app')"), 0o644))
	// A file next to the UI folder, which must never be reachable.
	must(t, os.WriteFile(filepath.Join(filepath.Dir(root), "secret.txt"), []byte("SECRET"), 0o600))
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

// The browser's token reaches the server unchanged, and the dashboard adds none.
func TestTheTokenPassesThroughAndIsNeverAdded(t *testing.T) {
	fake, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	get(t, handler, http.MethodGet, "/api/status", map[string]string{"X-ND-Token": "from-the-browser"})
	if fake.seenPath != "/api/status" {
		t.Fatalf("server saw path %q, want /api/status", fake.seenPath)
	}
	if got := fake.seenHdr.Get("X-ND-Token"); got != "from-the-browser" {
		t.Fatalf("server saw token %q, want the browser's token", got)
	}

	get(t, handler, http.MethodGet, "/api/status", nil)
	if got := fake.seenHdr.Get("X-ND-Token"); got != "" {
		t.Fatalf("the dashboard must not add a token, but the server saw %q", got)
	}
}

// The server's own answers, including refusals, reach the browser unchanged.
func TestTheServersAnswerIsPassedThroughUnchanged(t *testing.T) {
	fake, server := newFakeServer(t)
	fake.status = http.StatusUnauthorized
	fake.body = `{"ok":false,"error":"invalid or missing dashboard token"}`
	handler := dashboardFor(t, server.URL, makeUI(t))

	recorder := get(t, handler, http.MethodGet, "/api/runtime", nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the server's 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid or missing dashboard token") {
		t.Fatalf("body was rewritten: %s", recorder.Body.String())
	}
}

// Only the API and /health are forwarded. Any other path stays on the dashboard.
func TestOnlyTheAPIAndHealthAreForwarded(t *testing.T) {
	fake, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	for _, target := range []string{"/admin", "/apix/status", "/etc/passwd", "/ui-other"} {
		recorder := get(t, handler, http.MethodGet, target, nil)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", target, recorder.Code)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("the server was called %d times for paths it must never see", fake.calls)
	}

	get(t, handler, http.MethodGet, "/health", nil)
	if fake.seenPath != "/health" {
		t.Fatalf("/health must be forwarded, server saw %q", fake.seenPath)
	}
}

func TestTheUIIsServedFromTheBuiltFolder(t *testing.T) {
	_, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	if got := get(t, handler, http.MethodGet, "/ui/", nil).Body.String(); got != "<html>the shell</html>" {
		t.Fatalf("/ui/ should serve the shell, got %q", got)
	}
	if got := get(t, handler, http.MethodGet, "/ui/assets/app.js", nil).Body.String(); !strings.Contains(got, "console.log") {
		t.Fatalf("an asset must be served as a file, got %q", got)
	}
	if got := get(t, handler, http.MethodGet, "/ui/permissions/deep/link", nil).Body.String(); got != "<html>the shell</html>" {
		t.Fatalf("an unknown page must get the shell for the single-page app, got %q", got)
	}
}

// A path that climbs out of the UI folder never reaches a file outside it.
func TestTheUIFolderCannotBeClimbedOut(t *testing.T) {
	_, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	for _, target := range []string{"/ui/../secret.txt", "/ui/%2e%2e/secret.txt", "/ui/assets/../../secret.txt"} {
		body := get(t, handler, http.MethodGet, target, nil).Body.String()
		if strings.Contains(body, "SECRET") {
			t.Fatalf("%s read a file outside the UI folder", target)
		}
	}
}

func TestRootSendsTheOperatorToTheDashboard(t *testing.T) {
	_, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	recorder := get(t, handler, http.MethodGet, "/", nil)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/ui/" {
		t.Fatalf("/ should redirect to /ui/, got %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

// When the server is down, the dashboard says so in one readable answer.
func TestAnUnreachableServerIsAClearError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := server.URL
	server.Close()
	handler := dashboardFor(t, closedURL, makeUI(t))

	recorder := get(t, handler, http.MethodGet, "/api/status", nil)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "did not answer") {
		t.Fatalf("the error should say the server did not answer: %s", recorder.Body.String())
	}
}

// Live state must not be kept by the browser or a cache in between.
func TestAPIAnswersAreNotCached(t *testing.T) {
	_, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	if got := get(t, handler, http.MethodGet, "/api/status", nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestTheShellIsNeverCached(t *testing.T) {
	_, server := newFakeServer(t)
	handler := dashboardFor(t, server.URL, makeUI(t))

	if got := get(t, handler, http.MethodGet, "/ui/", nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("the shell's Cache-Control = %q, want no-store", got)
	}
}

func TestConfigRefusesABadServerAddress(t *testing.T) {
	ui := makeUI(t)
	for _, server := range []string{"127.0.0.1:8300", "ftp://127.0.0.1:8300", "http://"} {
		if _, err := newConfig("127.0.0.1:8310", server, ui); err == nil {
			t.Fatalf("--server %q must be refused", server)
		}
	}
	if _, err := newConfig("127.0.0.1:8310", "http://127.0.0.1:8300", ui); err != nil {
		t.Fatalf("a valid server URL was refused: %v", err)
	}
}

func TestConfigNeedsAUIFolderWithAnIndex(t *testing.T) {
	empty := t.TempDir()
	if _, err := newConfig("127.0.0.1:8310", "http://127.0.0.1:8300", empty); err == nil {
		t.Fatal("a UI folder without index.html must be refused")
	}
	if _, err := newConfig("127.0.0.1:8310", "http://127.0.0.1:8300", makeUI(t)); err != nil {
		t.Fatalf("a UI folder with index.html was refused: %v", err)
	}
}

func TestRunRejectsBadUsageWithExitCode2(t *testing.T) {
	var stderr strings.Builder
	if code := run([]string{"--listen", "not-an-address"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if code := run([]string{"--no-such-flag"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("an unknown flag must exit 2, got %d", code)
	}
}
