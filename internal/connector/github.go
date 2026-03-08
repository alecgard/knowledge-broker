package connector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

// GitHubConnector pulls content from a GitHub repository.
type GitHubConnector struct {
	owner  string
	repo   string
	branch string
	token  string
}

// NewGitHubConnector creates a connector for the given GitHub repo.
// If branch is empty, defaults to the repo's default branch.
// If token is empty, uses unauthenticated access (60 req/hr limit).
func NewGitHubConnector(owner, repo, branch, token string) *GitHubConnector {
	return &GitHubConnector{
		owner:  owner,
		repo:   repo,
		branch: branch,
		token:  token,
	}
}

// Name returns the connector type identifier.
func (c *GitHubConnector) Name() string {
	return "github"
}

// Scan returns all content files from the GitHub repo.
func (c *GitHubConnector) Scan(ctx context.Context, known map[string]string) ([]model.RawDocument, []string, error) {
	client := github.NewClient(nil)
	if c.token != "" {
		client = client.WithAuthToken(c.token)
	}

	// Get the repo tree recursively.
	ref := c.branch
	if ref == "" {
		repo, _, err := client.Repositories.Get(ctx, c.owner, c.repo)
		if err != nil {
			return nil, nil, fmt.Errorf("get repo: %w", err)
		}
		ref = repo.GetDefaultBranch()
	}

	tree, _, err := client.Git.GetTree(ctx, c.owner, c.repo, ref, true)
	if err != nil {
		return nil, nil, fmt.Errorf("get tree: %w", err)
	}

	seen := make(map[string]bool)
	var docs []model.RawDocument

	for _, entry := range tree.Entries {
		if entry.GetType() != "blob" {
			continue
		}

		path := entry.GetPath()
		ext := strings.ToLower(filepath.Ext(path))
		name := filepath.Base(path)

		// Skip hidden files and binary extensions.
		if strings.HasPrefix(name, ".") {
			continue
		}
		if binaryExts[ext] {
			continue
		}

		// Skip known non-content directories.
		for dir := range skipDirs {
			if strings.HasPrefix(path, dir+"/") || strings.Contains(path, "/"+dir+"/") {
				continue
			}
		}

		// Skip large files (GitHub tree entries include size).
		if entry.GetSize() > int(maxFileSize) {
			continue
		}

		// Use the SHA as checksum.
		checksum := entry.GetSHA()
		seen[path] = true

		// Skip unchanged files.
		if prev, ok := known[path]; ok && prev == checksum {
			continue
		}

		// Fetch file content.
		content, _, _, err := client.Repositories.GetContents(ctx, c.owner, c.repo, path, &github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			log.Printf("warning: skipping %s: %v", path, err)
			continue
		}

		decoded, err := content.GetContent()
		if err != nil {
			log.Printf("warning: skipping %s: cannot decode: %v", path, err)
			continue
		}

		// Get last commit for this file.
		lastModified := time.Now()
		author := ""
		commits, _, err := client.Repositories.ListCommits(ctx, c.owner, c.repo, &github.CommitsListOptions{
			Path:        path,
			SHA:         ref,
			ListOptions: github.ListOptions{PerPage: 1},
		})
		if err == nil && len(commits) > 0 {
			if ca := commits[0].GetCommit().GetAuthor(); ca != nil {
				if t := ca.GetDate(); !t.IsZero() {
					lastModified = t.Time
				}
				author = ca.GetName()
			}
		}

		fileType := strings.TrimPrefix(ext, ".")
		if fileType == "" {
			fileType = "unknown"
		}

		sourceURI := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", c.owner, c.repo, ref, path)

		// Compute content checksum for fragment ID stability.
		hash := sha256.Sum256([]byte(decoded))
		contentChecksum := fmt.Sprintf("%x", hash)
		_ = contentChecksum // Use GitHub SHA as checksum since it's already unique

		docs = append(docs, model.RawDocument{
			Path:         path,
			Content:      []byte(decoded),
			LastModified: lastModified,
			Author:       author,
			SourceURI:    sourceURI,
			SourceType:   "github",
			FileType:     fileType,
			Checksum:     checksum,
		})
	}

	// Detect deleted paths.
	var deleted []string
	for knownPath := range known {
		if !seen[knownPath] {
			deleted = append(deleted, knownPath)
		}
	}

	return docs, deleted, nil
}
