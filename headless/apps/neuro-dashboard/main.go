// neuro-dashboard is the operator's UI, as a program of its own.
//
// It serves the built dashboard (the frontend folder, with index.html) at /ui/,
// and it forwards the dashboard's API calls (/api/... and /health) to a
// neuro-integration server. It holds no policy, no audit log, and no secret. The
// operator types the dashboard token into the sign-in page, and this program passes
// it through unchanged. It never adds a token of its own.
//
// The dashboard can run on the operator's PC, or anywhere that can reach the
// server. It does not run on the PC that Neuro controls.
//
//	neuro-dashboard --server http://127.0.0.1:8300 --listen 127.0.0.1:8310
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultListen = "127.0.0.1:8310"
	defaultServer = "http://127.0.0.1:8300"
)

// config is what the dashboard needs to start. It is built from flags and the
// environment, and checked before anything listens.
type config struct {
	listen string
	server *url.URL
	uiDir  string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses the arguments, serves until the process stops, and returns the exit
// code: 0 for a normal stop, 1 for a startup failure, 2 for bad usage.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("neuro-dashboard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", envOr("NEURO_DASHBOARD_LISTEN", defaultListen), "address the dashboard listens on")
	server := flags.String("server", envOr("NEURO_DASHBOARD_SERVER", defaultServer), "the neuro-integration server; /api and /health are forwarded to it")
	uiDir := flags.String("ui-dir", envOr("NEURO_UI_DIR", ""), "the built dashboard folder (contains index.html); found automatically when empty")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	cfg, err := newConfig(*listen, *server, *uiDir)
	if err != nil {
		fmt.Fprintf(stderr, "neuro-dashboard: %v\n", err)
		return 2
	}

	log.SetOutput(stderr)
	log.Printf("neuro-dashboard: dashboard at http://%s/ui/", cfg.listen)
	log.Printf("neuro-dashboard: API forwarded to %s", cfg.server)
	log.Printf("neuro-dashboard: UI from %s", cfg.uiDir)
	warnAboutExposure(cfg)

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           newHandler(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "neuro-dashboard: %v\n", err)
		return 1
	}
	return 0
}

// newConfig checks the settings. The server address must be an http or https URL
// with a host. The UI folder must exist and contain index.html.
func newConfig(listen, server, uiDir string) (config, error) {
	listen = strings.TrimSpace(listen)
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return config{}, fmt.Errorf("--listen %q must be host:port: %w", listen, err)
	}

	target, err := url.Parse(strings.TrimSpace(server))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return config{}, fmt.Errorf("--server %q must be an http:// or https:// URL, for example http://127.0.0.1:8300", server)
	}
	target.Path = strings.TrimSuffix(target.Path, "/")

	dir, err := findUIDir(uiDir)
	if err != nil {
		return config{}, err
	}
	return config{listen: listen, server: target, uiDir: dir}, nil
}

// findUIDir returns the built dashboard folder. An explicit folder must hold
// index.html. Without one, the usual places are tried, next to the program first.
func findUIDir(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		if hasIndex(explicit) {
			return filepath.Abs(explicit)
		}
		return "", fmt.Errorf("--ui-dir %q has no index.html; point it at the built frontend folder", explicit)
	}

	candidates := []string{
		"frontend",
		filepath.Join("frontend", "dist"),
		filepath.Join("desktop", "frontend", "dist"),
		filepath.Join("..", "..", "..", "desktop", "frontend", "dist"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "frontend"), filepath.Join(dir, "frontend", "dist"))
	}
	for _, candidate := range candidates {
		if hasIndex(candidate) {
			return filepath.Abs(candidate)
		}
	}
	return "", errors.New("no dashboard found: pass --ui-dir with the built frontend folder (it contains index.html)")
}

func hasIndex(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !info.IsDir()
}

// newHandler routes requests. Only /ui/, /api/ and /health are served, and only
// the API routes reach the server. Everything else is a 404.
func newHandler(cfg config) http.Handler {
	mux := http.NewServeMux()
	api := newAPIProxy(cfg.server)
	mux.Handle("/api/", api)
	mux.Handle("/health", api)
	mux.Handle("/ui/", uiHandler(cfg.uiDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
	return withHeaders(mux)
}

// newAPIProxy forwards a request to the server unchanged, with its headers. The
// dashboard token is the browser's to send, and it is passed through as it arrives.
// Answers are never cached, because they are live state.
func newAPIProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = target.Host
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Header.Get("Cache-Control") == "" {
			resp.Header.Set("Cache-Control", "no-store")
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeJSONError(w, http.StatusBadGateway,
			fmt.Sprintf("the Neuro Desktop server at %s did not answer (%v); start it, or check --server", target, err))
	}
	return proxy
}

// uiHandler serves the built dashboard. A file that exists is served as it is. A
// path that is not a file gets the shell (index.html), so the single-page app can
// handle it. The path is cleaned, so nothing outside the folder can be named.
func uiHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/ui/")
		if rel == "" {
			serveShell(w, r, root)
			return
		}
		clean := path.Clean("/" + rel)
		full := filepath.Join(root, filepath.FromSlash(clean))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			http.ServeFile(w, r, full)
			return
		}
		serveShell(w, r, root)
	})
}

// serveShell sends index.html. It is read on every request, so a rebuilt dashboard
// is picked up without restarting, and the browser is told not to keep it.
func serveShell(w http.ResponseWriter, r *http.Request, root string) {
	data, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		http.Error(w, "the dashboard's index.html is missing", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "{\"ok\": false, \"error\": %q}\n", message)
}

// warnAboutExposure says plainly when the dashboard or the server is reached in a
// way that a stranger on the network could use, or that would send the token in
// the clear. It does not stop the program: the operator may know better.
func warnAboutExposure(cfg config) {
	host, _, _ := net.SplitHostPort(cfg.listen)
	if !isLoopbackHost(host) {
		log.Printf("neuro-dashboard: WARNING: the dashboard listens on %s, so other machines can reach the page. It holds no secret, but every API call it forwards still needs the dashboard token.", cfg.listen)
	}
	if cfg.server.Scheme == "http" && !isLoopbackHost(cfg.server.Hostname()) {
		log.Printf("neuro-dashboard: WARNING: the server is reached over plain HTTP at %s. The dashboard token and the replies cross the network unencrypted. Tunnel the port over SSH, or put TLS in front of the server.", cfg.server.Host)
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
