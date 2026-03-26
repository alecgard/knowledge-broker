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

// SourceTypeGit is the source type identifier for git repository sources.
const SourceTypeGit = "git"

func init() {
	Register(SourceTypeGit, func(config map[string]string) (Connector, error) {
		if config["url"] == "" {
			return nil, fmt.Errorf("git source missing 'url' in config")
		}
		c := NewGitConnector(config["url"], config["branch"])
		c.commit = config["commit"]
		c.lastCommit = config["last_commit"]
		c.token = config["token"]
		return c, nil
	})
}

// GitConnector clones a git repository and scans it using FilesystemConnector.
// Works with any git remote: GitHub, GitLab, Bitbucket, self-hosted, etc.
type GitConnector struct {
	repoURL    string
	branch     string
	commit     string // pin to a specific commit SHA (optional)
	lastCommit string // SHA of the last successfully ingested commit
	headCommit string // SHA of HEAD after clone (populated by Scan)
	token      string // explicit token (e.g. from UI paste flow)
}

// NewGitConnector creates a connector for the given git remote URL.
func NewGitConnector(repoURL, branch string) *GitConnector {
	return &GitConnector{
		repoURL: repoURL,
		branch:  branch,
	}
}

// SetCommit pins the connector to a specific commit SHA.
func (c *GitConnector) SetCommit(sha string) {
	c.commit = sha
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
	if c.commit != "" {
		cfg["commit"] = c.commit
	}
	// Store HEAD commit SHA so next ingest can use diff-based scan.
	if c.headCommit != "" {
		cfg["last_commit"] = c.headCommit
	}
	if mode != model.SourceModePush && c.token != "" {
		cfg["token"] = c.token
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

	if c.commit != "" {
		fmt.Fprintf(os.Stderr, "Cloning %s at %s...\n", c.repoURL, c.commit)
	} else {
		fmt.Fprintf(os.Stderr, "Cloning %s...\n", c.repoURL)
	}

	// If pinned to a specific commit, we need a full clone to reach it.
	// If we have a previous commit, do a blobless clone so we can diff.
	// Otherwise, shallow clone for speed.
	pinned := c.commit != ""
	useDiff := c.lastCommit != "" && !opts.Force && !pinned
	cloneArgs := []string{"clone"}
	if pinned {
		// Treeless clone: downloads commit objects but defers tree/blob fetching,
		// which is much faster for large repos when checking out a single commit.
		cloneArgs = append(cloneArgs, "--filter=tree:0", "--no-checkout")
	} else if useDiff {
		cloneArgs = append(cloneArgs, "--filter=blob:none", "--no-checkout")
	} else {
		cloneArgs = append(cloneArgs, "--depth", "1")
	}
	if c.branch != "" {
		cloneArgs = append(cloneArgs, "--branch", c.branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tmpDir)

	pw := newPrefixWriter(fmt.Sprintf("  [%s] ", c.SourceName()), os.Stderr)
	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	cmd.Stderr = pw
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git clone: %w", err)
	}

	// Checkout pinned commit if specified.
	if pinned {
		checkoutCmd := exec.CommandContext(ctx, "git", "-c", "advice.detachedHead=false", "checkout", c.commit)
		checkoutCmd.Dir = tmpDir
		checkoutCmd.Stderr = pw
		if err := checkoutCmd.Run(); err != nil {
			return nil, nil, fmt.Errorf("git checkout %s: %w", c.commit, err)
		}
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
		fmt.Fprintf(os.Stderr, "%s: Diff-based scan failed (%v), falling back to full scan\n", c.SourceName(), err)
	}

	// Full checkout for full scan.
	if useDiff {
		// We cloned with --no-checkout for diff; need to checkout now.
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "HEAD")
		checkoutCmd.Dir = tmpDir
		checkoutCmd.Stderr = newPrefixWriter(fmt.Sprintf("  [%s] ", c.SourceName()), os.Stderr)
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
		fmt.Fprintf(os.Stderr, "%s: No changes since last ingest\n", c.SourceName())
		return nil, nil, nil
	}

	fmt.Fprintf(os.Stderr, "%s: Diff: %d changed, %d deleted (since %s)\n",
		c.SourceName(), len(changedPaths), len(deleted), c.lastCommit[:8])

	if len(changedPaths) == 0 {
		return nil, deleted, nil
	}

	// Checkout only the changed files (sparse checkout of specific paths).
	checkoutArgs := append([]string{"checkout", "HEAD", "--"}, changedPaths...)
	checkoutCmd := exec.CommandContext(ctx, "git", checkoutArgs...)
	checkoutCmd.Dir = tmpDir
	checkoutCmd.Stderr = newPrefixWriter(fmt.Sprintf("  [%s] ", c.SourceName()), os.Stderr)
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

	// Delegate to filesystem connector. Skip per-file git log calls since
	// this is a temp clone — the metadata would be from the shallow copy and
	// spawning git-log for every file is prohibitively slow for large repos.
	fs := NewFilesystemConnector(tmpDir)
	fs.SkipGitMeta = true
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

// ScanStream returns a channel of ScanEvents for streaming ingestion.
// For full scans, it delegates to FilesystemConnector.ScanStream and rewrites
// metadata. For diff scans (small change sets), it wraps the existing Scan result.
func (c *GitConnector) ScanStream(ctx context.Context, opts ScanOptions) <-chan ScanEvent {
	ch := make(chan ScanEvent, 64)

	go func() {
		defer close(ch)

		cloneURL, err := c.authenticatedURL(ctx)
		if err != nil {
			ch <- ScanEvent{Err: fmt.Errorf("resolve auth: %w", err)}
			return
		}

		tmpDir, err := os.MkdirTemp("", "kb-git-*")
		if err != nil {
			ch <- ScanEvent{Err: fmt.Errorf("create temp dir: %w", err)}
			return
		}

		if c.commit != "" {
			fmt.Fprintf(os.Stderr, "Cloning %s at %s...\n", c.repoURL, c.commit)
		} else {
			fmt.Fprintf(os.Stderr, "Cloning %s...\n", c.repoURL)
		}

		pinned := c.commit != ""
		useDiff := c.lastCommit != "" && !opts.Force && !pinned

		cloneArgs := []string{"clone"}
		if pinned {
			cloneArgs = append(cloneArgs, "--filter=tree:0", "--no-checkout")
		} else if useDiff {
			cloneArgs = append(cloneArgs, "--filter=blob:none", "--no-checkout")
		} else {
			cloneArgs = append(cloneArgs, "--depth", "1")
		}
		if c.branch != "" {
			cloneArgs = append(cloneArgs, "--branch", c.branch)
		}
		cloneArgs = append(cloneArgs, cloneURL, tmpDir)

		pw := newPrefixWriter(fmt.Sprintf("  [%s] ", c.SourceName()), os.Stderr)
		cmd := exec.CommandContext(ctx, "git", cloneArgs...)
		cmd.Stderr = pw
		if err := cmd.Run(); err != nil {
			os.RemoveAll(tmpDir)
			ch <- ScanEvent{Err: fmt.Errorf("git clone: %w", err)}
			return
		}

		if pinned {
			checkoutCmd := exec.CommandContext(ctx, "git", "-c", "advice.detachedHead=false", "checkout", c.commit)
			checkoutCmd.Dir = tmpDir
			checkoutCmd.Stderr = pw
			if err := checkoutCmd.Run(); err != nil {
				os.RemoveAll(tmpDir)
				ch <- ScanEvent{Err: fmt.Errorf("git checkout %s: %w", c.commit, err)}
				return
			}
		}

		c.headCommit = gitHead(ctx, tmpDir)

		// Diff-based scan: small number of files, wrap into events.
		if useDiff {
			docs, deleted, err := c.diffScan(ctx, tmpDir, opts)
			os.RemoveAll(tmpDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: Diff-based scan failed (%v), falling back to full scan\n", c.SourceName(), err)
				// Fall through to full scan below by re-cloning.
				c.scanStreamFullScan(ctx, ch, opts)
				return
			}
			for i := range docs {
				select {
				case ch <- ScanEvent{Doc: &docs[i]}:
				case <-ctx.Done():
					return
				}
			}
			select {
			case ch <- ScanEvent{Deleted: deleted}:
			case <-ctx.Done():
			}
			return
		}

		// Full scan: if we cloned with --no-checkout for diff attempt that failed,
		// we need to re-clone. But if we got here directly, checkout is done.
		// For streaming full scan, delegate to FilesystemConnector.ScanStream.

		// Rewrite known keys from relative paths to absolute.
		fsOpts := ScanOptions{Known: opts.Known, Force: opts.Force}
		if len(opts.Known) > 0 {
			abs := make(map[string]string, len(opts.Known))
			for rel, checksum := range opts.Known {
				abs[filepath.Join(tmpDir, rel)] = checksum
			}
			fsOpts.Known = abs
		}

		fs := NewFilesystemConnector(tmpDir)
		fs.SkipGitMeta = true
		fsCh := fs.ScanStream(ctx, fsOpts)

		sourceURI := c.baseSourceURI()
		sourceName := c.SourceName()

		for ev := range fsCh {
			if ev.Err != nil {
				ch <- ev
				os.RemoveAll(tmpDir)
				return
			}
			if ev.Doc != nil {
				relPath, _ := filepath.Rel(tmpDir, ev.Doc.Path)
				ev.Doc.Path = relPath
				ev.Doc.SourceType = SourceTypeGit
				ev.Doc.SourceName = sourceName
				ev.Doc.SourceURI = sourceURI + "/" + relPath
				select {
				case ch <- ev:
				case <-ctx.Done():
					os.RemoveAll(tmpDir)
					return
				}
			}
			if ev.Deleted != nil {
				// Convert deleted paths back to relative.
				for i := range ev.Deleted {
					if rel, err := filepath.Rel(tmpDir, ev.Deleted[i]); err == nil {
						ev.Deleted[i] = rel
					}
				}
				select {
				case ch <- ev:
				case <-ctx.Done():
				}
			}
		}

		os.RemoveAll(tmpDir)
	}()

	return ch
}

// scanStreamFullScan re-clones and does a full streaming scan. Used as fallback
// when diff scan fails during ScanStream.
func (c *GitConnector) scanStreamFullScan(ctx context.Context, ch chan<- ScanEvent, opts ScanOptions) {
	cloneURL, err := c.authenticatedURL(ctx)
	if err != nil {
		ch <- ScanEvent{Err: fmt.Errorf("resolve auth: %w", err)}
		return
	}

	tmpDir, err := os.MkdirTemp("", "kb-git-*")
	if err != nil {
		ch <- ScanEvent{Err: fmt.Errorf("create temp dir: %w", err)}
		return
	}

	cloneArgs := []string{"clone", "--depth", "1"}
	if c.branch != "" {
		cloneArgs = append(cloneArgs, "--branch", c.branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	cmd.Stderr = newPrefixWriter(fmt.Sprintf("  [%s] ", c.SourceName()), os.Stderr)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		ch <- ScanEvent{Err: fmt.Errorf("git clone: %w", err)}
		return
	}

	c.headCommit = gitHead(ctx, tmpDir)

	fsOpts := ScanOptions{Known: opts.Known, Force: opts.Force}
	if len(opts.Known) > 0 {
		abs := make(map[string]string, len(opts.Known))
		for rel, checksum := range opts.Known {
			abs[filepath.Join(tmpDir, rel)] = checksum
		}
		fsOpts.Known = abs
	}

	fs := NewFilesystemConnector(tmpDir)
	fs.SkipGitMeta = true
	fsCh := fs.ScanStream(ctx, fsOpts)

	sourceURI := c.baseSourceURI()
	sourceName := c.SourceName()

	for ev := range fsCh {
		if ev.Err != nil {
			ch <- ev
			os.RemoveAll(tmpDir)
			return
		}
		if ev.Doc != nil {
			relPath, _ := filepath.Rel(tmpDir, ev.Doc.Path)
			ev.Doc.Path = relPath
			ev.Doc.SourceType = SourceTypeGit
			ev.Doc.SourceName = sourceName
			ev.Doc.SourceURI = sourceURI + "/" + relPath
			select {
			case ch <- ev:
			case <-ctx.Done():
				os.RemoveAll(tmpDir)
				return
			}
		}
		if ev.Deleted != nil {
			for i := range ev.Deleted {
				if rel, err := filepath.Rel(tmpDir, ev.Deleted[i]); err == nil {
					ev.Deleted[i] = rel
				}
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
			}
		}
	}

	os.RemoveAll(tmpDir)
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
	// Check explicit token (e.g. from UI or config).
	if c.token != "" {
		return c.token
	}

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

	// Try gh CLI for GitHub repos.
	if c.isGitHub() {
		if token := ghCLIToken(); token != "" {
			return token
		}
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

