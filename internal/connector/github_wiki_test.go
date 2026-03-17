package connector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

)

// Compile-time interface compliance check.
var _ Connector = (*GitHubWikiConnector)(nil)

func TestGitHubWikiName(t *testing.T) {
	c := NewGitHubWikiConnector("https://github.com/owner/repo", "", "")
	if got := c.Name(); got != SourceTypeGitHubWiki {
		t.Errorf("Name() = %q, want %q", got, SourceTypeGitHubWiki)
	}
}

func TestGitHubWikiSourceName(t *testing.T) {
	tests := []struct {
		repoURL string
		want    string
	}{
		{"https://github.com/owner/repo", "owner/repo/wiki"},
		{"https://github.com/owner/repo.git", "owner/repo/wiki"},
		{"https://github.com/my-org/my-repo", "my-org/my-repo/wiki"},
	}
	for _, tt := range tests {
		c := NewGitHubWikiConnector(tt.repoURL, "", "")
		if got := c.SourceName(); got != tt.want {
			t.Errorf("SourceName(%q) = %q, want %q", tt.repoURL, got, tt.want)
		}
	}
}

func TestGitHubWikiCloneURL(t *testing.T) {
	tests := []struct {
		repoURL string
		want    string
	}{
		{"https://github.com/owner/repo", "https://github.com/owner/repo.wiki.git"},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo.wiki.git"},
	}
	for _, tt := range tests {
		c := NewGitHubWikiConnector(tt.repoURL, "", "")
		if got := c.WikiCloneURL(); got != tt.want {
			t.Errorf("WikiCloneURL(%q) = %q, want %q", tt.repoURL, got, tt.want)
		}
	}
}

func TestGitHubWikiConfig(t *testing.T) {
	c := NewGitHubWikiConnector("https://github.com/owner/repo", "main", "")
	cfg := c.Config("local")
	if cfg["url"] != "https://github.com/owner/repo" {
		t.Errorf("Config url = %q, want %q", cfg["url"], "https://github.com/owner/repo")
	}
	if cfg["branch"] != "main" {
		t.Errorf("Config branch = %q, want %q", cfg["branch"], "main")
	}

	// Without branch.
	c2 := NewGitHubWikiConnector("https://github.com/owner/repo", "", "")
	cfg2 := c2.Config("local")
	if _, ok := cfg2["branch"]; ok {
		t.Error("Config should not include branch when empty")
	}
}

func TestWikiPageName(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"Home.md", "Home"},
		{"Getting-Started.md", "Getting-Started"},
		{"sub/Page.md", "sub/Page"},
		{"README.txt", "README"},
		{"no-ext", "no-ext"},
	}
	for _, tt := range tests {
		if got := wikiPageName(tt.relPath); got != tt.want {
			t.Errorf("wikiPageName(%q) = %q, want %q", tt.relPath, got, tt.want)
		}
	}
}

// setupBareWikiRepo creates a bare git repo with some wiki-like markdown files,
// returning the file:// URL to the bare repo. This simulates a GitHub wiki repo.
func setupBareWikiRepo(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	bareDir := filepath.Join(base, "repo.wiki.git")
	workDir := filepath.Join(base, "work")

	// Create bare repo with explicit default branch to avoid failures
	// when global init.defaultBranch is not set.
	run(t, "git", "init", "--bare", "--initial-branch=main", bareDir)

	// Clone it, add files, push.
	run(t, "git", "clone", bareDir, workDir)

	// Configure git user for commits.
	runIn(t, workDir, "git", "config", "user.email", "test@test.com")
	runIn(t, workDir, "git", "config", "user.name", "Test")

	// Create wiki pages.
	writeFile(t, workDir, "Home.md", "# Welcome\nThis is the home page.")
	writeFile(t, workDir, "Getting-Started.md", "# Getting Started\nFollow these steps.")
	writeFile(t, workDir, "API-Reference.md", "# API Reference\nEndpoints listed here.")

	runIn(t, workDir, "git", "add", "-A")
	runIn(t, workDir, "git", "commit", "-m", "Initial wiki pages")
	runIn(t, workDir, "git", "push", "origin", "HEAD:main")

	return "file://" + bareDir
}

func TestGitHubWikiScan(t *testing.T) {
	bareURL := setupBareWikiRepo(t)

	// The connector expects a repo URL (without .wiki.git), but our bare repo
	// path already ends with .wiki.git. We need to strip that so WikiCloneURL()
	// re-adds it. Since bareURL is like file:///path/to/repo.wiki.git, we strip
	// the .wiki.git suffix to get the "repo URL", then WikiCloneURL will append
	// .wiki.git back.
	repoURL := strings.TrimSuffix(bareURL, ".wiki.git")

	c := NewGitHubWikiConnector(repoURL, "", "")

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(deleted) != 0 {
		t.Errorf("expected no deleted paths, got %d", len(deleted))
	}

	if len(docs) != 3 {
		names := make([]string, len(docs))
		for i, d := range docs {
			names[i] = d.Path
		}
		t.Fatalf("expected 3 documents, got %d: %v", len(docs), names)
	}

	// Sort for deterministic checks.
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })

	// Verify API-Reference.md
	apiDoc := docs[0]
	if apiDoc.Path != "API-Reference.md" {
		t.Errorf("expected path 'API-Reference.md', got %q", apiDoc.Path)
	}
	if apiDoc.SourceType != SourceTypeGitHubWiki {
		t.Errorf("expected SourceType %q, got %q", SourceTypeGitHubWiki, apiDoc.SourceType)
	}
	expectedURI := repoURL + "/wiki/API-Reference"
	if apiDoc.SourceURI != expectedURI {
		t.Errorf("expected SourceURI %q, got %q", expectedURI, apiDoc.SourceURI)
	}
	if apiDoc.SourceName != c.SourceName() {
		t.Errorf("expected SourceName %q, got %q", c.SourceName(), apiDoc.SourceName)
	}
	if apiDoc.Checksum == "" {
		t.Error("expected non-empty checksum")
	}

	// Verify Getting-Started.md
	gsDoc := docs[1]
	if gsDoc.Path != "Getting-Started.md" {
		t.Errorf("expected path 'Getting-Started.md', got %q", gsDoc.Path)
	}
	expectedURI = repoURL + "/wiki/Getting-Started"
	if gsDoc.SourceURI != expectedURI {
		t.Errorf("expected SourceURI %q, got %q", expectedURI, gsDoc.SourceURI)
	}

	// Verify Home.md
	homeDoc := docs[2]
	if homeDoc.Path != "Home.md" {
		t.Errorf("expected path 'Home.md', got %q", homeDoc.Path)
	}
	expectedURI = repoURL + "/wiki/Home"
	if homeDoc.SourceURI != expectedURI {
		t.Errorf("expected SourceURI %q, got %q", expectedURI, homeDoc.SourceURI)
	}

	// Verify checksum correctness.
	expectedChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte("# Welcome\nThis is the home page.")))
	if homeDoc.Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: got %q, want %q", homeDoc.Checksum, expectedChecksum)
	}
}

func TestGitHubWikiScanIncremental(t *testing.T) {
	bareURL := setupBareWikiRepo(t)
	repoURL := strings.TrimSuffix(bareURL, ".wiki.git")

	c := NewGitHubWikiConnector(repoURL, "", "")

	// First scan.
	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(docs))
	}

	// Build known map.
	known := make(map[string]string)
	for _, d := range docs {
		known[d.Path] = d.Checksum
	}

	// Second scan with known checksums — nothing changed.
	docs2, deleted, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}
	if len(docs2) != 0 {
		t.Errorf("expected 0 changed documents, got %d", len(docs2))
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted paths, got %d", len(deleted))
	}
}

func TestGitHubWikiScanDeletion(t *testing.T) {
	base := t.TempDir()
	bareDir := filepath.Join(base, "repo.wiki.git")
	workDir := filepath.Join(base, "work")

	// Create bare repo with two files.
	run(t, "git", "init", "--bare", "--initial-branch=main", bareDir)
	run(t, "git", "clone", bareDir, workDir)
	runIn(t, workDir, "git", "config", "user.email", "test@test.com")
	runIn(t, workDir, "git", "config", "user.name", "Test")

	writeFile(t, workDir, "Home.md", "# Home")
	writeFile(t, workDir, "Extra.md", "# Extra")
	runIn(t, workDir, "git", "add", "-A")
	runIn(t, workDir, "git", "commit", "-m", "two pages")
	runIn(t, workDir, "git", "push", "origin", "HEAD:main")

	repoURL := strings.TrimSuffix("file://"+bareDir, ".wiki.git")
	c := NewGitHubWikiConnector(repoURL, "", "")

	// First scan — get known state.
	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}

	known := make(map[string]string)
	for _, d := range docs {
		known[d.Path] = d.Checksum
	}

	// Remove Extra.md from the repo.
	os.Remove(filepath.Join(workDir, "Extra.md"))
	runIn(t, workDir, "git", "add", "-A")
	runIn(t, workDir, "git", "commit", "-m", "remove Extra")
	runIn(t, workDir, "git", "push", "origin", "HEAD:main")

	// Second scan — should detect deletion.
	_, deleted, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}

	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted path, got %d: %v", len(deleted), deleted)
	}
	if deleted[0] != "Extra.md" {
		t.Errorf("expected deleted path 'Extra.md', got %q", deleted[0])
	}
}

func TestGitHubWikiScanNoWiki(t *testing.T) {
	// Point at a non-existent wiki — clone should fail with a clear error.
	c := NewGitHubWikiConnector("file:///nonexistent/repo", "", "")
	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for non-existent wiki, got nil")
	}
	if !strings.Contains(err.Error(), "git clone wiki") {
		t.Errorf("expected error to mention 'git clone wiki', got: %v", err)
	}
}

// run executes a command and fails the test on error.
func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v", name, args, err)
	}
}

// runIn executes a command in a specific directory.
func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v (in %s) failed: %v", name, args, dir, err)
	}
}
