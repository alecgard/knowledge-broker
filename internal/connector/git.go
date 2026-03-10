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
		return NewGitConnector(config["url"], config["branch"], config["github_client_id"]), nil
	})
}

// GitConnector clones a git repository and scans it using FilesystemConnector.
// Works with any git remote: GitHub, GitLab, Bitbucket, self-hosted, etc.
type GitConnector struct {
	repoURL  string
	branch   string
	clientID string // GitHub OAuth client ID (only used for github.com URLs)
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
	return cfg
}

// Scan clones the repo to a temp dir and delegates to FilesystemConnector.
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

	cloneArgs := []string{"clone", "--depth", "1"}
	if c.branch != "" {
		cloneArgs = append(cloneArgs, "--branch", c.branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git clone: %w", err)
	}

	// Delegate to filesystem connector.
	fs := NewFilesystemConnector(tmpDir)
	docs, deleted, err := fs.Scan(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	// Rewrite source metadata to reflect the git remote, not the temp dir.
	sourceURI := c.baseSourceURI()
	sourceName := c.SourceName()
	for i := range docs {
		// Convert absolute temp path to relative path within repo.
		relPath, _ := filepath.Rel(tmpDir, docs[i].Path)
		docs[i].Path = relPath
		docs[i].SourceType = SourceTypeGit
		docs[i].SourceName = sourceName
		docs[i].SourceURI = sourceURI + "/" + relPath
	}

	return docs, deleted, nil
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
	if token := envOrFallback("KB_GITHUB_TOKEN", "GITHUB_TOKEN"); token != "" && c.isGitHub() {
		return token
	}
	if token := envOrFallback("KB_GITLAB_TOKEN", "GITLAB_TOKEN"); token != "" && c.isGitLab() {
		return token
	}
	if token := envOrFallback("KB_GIT_TOKEN", "GIT_TOKEN"); token != "" {
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
