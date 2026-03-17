package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeGit is the source type identifier for git repository sources.
const SourceTypeGit = "git"

func init() {
	Register(SourceTypeGit, func(config map[string]string) (Connector, error) {
		c := NewGitConnector(config["url"], config["branch"], config["github_client_id"])
		c.lastCommit = config["last_commit"]
		return c, nil
	})
}

// GitConnector clones a git repository and scans it using FilesystemConnector.
// Works with any git remote: GitHub, GitLab, Bitbucket, self-hosted, etc.
type GitConnector struct {
	repoURL    string
	branch     string
	clientID   string // GitHub OAuth client ID (only used for github.com URLs)
	lastCommit string // SHA of the last successfully ingested commit
	headCommit string // SHA of HEAD after clone (populated by Scan)
}

// NewGitConnector creates a connector for the given git remote URL.
// clientID is optional and only used for GitHub device flow auth.
func NewGitConnector(repoURL, branch, clientID string) *GitConnector {
	return &GitConnector{
		repoURL:  repoURL,
		branch:   branch,
		clientID: clientID,
	}
}

// Name returns the connector type identifier.
func (c *GitConnector) Name() string {
	return SourceTypeGit
}

// Config returns the connector's configuration for source registration.
func (c *GitConnector) Config(mode string) map[string]string {
	cfg := map[string]string{"url": c.repoURL}
	if c.branch != "" {
		cfg["branch"] = c.branch
	}
	if c.clientID != "" {
		cfg["github_client_id"] = c.clientID
	}
	// Store HEAD commit SHA so next ingest can use diff-based scan.
	if c.headCommit != "" {
		cfg["last_commit"] = c.headCommit
	}
	return cfg
}

// Scan clones the repo to a temp dir and returns new/changed documents.
// When a previous commit SHA is available (from a prior ingest), it uses
// git diff to identify only the changed files, avoiding a full filesystem scan.
func (c *GitConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	cloneURL, err := c.authenticatedURL(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve auth: %w", err)
	}

	// Clone to temp dir.
	tmpDir, err := os.MkdirTemp("", "kb-git-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(os.Stderr, "Cloning %s...\n", c.repoURL)

	// If we have a previous commit, do a blobless clone so we can diff.
	// Otherwise, shallow clone for speed.
	useDiff := c.lastCommit != "" && !opts.Force
	cloneArgs := []string{"clone"}
	if useDiff {
		cloneArgs = append(cloneArgs, "--filter=blob:none", "--no-checkout")
	} else {
		cloneArgs = append(cloneArgs, "--depth", "1")
	}
	if c.branch != "" {
		cloneArgs = append(cloneArgs, "--branch", c.branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git clone: %w", err)
	}

	// Record HEAD commit SHA for next ingest.
	c.headCommit = gitHead(ctx, tmpDir)

	// Try diff-based scan if we have a previous commit.
	if useDiff {
		docs, deleted, err := c.diffScan(ctx, tmpDir, opts)
		if err == nil {
			return docs, deleted, nil
		}
		// Diff failed (e.g. last_commit unreachable after force push).
		// Fall back to full scan.
		fmt.Fprintf(os.Stderr, "Diff-based scan failed (%v), falling back to full scan\n", err)
	}

	// Full checkout for full scan.
	if useDiff {
		// We cloned with --no-checkout for diff; need to checkout now.
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "HEAD")
		checkoutCmd.Dir = tmpDir
		checkoutCmd.Stderr = os.Stderr
		if err := checkoutCmd.Run(); err != nil {
			return nil, nil, fmt.Errorf("git checkout: %w", err)
		}
	}

	return c.fullScan(ctx, tmpDir, opts)
}

// diffScan uses git diff to find only changed files since the last ingested commit.
func (c *GitConnector) diffScan(ctx context.Context, tmpDir string, opts ScanOptions) ([]model.RawDocument, []string, error) {
	// Check if lastCommit is reachable.
	checkCmd := exec.CommandContext(ctx, "git", "cat-file", "-t", c.lastCommit)
	checkCmd.Dir = tmpDir
	if err := checkCmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("last commit %s not reachable", c.lastCommit[:8])
	}

	// Get list of changed files.
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--name-status", c.lastCommit+"..HEAD")
	diffCmd.Dir = tmpDir
	out, err := diffCmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git diff: %w", err)
	}

	var changedPaths []string
	var deleted []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status, path := parts[0], parts[1]
		switch {
		case strings.HasPrefix(status, "D"):
			deleted = append(deleted, path)
		case strings.HasPrefix(status, "A"), strings.HasPrefix(status, "M"),
			strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			// For renames (R100\told\tnew), the path is tab-separated.
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				// path contains "old\tnew", we want the new path.
				renameParts := strings.SplitN(path, "\t", 2)
				if len(renameParts) == 2 {
					deleted = append(deleted, renameParts[0])
					path = renameParts[1]
				}
			}
			changedPaths = append(changedPaths, path)
		}
	}

	if c.headCommit == c.lastCommit {
		fmt.Fprintf(os.Stderr, "No changes since last ingest\n")
		return nil, nil, nil
	}

	fmt.Fprintf(os.Stderr, "Diff: %d changed, %d deleted (since %s)\n",
		len(changedPaths), len(deleted), c.lastCommit[:8])

	if len(changedPaths) == 0 {
		return nil, deleted, nil
	}

	// Checkout only the changed files (sparse checkout of specific paths).
	checkoutArgs := append([]string{"checkout", "HEAD", "--"}, changedPaths...)
	checkoutCmd := exec.CommandContext(ctx, "git", checkoutArgs...)
	checkoutCmd.Dir = tmpDir
	checkoutCmd.Stderr = os.Stderr
	if err := checkoutCmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git checkout changed files: %w", err)
	}

	// Build documents from only the changed files.
	sourceURI := c.baseSourceURI()
	sourceName := c.SourceName()
	var docs []model.RawDocument

	fs := NewFilesystemConnector(tmpDir)
	for _, relPath := range changedPaths {
		absPath := filepath.Join(tmpDir, relPath)
		doc, err := fs.ReadDocument(absPath)
		if err != nil {
			continue // file might be binary or unreadable
		}
		doc.Path = relPath
		doc.SourceType = SourceTypeGit
		doc.SourceName = sourceName
		doc.SourceURI = sourceURI + "/" + relPath
		docs = append(docs, doc)
	}

	return docs, deleted, nil
}

// fullScan walks the entire repository (original behavior).
func (c *GitConnector) fullScan(ctx context.Context, tmpDir string, opts ScanOptions) ([]model.RawDocument, []string, error) {
	// Rewrite known keys from relative paths to absolute (matching filesystem walk).
	if len(opts.Known) > 0 {
		abs := make(map[string]string, len(opts.Known))
		for rel, checksum := range opts.Known {
			abs[filepath.Join(tmpDir, rel)] = checksum
		}
		opts.Known = abs
	}

	// Delegate to filesystem connector.
	fs := NewFilesystemConnector(tmpDir)
	docs, deleted, err := fs.Scan(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	// Convert deleted paths back to relative (fs.Scan returns absolute temp paths).
	for i := range deleted {
		if rel, err := filepath.Rel(tmpDir, deleted[i]); err == nil {
			deleted[i] = rel
		}
	}

	// Rewrite source metadata to reflect the git remote, not the temp dir.
	sourceURI := c.baseSourceURI()
	sourceName := c.SourceName()
	for i := range docs {
		relPath, _ := filepath.Rel(tmpDir, docs[i].Path)
		docs[i].Path = relPath
		docs[i].SourceType = SourceTypeGit
		docs[i].SourceName = sourceName
		docs[i].SourceURI = sourceURI + "/" + relPath
	}

	return docs, deleted, nil
}

// gitHead returns the HEAD commit SHA in the given repo directory.
func gitHead(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// authenticatedURL returns the clone URL with embedded token if available.
func (c *GitConnector) authenticatedURL(ctx context.Context) (string, error) {
	// Only attempt token auth for HTTPS URLs.
	if !strings.HasPrefix(c.repoURL, "https://") {
		return c.repoURL, nil // SSH URLs handle auth via ssh-agent/keys
	}

	token := c.resolveToken(ctx)
	if token == "" {
		return c.repoURL, nil // Public repo or no auth available
	}

	// Embed token in URL: https://token@github.com/owner/repo.git
	parsed, err := url.Parse(c.repoURL)
	if err != nil {
		return c.repoURL, nil
	}
	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

// resolveToken attempts to find a token for the git host.
// Returns empty string if no token is available (public repo access).
func (c *GitConnector) resolveToken(ctx context.Context) string {
	// Check explicit env vars.
	if token := os.Getenv("KB_GITHUB_TOKEN"); token != "" && c.isGitHub() {
		return token
	}
	if token := os.Getenv("KB_GITLAB_TOKEN"); token != "" && c.isGitLab() {
		return token
	}
	if token := os.Getenv("KB_GIT_TOKEN"); token != "" {
		return token
	}

	// Check cached GitHub token.
	if c.isGitHub() {
		if cached := loadCachedToken(); cached != "" {
			return cached
		}
	}

	// Try gh CLI for GitHub repos.
	if c.isGitHub() {
		if token := ghCLIToken(); token != "" {
			return token
		}
	}

	// Try GitHub device flow.
	if c.isGitHub() && c.clientID != "" {
		token, err := deviceFlowAuth(ctx, c.clientID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: device flow auth failed: %v\n", err)
			return ""
		}
		if err := saveCachedToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to cache token: %v\n", err)
		}
		return token
	}

	return ""
}

func (c *GitConnector) isGitHub() bool {
	return strings.Contains(c.repoURL, "github.com")
}

func (c *GitConnector) isGitLab() bool {
	return strings.Contains(c.repoURL, "gitlab.com") || strings.Contains(c.repoURL, "gitlab.")
}

// baseSourceURI returns a browsable base URI for the repo.
func (c *GitConnector) baseSourceURI() string {
	// For HTTPS URLs, strip .git suffix for a browsable link.
	uri := strings.TrimSuffix(c.repoURL, ".git")
	if c.isGitHub() || c.isGitLab() {
		return uri + "/blob/" + c.branchOrDefault()
	}
	return uri
}

// SourceName derives a human-readable source name from the repo URL.
// For GitHub URLs like "https://github.com/owner/repo", it returns "owner/repo".
// For other URLs, it returns the repo name (last path segment without .git).
func (c *GitConnector) SourceName() string {
	u := strings.TrimSuffix(c.repoURL, ".git")

	// Try to parse as URL and extract path.
	parsed, err := url.Parse(u)
	if err == nil && parsed.Host != "" {
		path := strings.Trim(parsed.Path, "/")
		// For GitHub/GitLab, return "owner/repo".
		if c.isGitHub() || c.isGitLab() {
			parts := strings.SplitN(path, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
		// For other hosts, return just the repo name.
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			return path[idx+1:]
		}
		return path
	}

	// SSH-style URLs like git@github.com:owner/repo.git
	if idx := strings.Index(u, ":"); idx >= 0 {
		path := u[idx+1:]
		return path
	}

	return u
}

func (c *GitConnector) branchOrDefault() string {
	if c.branch != "" {
		return c.branch
	}
	return "main"
}

// --- Shared auth helpers (also used by device flow) ---

// ghCLIToken attempts to get a token from the gh CLI.
func ghCLIToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tokenCachePath returns the path to the cached GitHub token file.
func tokenCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "kb", "connectors", "github", "token")
}

// loadCachedToken reads the cached GitHub token, if any.
func loadCachedToken() string {
	path := tokenCachePath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveCachedToken writes the GitHub token to the cache file.
func saveCachedToken(token string) error {
	path := tokenCachePath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	return os.WriteFile(path, []byte(token+"\n"), 0600)
}

// --- GitHub Device Flow ---

type deviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type deviceFlowTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
}

func deviceFlowAuth(ctx context.Context, clientID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/device/code", strings.NewReader(url.Values{
		"client_id": {clientID},
		"scope":     {"repo"},
	}.Encode()))
	if err != nil {
		return "", fmt.Errorf("create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(body))
	}

	var deviceResp deviceFlowResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return "", fmt.Errorf("parse device code response: %w", err)
	}

	fmt.Fprintf(os.Stderr, "To authenticate with GitHub, visit: %s\nEnter code: %s\n",
		deviceResp.VerificationURI, deviceResp.UserCode)

	interval := deviceResp.Interval
	if interval < 5 {
		interval = 5
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(url.Values{
			"client_id":   {clientID},
			"device_code": {deviceResp.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}.Encode()))
		if err != nil {
			return "", fmt.Errorf("create token request: %w", err)
		}
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		tokenReq.Header.Set("Accept", "application/json")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			return "", fmt.Errorf("poll for token: %w", err)
		}

		tokenBody, err := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read token response: %w", err)
		}

		var tokenResult deviceFlowTokenResponse
		if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
			return "", fmt.Errorf("parse token response: %w", err)
		}

		switch tokenResult.Error {
		case "":
			if tokenResult.AccessToken != "" {
				return tokenResult.AccessToken, nil
			}
			return "", fmt.Errorf("received empty access token")
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		case "expired_token":
			return "", fmt.Errorf("device code expired; please try again")
		case "access_denied":
			return "", fmt.Errorf("access denied by user")
		default:
			return "", fmt.Errorf("unexpected error: %s", tokenResult.Error)
		}
	}
}
