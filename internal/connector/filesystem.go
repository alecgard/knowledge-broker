package connector

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

// maxFileSize is the maximum file size (1 MB) that the scanner will read.
const maxFileSize = 1 << 20

// skipDirs contains directory names that should be skipped during scanning.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
	".venv":        true,
	"dist":         true,
	"build":        true,
}

// binaryExts contains file extensions that are considered binary and should be skipped.
var binaryExts = map[string]bool{
	".exe":   true,
	".bin":   true,
	".png":   true,
	".jpg":   true,
	".gif":   true,
	".ico":   true,
	".woff":  true,
	".ttf":   true,
	".zip":   true,
	".tar":   true,
	".gz":    true,
	".pdf":   true,
	".o":     true,
	".a":     true,
	".so":    true,
	".dylib": true,
}

// FilesystemConnector scans a local directory tree for content files.
type FilesystemConnector struct {
	rootPath string
}

// NewFilesystemConnector creates a new connector rooted at the given path.
func NewFilesystemConnector(rootPath string) *FilesystemConnector {
	return &FilesystemConnector{rootPath: rootPath}
}

// Name returns the connector type identifier.
func (c *FilesystemConnector) Name() string {
	return "filesystem"
}

// Scan walks the directory tree and returns new/changed documents and deleted paths.
// The known map holds path -> checksum for previously ingested files.
func (c *FilesystemConnector) Scan(ctx context.Context, known map[string]string) ([]model.RawDocument, []string, error) {
	root, err := filepath.Abs(c.rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving root path: %w", err)
	}

	// Track which known paths we encounter so we can detect deletions.
	seen := make(map[string]bool, len(known))

	var docs []model.RawDocument

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("warning: skipping %s: %v", path, err)
			return nil
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		name := d.Name()

		// Skip hidden directories and known non-content directories.
		if d.IsDir() {
			if strings.HasPrefix(name, ".") && path != root {
				return fs.SkipDir
			}
			if skipDirs[name] && path != root {
				return fs.SkipDir
			}
			return nil
		}

		// Skip hidden files.
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Skip binary files by extension.
		ext := strings.ToLower(filepath.Ext(name))
		if binaryExts[ext] {
			return nil
		}

		// Check file info for size.
		info, err := d.Info()
		if err != nil {
			log.Printf("warning: skipping %s: cannot stat: %v", path, err)
			return nil
		}
		if info.Size() > maxFileSize {
			return nil
		}

		// Read file content.
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("warning: skipping %s: cannot read: %v", path, err)
			return nil
		}

		// Compute checksum.
		hash := sha256.Sum256(content)
		checksum := fmt.Sprintf("%x", hash)

		// Mark as seen for deletion detection.
		seen[path] = true

		// Skip unchanged files.
		if prev, ok := known[path]; ok && prev == checksum {
			return nil
		}

		// Extract git metadata.
		lastModified, author := gitMetadata(path)
		if lastModified.IsZero() {
			lastModified = info.ModTime()
		}

		// Determine file type from extension.
		fileType := strings.TrimPrefix(ext, ".")
		if fileType == "" {
			fileType = "unknown"
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}

		docs = append(docs, model.RawDocument{
			Path:         path,
			Content:      content,
			LastModified: lastModified,
			Author:       author,
			SourceURI:    "file://" + absPath,
			SourceType:   "filesystem",
			FileType:     fileType,
			Checksum:     checksum,
		})

		return nil
	})

	if walkErr != nil {
		return nil, nil, fmt.Errorf("walking directory: %w", walkErr)
	}

	// Detect deleted paths: paths in known that were not seen during the walk.
	var deleted []string
	for knownPath := range known {
		if !seen[knownPath] {
			deleted = append(deleted, knownPath)
		}
	}

	return docs, deleted, nil
}

// gitMetadata attempts to extract the last commit time and author for a file
// using git log. Returns zero time and empty string if git is unavailable or
// the file is not tracked.
func gitMetadata(path string) (time.Time, string) {
	dir := filepath.Dir(path)
	cmd := exec.Command("git", "log", "-1", "--format=%aI|%aN", "--", path)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, ""
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return time.Time{}, ""
	}

	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, ""
	}

	t, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return time.Time{}, ""
	}

	return t, parts[1]
}
