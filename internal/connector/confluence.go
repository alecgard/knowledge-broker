package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeConfluence is the source type identifier for Confluence sources.
const SourceTypeConfluence = "confluence"

func init() {
	Register(SourceTypeConfluence, func(config map[string]string) (Connector, error) {
		return NewConfluenceConnector(config["base_url"], config["space_key"], config["username"], config["api_token"]), nil
	})
}

// HTTPClient is an interface for making HTTP requests, allowing test mocking.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ConfluenceConnector scans a Confluence Cloud space for pages.
type ConfluenceConnector struct {
	baseURL  string
	spaceKey string
	username string
	apiToken string
	client   HTTPClient
}

// NewConfluenceConnector creates a connector for the given Confluence space.
func NewConfluenceConnector(baseURL, spaceKey, username, apiToken string) *ConfluenceConnector {
	return &ConfluenceConnector{
		baseURL:  strings.TrimRight(baseURL, "/"),
		spaceKey: spaceKey,
		username: username,
		apiToken: apiToken,
		client:   http.DefaultClient,
	}
}

// Name returns the connector type identifier.
func (c *ConfluenceConnector) Name() string {
	return SourceTypeConfluence
}

// SourceName returns a human-readable name for this source.
func (c *ConfluenceConnector) SourceName() string {
	return c.spaceKey
}

// Config returns the connector's configuration for source registration.
func (c *ConfluenceConnector) Config(mode string) map[string]string {
	cfg := map[string]string{
		"base_url":  c.baseURL,
		"space_key": c.spaceKey,
		"username":  c.username,
	}
	if mode == model.SourceModeLocal {
		cfg["api_token"] = c.apiToken
	}
	return cfg
}

// confluenceResponse is the JSON response from the Confluence REST API content endpoint.
type confluenceResponse struct {
	Results []confluencePage   `json:"results"`
	Links   confluenceLinks    `json:"_links"`
}

type confluencePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Version struct {
		When string `json:"when"`
		By   struct {
			DisplayName string `json:"displayName"`
		} `json:"by"`
	} `json:"version"`
}

type confluenceLinks struct {
	Next string `json:"next"`
}

// Scan fetches all pages from the Confluence space and returns new/changed
// documents and deleted paths. The known map holds path -> checksum for
// previously ingested pages.
func (c *ConfluenceConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	known := opts.Known
	seen := make(map[string]bool, len(known))

	var docs []model.RawDocument

	// Start with the first page of results.
	endpoint := fmt.Sprintf("%s/wiki/rest/api/content?spaceKey=%s&expand=body.storage,version&limit=25&start=0",
		c.baseURL, url.QueryEscape(c.spaceKey))

	for endpoint != "" {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		pages, nextURL, err := c.fetchPage(ctx, endpoint)
		if err != nil {
			return nil, nil, err
		}

		for _, page := range pages {
			path := page.ID

			content := page.Body.Storage.Value
			hash := sha256.Sum256([]byte(content))
			checksum := fmt.Sprintf("%x", hash)

			seen[path] = true

			// Skip unchanged pages.
			if prev, ok := known[path]; ok && prev == checksum {
				continue
			}

			var lastModified time.Time
			if page.Version.When != "" {
				lastModified = parseConfluenceTimestamp(page.Version.When)
			}

			sourceURI := fmt.Sprintf("%s/wiki/spaces/%s/pages/%s",
				c.baseURL, c.spaceKey, page.ID)

			docs = append(docs, model.RawDocument{
				Path:         path,
				Content:      []byte(content),
				ContentDate: lastModified,
				Author:       page.Version.By.DisplayName,
				SourceURI:    sourceURI,
				SourceType:   SourceTypeConfluence,
				SourceName:   c.SourceName(),
				Checksum:     checksum,
			})
		}

		// Resolve next page URL.
		if nextURL != "" {
			// The next link may be relative or absolute.
			if strings.HasPrefix(nextURL, "http") {
				endpoint = nextURL
			} else {
				endpoint = c.baseURL + nextURL
			}
		} else {
			endpoint = ""
		}
	}

	// Detect deleted pages: paths in known that were not seen in the API response.
	var deleted []string
	for knownPath := range known {
		if !seen[knownPath] {
			deleted = append(deleted, knownPath)
		}
	}

	return docs, deleted, nil
}

// fetchPage fetches a single page of results from the Confluence API.
func (c *ConfluenceConnector) fetchPage(ctx context.Context, url string) ([]confluencePage, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	req.SetBasicAuth(c.username, c.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("confluence API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("confluence API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result confluenceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}

	return result.Results, result.Links.Next, nil
}

// confluenceTimestampFormats lists timestamp formats used by Confluence, tried in order.
var confluenceTimestampFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05.000+0000",
	"2006-01-02T15:04:05.000-0700",
}

// parseConfluenceTimestamp tries multiple known Confluence timestamp formats
// and returns the parsed time, or zero time if none match.
func parseConfluenceTimestamp(s string) time.Time {
	for _, layout := range confluenceTimestampFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
