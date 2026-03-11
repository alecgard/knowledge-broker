package connector

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeGitHubWiki is the source type identifier for GitHub wiki sources.
const SourceTypeGitHubWiki = "github_wiki"

func init() {
	Register(SourceTypeGitHubWiki, func(config map[string]string) (Connector, error) {
		return NewGitHubWikiConnector(config["url"], config["branch"], config["github_client_id"]), nil
	})
}

// GitHubWikiConnector clones a GitHub wiki repository (which is a git repo at
// {repoURL}.wiki.git) and scans it using FilesystemConnector.
type GitHubWikiConnector struct {
	repoURL  string
	branch   string
	clientID string // GitHub OAuth client ID for device flow auth
}

// NewGitHubWikiConnector creates a connector for the wiki of the given GitHub repo URL.
// repoURL should be the main repository URL (e.g., "https://github.com/owner/repo").
func NewGitHubWikiConnector(repoURL, branch, clientID string) *GitHubWikiConnector {
	return &GitHubWikiConnector{
		repoURL:  strings.TrimSuffix(repoURL, "/"),
		branch:   branch,
		clientID: clientID,
	}
}

// Name returns the connector type identifier.
func (c *GitHubWikiConnector) Name() string {
	return SourceTypeGitHubWiki
}

// SourceName returns a human-readable name like "owner/repo/wiki".
func (c *GitHubWikiConnector) SourceName() string {
	u := strings.TrimSuffix(c.repoURL, ".git")
	parsed, err := url.Parse(u)
	if err == nil && parsed.Host != "" {
		path := strings.Trim(parsed.Path, "/")
		parts := strings.SplitN(path, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1] + "/wiki"
		}
	}
	return u + "/wiki"
}

// Config returns the connector's configuration for source registration.
func (c *GitHubWikiConnector) Config(mode string) map[string]string {
	cfg := map[string]string{"url": c.repoURL}
	if c.branch != "" {
		cfg["branch"] = c.branch
	}
	if c.clientID != "" {
		cfg["github_client_id"] = c.clientID
	}
	return cfg
}

// WikiCloneURL returns the git clone URL for the wiki repository.
// GitHub wikis are stored at {repoURL}.wiki.git.
func (c *GitHubWikiConnector) WikiCloneURL() string {
	u := strings.TrimSuffix(c.repoURL, ".git")
	return u + ".wiki.git"
}

// Scan clones the wiki repo and scans it using FilesystemConnector.
func (c *GitHubWikiConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	cloneURL, err := c.authenticatedURL(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve auth: %w", err)
	}

	// Clone to temp dir.
	tmpDir, err := os.MkdirTemp("", "kb-github-wiki-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(os.Stderr, "Cloning wiki for %s...\n", c.repoURL)

	cloneArgs := []string{"clone", "--depth", "1"}
	if c.branch != "" {
		cloneArgs = append(cloneArgs, "--branch", c.branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git clone wiki: %w (does the wiki exist for %s?)", err, c.repoURL)
	}

	// Translate known map: relative paths -> absolute tmpDir paths for FS connector.
	fsOpts := ScanOptions{}
	if len(opts.Known) > 0 {
		fsOpts.Known = make(map[string]string, len(opts.Known))
		for relPath, checksum := range opts.Known {
			fsOpts.Known[filepath.Join(tmpDir, relPath)] = checksum
		}
	}

	// Delegate to filesystem connector.
	fs := NewFilesystemConnector(tmpDir)
	docs, deleted, err := fs.Scan(ctx, fsOpts)
	if err != nil {
		return nil, nil, err
	}

	// Rewrite source metadata.
	baseURI := strings.TrimSuffix(c.repoURL, ".git")
	sourceName := c.SourceName()
	for i := range docs {
		relPath, _ := filepath.Rel(tmpDir, docs[i].Path)
		docs[i].Path = relPath
		docs[i].SourceType = SourceTypeGitHubWiki
		docs[i].SourceName = sourceName
		docs[i].SourceURI = baseURI + "/wiki/" + wikiPageName(relPath)
	}

	// Rewrite deleted paths from absolute to relative.
	for i := range deleted {
		relPath, err := filepath.Rel(tmpDir, deleted[i])
		if err == nil {
			deleted[i] = relPath
		}
	}

	return docs, deleted, nil
}

// wikiPageName derives the wiki page name from a file path.
// For example, "Getting-Started.md" becomes "Getting-Started",
// and "sub/Page.md" becomes "sub/Page".
func wikiPageName(relPath string) string {
	ext := filepath.Ext(relPath)
	if ext != "" {
		return strings.TrimSuffix(relPath, ext)
	}
	return relPath
}

// authenticatedURL returns the wiki clone URL with embedded token if available.
func (c *GitHubWikiConnector) authenticatedURL(ctx context.Context) (string, error) {
	wikiURL := c.WikiCloneURL()

	if !strings.HasPrefix(wikiURL, "https://") {
		return wikiURL, nil
	}

	token := c.resolveToken(ctx)
	if token == "" {
		return wikiURL, nil
	}

	parsed, err := url.Parse(wikiURL)
	if err != nil {
		return wikiURL, nil
	}
	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

// resolveToken attempts to find a GitHub token.
func (c *GitHubWikiConnector) resolveToken(ctx context.Context) string {
	if token := os.Getenv("KB_GITHUB_TOKEN"); token != "" {
		return token
	}

	if cached := loadCachedToken(); cached != "" {
		return cached
	}

	if token := ghCLIToken(); token != "" {
		return token
	}

	if c.clientID != "" {
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
