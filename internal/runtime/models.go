package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// tagsResponse represents the Ollama /api/tags response.
type tagsResponse struct {
	Models []modelEntry `json:"models"`
}

type modelEntry struct {
	Name string `json:"name"`
}

// pullRequest is the request body for POST /api/pull.
type pullRequest struct {
	Name string `json:"name"`
}

// pullProgress represents a single line of streaming progress from /api/pull.
type pullProgress struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

// EnsureModels checks required models and pulls missing ones.
// mandatoryModels indicates which models are required (failure to pull = error).
// Models not in mandatoryModels are optional (failure to pull = warning).
func EnsureModels(ctx context.Context, baseURL string, models []string, mandatoryModels map[string]bool, verbose bool) error {
	installed, err := listInstalledModels(baseURL)
	if err != nil {
		return fmt.Errorf("list Ollama models: %w", err)
	}

	installedSet := make(map[string]bool, len(installed))
	for _, m := range installed {
		installedSet[stripTag(m)] = true
	}

	for _, model := range models {
		if installedSet[stripTag(model)] {
			continue
		}

		mandatory := mandatoryModels[model]
		if err := pullModel(ctx, baseURL, model, mandatory, verbose); err != nil {
			if mandatory {
				return err
			}
			// Optional model: print note and continue.
			fmt.Fprintf(os.Stderr, "Pulling %s... skipped (run 'ollama pull %s' to enable chunk enrichment)\n", model, model)
		}
	}

	return nil
}

// listInstalledModels fetches the list of installed models from Ollama.
func listInstalledModels(baseURL string) ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode /api/tags response: %w", err)
	}

	names := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		names[i] = m.Name
	}
	return names, nil
}

// pullModel pulls a single model from Ollama, showing progress if verbose.
func pullModel(ctx context.Context, baseURL, model string, mandatory, verbose bool) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "Pulling %s...\n", model)
	}

	body, err := json.Marshal(pullRequest{Name: model})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{} // no timeout, pulls can take a while
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pull %s: %w", model, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull %s: HTTP %d: %s", model, resp.StatusCode, string(respBody))
	}

	// Read streaming progress.
	decoder := json.NewDecoder(resp.Body)
	var lastPercent int
	for decoder.More() {
		var p pullProgress
		if err := decoder.Decode(&p); err != nil {
			break
		}
		if verbose && p.Total > 0 {
			percent := int(float64(p.Completed) / float64(p.Total) * 100)
			if percent != lastPercent {
				lastPercent = percent
				bar := renderProgressBar(percent, 20)
				sizeMB := float64(p.Total) / 1_000_000
				fmt.Fprintf(os.Stderr, "\r  Pulling %s (%.0f MB)... %s %d%%", model, sizeMB, bar, percent)
			}
		}
		if p.Status == "success" {
			if verbose {
				fmt.Fprintf(os.Stderr, "\r  Pulling %s... done.                                    \n", model)
			}
			return nil
		}
	}

	// If we got here without "success", the stream ended unexpectedly.
	// Check if the model is now available.
	installed, err := listInstalledModels(baseURL)
	if err == nil {
		for _, m := range installed {
			if stripTag(m) == stripTag(model) {
				return nil
			}
		}
	}

	return fmt.Errorf("pull %s: stream ended without success status", model)
}

// stripTag removes the ":latest" suffix (or any tag after the last colon that
// equals "latest") for comparison purposes.
func stripTag(model string) string {
	if strings.HasSuffix(model, ":latest") {
		return strings.TrimSuffix(model, ":latest")
	}
	return model
}

// renderProgressBar renders a simple text progress bar.
func renderProgressBar(percent, width int) string {
	filled := percent * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled
	return strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", empty)
}
