package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	neuro "github.com/cassitly/neuro-integration-sdk"
)

// Extensions are catalog items that have been installed. Installing never takes
// a URL from the model or the dashboard: the id is looked up in the catalog
// index, and NEURO_EXTENSION_INSTALL_MODE decides what happens next.
//
//	metadata_only  record the install and fetch nothing (the default)
//	git_clone      fetch the item's pinned commit from its https repository
//
// A catalog item that is signed by a publisher in publishers.json is "verified".
// A tampered item (a signature that does not match) is refused in every mode.
// Fetching code also requires a verified item, unless NEURO_EXTENSIONS_ALLOW_UNSIGNED
// is set for a lab machine. See catalog_trust.go for the signing rules.

const (
	installModeMetadata = "metadata_only"
	installModeGitClone = "git_clone"
)

var (
	extensionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	gitCommitPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type ExtensionStateFile struct {
	Installed map[string]ExtensionInstallState `json:"installed"`
}

type ExtensionInstallState struct {
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at"`
	Source      string `json:"source,omitempty"`
	Path        string `json:"path,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Commit      string `json:"commit,omitempty"`
	// Trust is the signature state when the item was installed:
	// verified, unsigned, or untrusted (see catalog_trust.go).
	Trust string `json:"trust,omitempty"`
}

func extensionStatePath() string {
	path := strings.TrimSpace(os.Getenv("NEURO_EXTENSIONS_STATE_FILE"))
	if path != "" {
		return path
	}
	return "./catalog/extensions-state.json"
}

func extensionInstallMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("NEURO_EXTENSION_INSTALL_MODE")))
	if mode == "" {
		return installModeMetadata
	}
	return mode
}

// extensionAllowsUnsigned, the lab escape hatch, is defined per build: see
// extensions_unsigned_dev.go (-tags neurodev) and extensions_unsigned_release.go.
// It is never true in a release build.

func extensionRootPath() string {
	root := strings.TrimSpace(os.Getenv("NEURO_EXTENSION_DIR"))
	if root != "" {
		return root
	}
	return "./plugins"
}

func loadExtensionState(path string) (ExtensionStateFile, error) {
	state := ExtensionStateFile{
		Installed: map[string]ExtensionInstallState{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return ExtensionStateFile{}, fmt.Errorf("failed to read extension state file: %w", err)
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return ExtensionStateFile{}, fmt.Errorf("failed to parse extension state file: %w", err)
	}

	if state.Installed == nil {
		state.Installed = map[string]ExtensionInstallState{}
	}
	return state, nil
}

func saveExtensionState(path string, state ExtensionStateFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize extension state: %w", err)
	}
	if err := atomicWriteFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write extension state file: %w", err)
	}
	return nil
}

// normalizeExtensionID accepts only plain ids. An id is also a directory name,
// so separators and dot-dot are refused here, before any path is built.
func normalizeExtensionID(itemID string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(itemID))
	if !extensionIDPattern.MatchString(id) {
		return "", fmt.Errorf("%q is not a valid extension id (lowercase letters, digits, dots, dashes and underscores)", itemID)
	}
	return id, nil
}

func findCatalogItemByID(index CatalogIndex, itemID string) *CatalogItem {
	target := strings.ToLower(strings.TrimSpace(itemID))
	if target == "" {
		return nil
	}

	for i := range index.Items {
		if strings.ToLower(strings.TrimSpace(index.Items[i].ID)) == target {
			return &index.Items[i]
		}
	}

	return nil
}

// pathInsideRoot reports whether target is strictly inside root. It compares
// path components, so "/plugins-evil" is not inside "/plugins", and the root
// itself is not inside the root.
func pathInsideRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// requireHTTPSRepo accepts an https repository URL with no embedded credentials.
func requireHTTPSRepo(raw string) (string, error) {
	repo := strings.TrimSpace(raw)
	parsed, err := url.Parse(repo)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("only https:// repositories without embedded credentials can be installed (catalog repository: %q)", raw)
	}
	return repo, nil
}

// runGit runs git and returns trimmed stdout. Stderr is folded into the error,
// and terminal prompts are disabled so a bad URL cannot hang the bridge.
func runGit(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	gitArgs := append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir="}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.WaitDelay = 3 * time.Second
	// Source staging must not inherit user hooks, filter commands, credential
	// helpers, alternate work trees, or command-line config from its parent.
	for _, pair := range os.Environ() {
		name := strings.SplitN(pair, "=", 2)[0]
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			cmd.Env = append(cmd.Env, pair)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("git %s exceeded the two-minute timeout", gitSubcommand(args))
	}
	if err != nil {
		return "", fmt.Errorf("git %s failed: %v (%s)", gitSubcommand(args), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitSubcommand names the git verb in args, skipping a leading -C <dir>.
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" {
			i++
			continue
		}
		return args[i]
	}
	return "command"
}

// fetchPinnedCommit places exactly one commit of repository at dest. It fetches
// only that commit (depth 1) and checks the result, so the installed code is the
// code that was signed. An existing checkout must already be at the same commit.
func fetchPinnedCommit(repository, commit, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		head, err := runGit("-C", dest, "rev-parse", "HEAD")
		if err != nil || head != commit {
			return fmt.Errorf("%s already exists at a different revision; uninstall it first", dest)
		}
		return nil
	}

	if _, err := runGit("init", "--quiet", dest); err != nil {
		return err
	}
	steps := [][]string{
		{"-C", dest, "remote", "add", "origin", repository},
		{"-C", dest, "fetch", "--quiet", "--depth", "1", "origin", commit},
		{"-C", dest, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	for _, args := range steps {
		if _, err := runGit(args...); err != nil {
			_ = os.RemoveAll(dest)
			return err
		}
	}

	head, err := runGit("-C", dest, "rev-parse", "HEAD")
	if err != nil || head != commit {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("the fetched revision %q does not match the pinned commit %s", head, commit)
	}
	return nil
}

func (n *NDIntegration) listInstalledExtensions() neuro.ExecutionResult {
	state, err := loadExtensionState(extensionStatePath())
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	if len(state.Installed) == 0 {
		return neuro.NewSuccessResult("No installed extensions")
	}

	ids := make([]string, 0, len(state.Installed))
	for id := range state.Installed {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	lines := make([]string, 0, len(ids))
	for _, itemID := range ids {
		install := state.Installed[itemID]
		status := "disabled"
		if install.Enabled {
			status = "enabled"
		}
		line := fmt.Sprintf("%s (%s)", itemID, status)
		if install.Trust != "" {
			line += " signature=" + install.Trust
		}
		if install.Source != "" {
			line += " source=" + install.Source
		}
		lines = append(lines, line)
	}

	return neuro.NewSuccessResult("Installed extensions -> " + strings.Join(lines, " | "))
}

func (n *NDIntegration) installExtension(itemID string) neuro.ExecutionResult {
	n.extensionMu.Lock()
	defer n.extensionMu.Unlock()
	id, err := normalizeExtensionID(itemID)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	index, err := loadCatalogIndex(catalogFilePath())
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	item := findCatalogItemByID(index, id)
	if item == nil {
		return neuro.NewFailureResult("Catalog extension not found: " + id)
	}

	mode := extensionInstallMode()
	if mode != installModeMetadata && mode != installModeGitClone {
		return neuro.NewFailureResult(fmt.Sprintf("Unknown NEURO_EXTENSION_INSTALL_MODE %q (use %s or %s)", mode, installModeMetadata, installModeGitClone))
	}

	trust := verifyCatalogItem(*item, loadPublishersOrEmpty())
	if trust.State == "invalid" {
		return neuro.NewFailureResult(fmt.Sprintf("Refusing to install %s: its catalog signature is invalid (%s). The catalog entry changed after it was signed.", id, trust.Detail))
	}

	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}
	if existing, ok := state.Installed[id]; ok {
		return neuro.NewSuccessResult(fmt.Sprintf("Extension %s is already installed (enabled=%t)", id, existing.Enabled))
	}

	source := strings.TrimSpace(item.Repository)
	if source == "" {
		source = "catalog"
	}
	install := ExtensionInstallState{
		// Installation never grants permission to start an extension.
		Enabled:     false,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      source,
		Publisher:   item.Publisher,
		Trust:       trust.State,
	}

	if mode == installModeGitClone {
		if trust.State != "verified" {
			if !extensionAllowsUnsigned() {
				return neuro.NewFailureResult(fmt.Sprintf("Refusing to fetch %s: its catalog signature is %s (%s). Only items signed by a publisher listed in publishers.json can fetch code.", id, trust.State, trust.Detail))
			}
			log.Printf("WARNING: fetching extension %s without a verified signature (NEURO_EXTENSIONS_ALLOW_UNSIGNED is set)", id)
		}

		repo, err := requireHTTPSRepo(item.Repository)
		if err != nil {
			return neuro.NewFailureResult(err.Error())
		}
		if !gitCommitPattern.MatchString(item.Commit) {
			return neuro.NewFailureResult(fmt.Sprintf("Catalog item %s must pin a 40-character lowercase commit in its \"commit\" field before it can be fetched", id))
		}

		root := extensionRootPath()
		installPath := filepath.Join(root, id)
		if !pathInsideRoot(root, installPath) {
			return neuro.NewFailureResult("Refusing to install outside the extension directory")
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to create extension directory: %v", err))
		}
		if err := fetchPinnedCommit(repo, item.Commit, installPath); err != nil {
			return neuro.NewFailureResult(err.Error())
		}
		install.Path = installPath
		install.Commit = item.Commit
	}

	state.Installed[id] = install
	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	return neuro.NewSuccessResult(fmt.Sprintf("Installed extension %s (mode %s, signature %s). It is disabled until its run permission and required capabilities are approved.", id, mode, trust.State))
}

func (n *NDIntegration) uninstallExtension(itemID string) neuro.ExecutionResult {
	n.extensionMu.Lock()
	defer n.extensionMu.Unlock()
	id, err := normalizeExtensionID(itemID)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	install, exists := state.Installed[id]
	if !exists {
		return neuro.NewFailureResult("Extension is not installed")
	}

	if install.Path != "" {
		root := extensionRootPath()
		if !pathInsideRoot(root, install.Path) {
			return neuro.NewFailureResult("Refusing to remove extension files outside the extension directory")
		}
		if err := os.RemoveAll(install.Path); err != nil {
			return neuro.NewFailureResult(fmt.Sprintf("Failed to remove extension files: %v", err))
		}
	}

	delete(state.Installed, id)
	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	go n.syncMCPServers()
	return neuro.NewSuccessResult(fmt.Sprintf("Uninstalled extension %s", id))
}

func (n *NDIntegration) setExtensionEnabled(itemID string, enabled bool) neuro.ExecutionResult {
	n.extensionMu.Lock()
	defer n.extensionMu.Unlock()
	id, err := normalizeExtensionID(itemID)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	statePath := extensionStatePath()
	state, err := loadExtensionState(statePath)
	if err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	install, exists := state.Installed[id]
	if !exists {
		return neuro.NewFailureResult("Extension is not installed")
	}
	if enabled {
		if err := n.extensionItemRuntimeAllowed(id); err != nil {
			return neuro.NewFailureResult(err.Error())
		}
	}

	install.Enabled = enabled
	state.Installed[id] = install

	if err := saveExtensionState(statePath, state); err != nil {
		return neuro.NewFailureResult(err.Error())
	}

	status := "disabled"
	if enabled {
		status = "enabled"
	}
	go n.syncMCPServers()
	return neuro.NewSuccessResult(fmt.Sprintf("Extension %s is now %s", id, status))
}

// extensionJob is an extension operation that Neuro asked for. It runs after the
// action has been acknowledged, because a git fetch can outlast Neuro's window
// for an action result. The outcome is sent to Neuro as a message.
type extensionJob struct {
	op string
	id string
}

func (n *NDIntegration) runExtensionJob(job *extensionJob) {
	var result neuro.ExecutionResult
	action := string(CmdInstallExtension)
	if job.op == "stage" {
		action = string(CmdStageIntegration)
	}
	if err := n.checkExtensionManagement(job.id); err != nil {
		result = neuro.NewFailureResult(err.Error())
	} else if policy := n.policy(); policy == nil || !policy.IsAllowed(action) {
		result = neuro.NewFailureResult("the action is no longer allowed by the permission policy")
	} else if reason := n.stop.blockReason(action); reason != "" {
		result = neuro.NewFailureResult(reason)
	} else {
		switch job.op {
		case "install":
			result = n.installExtension(job.id)
		case "stage":
			result = n.stageMarketplaceSource(job.id)
		default:
			result = neuro.NewFailureResult("unknown extension operation " + job.op)
		}
	}

	if result.Successful {
		n.sendToNeuro(fmt.Sprintf("## %s finished\n\n- item: `%s`\n- result: %s", action, job.id, result.Message), false)
		return
	}
	n.sendToNeuro(fmt.Sprintf("## %s failed\n\n- item: `%s`\n- error: %s", action, job.id, result.Message), false)
}
