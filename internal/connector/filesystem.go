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

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeFilesystem is the source type identifier for local filesystem sources.
const SourceTypeFilesystem = "filesystem"

func init() {
	Register(SourceTypeFilesystem, func(config map[string]string) (Connector, error) {
		path := config["path"]
		if path == "" {
			return nil, fmt.Errorf("filesystem source missing 'path' in config")
		}
		return NewFilesystemConnector(path), nil
	})
}

// maxFileSize is the maximum file size (1 MB) that the scanner will read.
const maxFileSize = 1 << 20

// SkipDirs contains directory names that should be skipped during scanning.
var SkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
	".venv":        true,
	"dist":         true,
	"build":        true,
}

// skipFiles contains exact filenames that should be skipped during scanning
// because they don't produce useful content for knowledge retrieval.
var skipFiles = map[string]bool{
	// Lock files with non-lock extensions (.lock and .sum are in binaryExts)
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	// License/notice files
	"LICENSE":    true,
	"LICENSE.md": true,
	"LICENSE.txt": true,
	"NOTICE":     true,
	"NOTICE.md":  true,
	// VCS and editor config
	".gitignore":     true,
	".gitattributes": true,
	".editorconfig":  true,
	".DS_Store":      true,
}

// skipSuffixes contains file suffixes for generated/minified files that
// should be skipped during scanning.
var skipSuffixes = []string{
	".min.js",
	".min.css",
	".map",
	".pb.go",
	".gen.go",
}

// shouldSkipFile returns true if the file should be skipped based on its name.
func shouldSkipFile(name string) bool {
	if skipFiles[name] {
		return true
	}
	nameLower := strings.ToLower(name)
	for _, suffix := range skipSuffixes {
		if strings.HasSuffix(nameLower, suffix) {
			return true
		}
	}
	if strings.Contains(nameLower, ".generated.") || strings.HasPrefix(nameLower, "_generated") {
		return true
	}
	return false
}

// binaryExts contains file extensions that are considered binary and should be skipped.
var binaryExts = map[string]bool{
	// Executables and libraries
	".exe": true, ".bin": true, ".o": true, ".a": true,
	".so": true, ".dylib": true, ".dll": true,
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".bmp": true, ".svg": true, ".webp": true,
	// Fonts
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	// Archives
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".rar": true, ".tgz": true,
	// Documents (non-text)
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true,
	// Databases
	".db": true, ".db-shm": true, ".db-wal": true, ".db-journal": true,
	".sqlite": true, ".sqlite3": true,
	// Media
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true,
	".flac": true, ".ogg": true, ".webm": true,
	// Data files
	".parquet": true, ".arrow": true, ".avro": true, ".tfrecord": true,
	// Lock files and generated
	".lock": true, ".sum": true,
	// Other binary
	".wasm": true, ".pyc": true, ".pyo": true, ".class": true,
	".jar": true, ".war": true,
}

// gitInfo holds git metadata for a single file.
type gitInfo struct {
	lastModified time.Time
	author       string
}

// FilesystemConnector scans a local directory tree for content files.
type FilesystemConnector struct {
	rootPath    string
	SkipGitMeta bool // skip per-file git log calls (used by GitConnector)
	gitMeta     map[string]gitInfo // cached batch git metadata (repo-relative path -> info)
	gitMetaOnce bool               // true if batch metadata has been attempted
	gitRepoRoot string             // cached repo root from git rev-parse
}

// NewFilesystemConnector creates a new connector rooted at the given path.
func NewFilesystemConnector(rootPath string) *FilesystemConnector {
	return &FilesystemConnector{rootPath: rootPath}
}

// SourceName derives a human-readable source name from the root path.
// For ".", it resolves to the directory name. For absolute paths, it uses
// filepath.Base. Otherwise it returns the path as-is.
func (c *FilesystemConnector) SourceName() string {
	p := c.rootPath
	if p == "." || p == "./" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "."
		}
		return filepath.Base(abs)
	}
	return filepath.Base(p)
}

// Name returns the connector type identifier.
func (c *FilesystemConnector) Name() string {
	return SourceTypeFilesystem
}

// Config returns the connector's configuration for source registration.
// For push mode, the local path is omitted as it's meaningless on the server.
func (c *FilesystemConnector) Config(mode string) map[string]string {
	if mode == model.SourceModePush {
		return map[string]string{}
	}
	absPath, err := filepath.Abs(c.rootPath)
	if err != nil {
		absPath = c.rootPath
	}
	return map[string]string{"path": absPath}
}

// Scan walks the directory tree and returns new/changed documents and deleted paths.
// The known map holds path -> checksum for previously ingested files.
func (c *FilesystemConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	known := opts.Known
	root, err := filepath.Abs(c.rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving root path: %w", err)
	}

	// Build batch git metadata map (once) to avoid per-file subprocess calls.
	if !c.SkipGitMeta {
		c.ensureGitMeta()
	}

	// Track which known paths we encounter so we can detect deletions.
	seen := make(map[string]bool, len(known))

	var docs []model.RawDocument
	var skipped int
	var filesProcessed int
	var progressPrinted bool
	lastProgress := time.Now()

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
			if SkipDirs[name] && path != root {
				return fs.SkipDir
			}
			return nil
		}

		// Skip symlinks that point to directories.
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Stat(path)
			if err != nil || target.IsDir() {
				return nil
			}
		}

		// Skip hidden files.
		if strings.HasPrefix(name, ".") {
			return nil
		}

		// Skip non-useful files (lock files, licenses, generated/minified).
		if shouldSkipFile(name) {
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

		// Skip files that look binary (contain null bytes in the first 8KB).
		// PDF files are binary but have a dedicated extractor, so allow them.
		if ext != ".pdf" && isBinary(content) {
			return nil
		}

		// Compute checksum.
		hash := sha256.Sum256(content)
		checksum := fmt.Sprintf("%x", hash)

		// Mark as seen for deletion detection.
		seen[path] = true

		// Skip unchanged files.
		if prev, ok := known[path]; ok && prev == checksum {
			skipped++
			return nil
		}

		// Extract git metadata (skip if caller opted out, e.g. git connector).
		var lastModified time.Time
		var author string
		if !c.SkipGitMeta {
			lastModified, author = c.lookupGitMeta(path)
		}
		if lastModified.IsZero() {
			lastModified = info.ModTime()
		}

		filesProcessed++
		if now := time.Now(); now.Sub(lastProgress) >= 2*time.Second {
			fmt.Fprintf(os.Stderr, "\r  [%s] Scanning: %d files processed...", c.SourceName(), filesProcessed)
			lastProgress = now
			progressPrinted = true
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}

		docs = append(docs, model.RawDocument{
			Path:         path,
			Content:      content,
			ContentDate: lastModified,
			Author:       author,
			SourceURI:    "file://" + absPath,
			SourceType:   SourceTypeFilesystem,
			SourceName:   c.SourceName(),
			Checksum:     checksum,
		})

		return nil
	})

	if walkErr != nil {
		return nil, nil, fmt.Errorf("walking directory: %w", walkErr)
	}

	// Clear the in-place progress line if we printed one.
	if progressPrinted {
		fmt.Fprintf(os.Stderr, "\r  [%s] Scanning: %d files processed, done\n", c.SourceName(), filesProcessed)
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

// ScanStream returns a channel of ScanEvents, streaming documents as they are
// discovered during the directory walk. This allows the pipeline to begin
// processing documents before the full scan completes.
func (c *FilesystemConnector) ScanStream(ctx context.Context, opts ScanOptions) <-chan ScanEvent {
	ch := make(chan ScanEvent, 64)

	// Build batch git metadata map before starting the walk.
	if !c.SkipGitMeta {
		c.ensureGitMeta()
	}

	go func() {
		defer close(ch)

		known := opts.Known
		root, err := filepath.Abs(c.rootPath)
		if err != nil {
			ch <- ScanEvent{Err: fmt.Errorf("resolving root path: %w", err)}
			return
		}

		seen := make(map[string]bool, len(known))

		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Printf("warning: skipping %s: %v", path, err)
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			name := d.Name()

			if d.IsDir() {
				if strings.HasPrefix(name, ".") && path != root {
					return fs.SkipDir
				}
				if SkipDirs[name] && path != root {
					return fs.SkipDir
				}
				return nil
			}

			if d.Type()&fs.ModeSymlink != 0 {
				target, err := os.Stat(path)
				if err != nil || target.IsDir() {
					return nil
				}
			}

			if strings.HasPrefix(name, ".") {
				return nil
			}

			if shouldSkipFile(name) {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(name))
			if binaryExts[ext] {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				log.Printf("warning: skipping %s: cannot stat: %v", path, err)
				return nil
			}
			if info.Size() > maxFileSize {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				log.Printf("warning: skipping %s: cannot read: %v", path, err)
				return nil
			}

			if ext != ".pdf" && isBinary(content) {
				return nil
			}

			hash := sha256.Sum256(content)
			checksum := fmt.Sprintf("%x", hash)

			seen[path] = true

			if prev, ok := known[path]; ok && prev == checksum {
				return nil
			}

			var lastModified time.Time
			var author string
			if !c.SkipGitMeta {
				lastModified, author = c.lookupGitMeta(path)
			}
			if lastModified.IsZero() {
				lastModified = info.ModTime()
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}

			doc := model.RawDocument{
				Path:        path,
				Content:     content,
				ContentDate: lastModified,
				Author:      author,
				SourceURI:   "file://" + absPath,
				SourceType:  SourceTypeFilesystem,
				SourceName:  c.SourceName(),
				Checksum:    checksum,
			}

			select {
			case ch <- ScanEvent{Doc: &doc}:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})

		if walkErr != nil {
			ch <- ScanEvent{Err: fmt.Errorf("walking directory: %w", walkErr)}
			return
		}

		// Detect deleted paths.
		var deleted []string
		for knownPath := range known {
			if !seen[knownPath] {
				deleted = append(deleted, knownPath)
			}
		}

		// Send final event with deletions (may be empty).
		select {
		case ch <- ScanEvent{Deleted: deleted}:
		case <-ctx.Done():
		}
	}()

	return ch
}

// ReadDocument reads a single file and returns a RawDocument.
// Used by GitConnector for diff-based scanning of specific files.
func (c *FilesystemConnector) ReadDocument(path string) (model.RawDocument, error) {
	info, err := os.Stat(path)
	if err != nil {
		return model.RawDocument{}, err
	}
	if info.Size() > maxFileSize {
		return model.RawDocument{}, fmt.Errorf("file too large: %d bytes", info.Size())
	}

	// Skip non-useful files (lock files, licenses, generated/minified).
	if shouldSkipFile(filepath.Base(path)) {
		return model.RawDocument{}, fmt.Errorf("non-useful file: %s", filepath.Base(path))
	}

	ext := strings.ToLower(filepath.Ext(path))
	if binaryExts[ext] {
		return model.RawDocument{}, fmt.Errorf("binary file extension: %s", ext)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return model.RawDocument{}, err
	}

	if ext != ".pdf" && isBinary(content) {
		return model.RawDocument{}, fmt.Errorf("binary content detected")
	}

	hash := sha256.Sum256(content)
	var lastModified time.Time
	var author string
	if !c.SkipGitMeta {
		c.ensureGitMeta()
		lastModified, author = c.lookupGitMeta(path)
	}
	if lastModified.IsZero() {
		lastModified = info.ModTime()
	}

	return model.RawDocument{
		Path:        path,
		Content:     content,
		ContentDate: lastModified,
		Author:      author,
		Checksum:    fmt.Sprintf("%x", hash),
	}, nil
}

// isBinary returns true if the content appears to be binary by checking for
// null bytes in the first 8KB.
func isBinary(content []byte) bool {
	check := content
	if len(check) > 8192 {
		check = check[:8192]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// buildGitMetadataMap runs a single git log command to collect the most recent
// commit date and author for every tracked file in the repository. It returns
// a map keyed by repo-relative path. Only the first (most recent) entry per
// file is kept.
func buildGitMetadataMap(repoRoot string) (map[string]gitInfo, error) {
	cmd := exec.Command("git", "log", "--format=%aI|%aN", "--name-only", "--diff-filter=ACMR", "HEAD")
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := make(map[string]gitInfo)
	lines := strings.Split(string(out), "\n")

	var currentDate time.Time
	var currentAuthor string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			// Blank line separates commit blocks; reset header for next block.
			currentDate = time.Time{}
			currentAuthor = ""
			continue
		}

		// Try to parse as a header line (date|author).
		if parts := strings.SplitN(line, "|", 2); len(parts) == 2 {
			if t, err := time.Parse(time.RFC3339, parts[0]); err == nil {
				currentDate = t
				currentAuthor = parts[1]
				continue
			}
		}

		// Otherwise it's a filename. Only keep the first occurrence (most recent).
		if !currentDate.IsZero() {
			if _, exists := result[line]; !exists {
				result[line] = gitInfo{lastModified: currentDate, author: currentAuthor}
			}
		}
	}

	return result, nil
}

// gitRepoRoot returns the root directory of the git repository containing dir,
// or an error if dir is not inside a git repo.
func gitRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ensureGitMeta lazily builds the batch git metadata map once per connector.
func (c *FilesystemConnector) ensureGitMeta() {
	if c.gitMetaOnce {
		return
	}
	c.gitMetaOnce = true

	root, err := filepath.Abs(c.rootPath)
	if err != nil {
		return
	}

	repoRoot, err := gitRepoRoot(root)
	if err != nil {
		return // not a git repo
	}
	c.gitRepoRoot = repoRoot

	m, err := buildGitMetadataMap(repoRoot)
	if err != nil {
		return // batch failed, will fall back to mtime
	}
	c.gitMeta = m
}

// lookupGitMeta returns git metadata for the given absolute path using the
// batch map. Falls back to the per-file gitMetadata() if the batch map is nil
// (e.g. not a git repo or batch command failed).
func (c *FilesystemConnector) lookupGitMeta(absPath string) (time.Time, string) {
	if c.gitMeta != nil && c.gitRepoRoot != "" {
		rel, err := filepath.Rel(c.gitRepoRoot, absPath)
		if err == nil {
			if info, ok := c.gitMeta[rel]; ok {
				return info.lastModified, info.author
			}
		}
		// File not in map means untracked; return zero (caller falls back to mtime).
		return time.Time{}, ""
	}
	// Batch not available, fall back to per-file git log.
	return gitMetadata(absPath)
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
