---
description: Build custom connectors to ingest content from any source into Knowledge Broker. Covers the Connector interface, registration, incremental ingestion, and testing.
---

# Custom Connectors

KB's connector system is pluggable. You can add support for any data source by implementing the `Connector` interface and registering a factory function. This guide walks through the interface, registration, and testing patterns.

## The Connector interface

The public interface lives in `pkg/connector/connector.go`. External connector packages import it directly:

```go
import "github.com/knowledge-broker/knowledge-broker/pkg/connector"
```

The interface has four methods:

```go
type Connector interface {
    Name() string
    SourceName() string
    Config(mode string) map[string]string
    Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error)
}
```

### `Name() string`

Returns the connector type identifier. This is a fixed string that uniquely identifies the kind of source, e.g. `"filesystem"`, `"git"`, `"confluence"`, `"slack"`. It is stored alongside every ingested fragment and used to look up the factory when reconstructing a connector from a saved source.

### `SourceName() string`

Returns a human-readable name for this specific source instance. Examples: `"owner/repo"` for a Git connector, `"ENGINEERING"` for a Confluence space, or the directory name for a filesystem connector. This value appears in query results and the source list.

### `Config(mode string) map[string]string`

Returns a map of configuration key-value pairs that fully describe how to reconstruct this connector instance. KB persists this map in the database so it can re-create the connector for future ingestion runs (e.g. `kb ingest --all`).

The `mode` parameter is one of two constants from `pkg/model`:

- `model.SourceModeLocal` (`"local"`) -- the source is being ingested on the same machine where KB runs. Return all config fields including local paths.
- `model.SourceModePush` (`"push"`) -- the source is being pushed to a remote KB server. Omit local-only details (file paths, local credentials) that would be meaningless or sensitive on the server side.

Example from the filesystem connector:

```go
func (c *FilesystemConnector) Config(mode string) map[string]string {
    if mode == model.SourceModePush {
        return map[string]string{}
    }
    absPath, _ := filepath.Abs(c.rootPath)
    return map[string]string{"path": absPath}
}
```

### `Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error)`

The core method. Scans the source and returns three values:

1. **`[]model.RawDocument`** -- new or changed documents to ingest.
2. **`[]string`** -- paths of documents that have been deleted since the last scan.
3. **`error`** -- any fatal error that should abort the scan.

The connector should respect `ctx` for cancellation (check `ctx.Done()` periodically in long scans).

## ScanOptions and incremental ingestion

```go
type ScanOptions struct {
    Known      map[string]string  // path -> SHA-256 checksum of previously ingested files
    Force      bool               // skip incremental optimizations, process everything
    LastIngest *time.Time         // timestamp of last successful ingestion (nil on first scan)
}
```

KB passes `Known` on every scan after the first. Your connector should:

1. Compute a SHA-256 checksum for each document's content.
2. Compare against `Known[path]`. If the checksum matches, skip the document.
3. Track which known paths were seen. Any path in `Known` that was not seen during the scan should be returned in the deleted list.

If `Force` is true, skip the checksum comparison and return all documents.

`LastIngest` is useful for connectors that talk to APIs with time-based filtering (e.g. Slack's `oldest` parameter). Use it to narrow the fetch window instead of always scanning the full history.

## RawDocument

`RawDocument` is defined in `pkg/model/model.go`. It represents a single file or page before KB's extractors chunk it:

```go
type RawDocument struct {
    Path        string     // unique path within the source (used as the document key)
    Content     []byte     // raw file content
    ContentDate time.Time  // when the content was last modified
    Author      string     // who last modified the content (optional)
    SourceURI   string     // a URI that links back to the original (e.g. "file:///abs/path", a web URL)
    SourceType  string     // must match your connector's Name()
    SourceName  string     // must match your connector's SourceName()
    Checksum    string     // SHA-256 hex digest of Content
    Chunks      []Chunk    // optional pre-chunked content; if set, KB skips its own extractors
}
```

Key points:

- **`Path`** is the primary key for a document within a source. It must be stable across scans so KB can detect updates and deletions.
- **`Checksum`** must be the hex-encoded SHA-256 of `Content`. KB uses this for incremental ingestion.
- **`SourceType`** and **`SourceName`** must match what `Name()` and `SourceName()` return. KB uses these to associate fragments with their source.
- **`Chunks`** is optional. If your connector produces pre-chunked content (e.g. Slack threads), set this field and KB will skip its built-in extractors. Each `Chunk` has a `Content` string and a `Metadata` map.
- **`SourceURI`** should be a clickable link back to the original content when possible. This appears in query results.

## Registering a factory

The internal registry in `internal/connector/connector.go` maps source type names to factory functions:

```go
type Factory func(config map[string]string) (Connector, error)
```

Register your connector in an `init()` function so it is available as soon as the package is imported:

```go
package connector

func init() {
    Register("myservice", func(config map[string]string) (Connector, error) {
        apiURL := config["api_url"]
        if apiURL == "" {
            return nil, fmt.Errorf("myservice source missing 'api_url' in config")
        }
        return NewMyServiceConnector(apiURL), nil
    })
}
```

The factory receives the same `map[string]string` that `Config()` returned when the source was first registered. Validate that all required keys are present and return a clear error if not.

## Minimal worked example

Below is a complete, minimal connector that ingests entries from a hypothetical REST API:

```go
package connector

import (
    "context"
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/knowledge-broker/knowledge-broker/pkg/model"
)

const SourceTypeMyService = "myservice"

func init() {
    Register(SourceTypeMyService, func(config map[string]string) (Connector, error) {
        apiURL := config["api_url"]
        if apiURL == "" {
            return nil, fmt.Errorf("myservice source missing 'api_url' in config")
        }
        return &MyServiceConnector{apiURL: apiURL}, nil
    })
}

type MyServiceConnector struct {
    apiURL string
}

func (c *MyServiceConnector) Name() string       { return SourceTypeMyService }
func (c *MyServiceConnector) SourceName() string  { return c.apiURL }

func (c *MyServiceConnector) Config(mode string) map[string]string {
    return map[string]string{"api_url": c.apiURL}
}

func (c *MyServiceConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
    // Fetch entries from the API.
    req, err := http.NewRequestWithContext(ctx, "GET", c.apiURL+"/entries", nil)
    if err != nil {
        return nil, nil, err
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, nil, fmt.Errorf("fetching entries: %w", err)
    }
    defer resp.Body.Close()

    var entries []struct {
        ID       string    `json:"id"`
        Body     string    `json:"body"`
        Author   string    `json:"author"`
        Updated  time.Time `json:"updated_at"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
        return nil, nil, fmt.Errorf("decoding response: %w", err)
    }

    seen := make(map[string]bool, len(entries))
    var docs []model.RawDocument

    for _, e := range entries {
        path := e.ID
        content := []byte(e.Body)
        hash := sha256.Sum256(content)
        checksum := fmt.Sprintf("%x", hash)

        seen[path] = true

        // Skip unchanged documents.
        if prev, ok := opts.Known[path]; ok && prev == checksum && !opts.Force {
            continue
        }

        docs = append(docs, model.RawDocument{
            Path:        path,
            Content:     content,
            ContentDate: e.Updated,
            Author:      e.Author,
            SourceURI:   fmt.Sprintf("%s/entries/%s", c.apiURL, e.ID),
            SourceType:  SourceTypeMyService,
            SourceName:  c.SourceName(),
            Checksum:    checksum,
        })
    }

    // Detect deletions.
    var deleted []string
    for knownPath := range opts.Known {
        if !seen[knownPath] {
            deleted = append(deleted, knownPath)
        }
    }

    return docs, deleted, nil
}
```

## Testing a connector

Follow the pattern used by the built-in connectors in `internal/connector/`. The key tests to write:

### Compile-time interface check

Verify your struct satisfies the `Connector` interface at compile time:

```go
var _ connector.Connector = (*MyServiceConnector)(nil)
```

### Scan returns correct documents

Create test data (or use an `httptest.Server` for API-based connectors), run `Scan`, and verify the returned `RawDocument` fields:

```go
func TestScanReturnsDocuments(t *testing.T) {
    // Set up a test HTTP server that returns known entries.
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode([]map[string]any{
            {"id": "doc-1", "body": "Hello world", "author": "alice", "updated_at": time.Now()},
        })
    }))
    defer srv.Close()

    c := &MyServiceConnector{apiURL: srv.URL}
    docs, deleted, err := c.Scan(context.Background(), connector.ScanOptions{})
    if err != nil {
        t.Fatalf("Scan failed: %v", err)
    }

    if len(deleted) != 0 {
        t.Errorf("expected no deletions, got %d", len(deleted))
    }
    if len(docs) != 1 {
        t.Fatalf("expected 1 document, got %d", len(docs))
    }
    if docs[0].SourceType != "myservice" {
        t.Errorf("unexpected source type: %s", docs[0].SourceType)
    }
}
```

### Incremental ingestion skips unchanged documents

Scan once, build the `Known` map from the results, then scan again without changing the data. The second scan should return zero documents:

```go
func TestScanSkipsUnchanged(t *testing.T) {
    // ... set up server ...

    c := &MyServiceConnector{apiURL: srv.URL}

    docs, _, _ := c.Scan(context.Background(), connector.ScanOptions{})
    known := map[string]string{}
    for _, d := range docs {
        known[d.Path] = d.Checksum
    }

    docs2, _, _ := c.Scan(context.Background(), connector.ScanOptions{Known: known})
    if len(docs2) != 0 {
        t.Errorf("expected 0 changed documents, got %d", len(docs2))
    }
}
```

### Deletion detection

Include a path in `Known` that the scan no longer returns. Verify it appears in the deleted list.

### Name and Config round-trip

Verify that `Config()` returns the keys your factory expects, and that creating a connector from those keys produces a working instance:

```go
func TestConfigRoundTrip(t *testing.T) {
    c := &MyServiceConnector{apiURL: "https://example.com"}
    config := c.Config(model.SourceModeLocal)

    c2, err := factoryFunc(config)
    if err != nil {
        t.Fatalf("factory failed: %v", err)
    }
    if c2.Name() != c.Name() {
        t.Error("name mismatch after round-trip")
    }
}
```

## Wiring up a CLI flag

If you are adding the connector to the KB codebase itself (rather than an external plugin), you will also need to:

1. Add a CLI flag in `cmd/kb/ingest.go` (e.g. `--myservice`).
2. Create the connector instance from the flag value and add it to the ingest sources slice.
3. Import the connector package in a file that gets compiled (often via a blank import `_ "path/to/package"` if using `init()` registration).
