// Package debug provides an HTTP round-tripper that logs all requests and responses.
package debug

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Transport wraps an http.RoundTripper and logs requests and responses.
type Transport struct {
	Base   http.RoundTripper
	Logger *slog.Logger
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	// Log request.
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
	}

	// Capture request body size if present.
	if req.Body != nil && req.ContentLength > 0 {
		attrs = append(attrs, slog.String("req_size", formatBytes(req.ContentLength)))
	}

	start := time.Now()
	resp, err := base.RoundTrip(req)
	duration := time.Since(start)

	if err != nil {
		t.Logger.LogAttrs(req.Context(), slog.LevelDebug, "HTTP request failed",
			append(attrs, slog.String("error", err.Error()), slog.Duration("duration", duration))...,
		)
		return nil, err
	}

	attrs = append(attrs,
		slog.Int("status", resp.StatusCode),
		slog.Duration("duration", duration),
	)

	// Capture response body size.
	if resp.ContentLength >= 0 {
		attrs = append(attrs, slog.String("resp_size", formatBytes(resp.ContentLength)))
	}

	t.Logger.LogAttrs(req.Context(), slog.LevelDebug, "HTTP request", attrs...)

	return resp, nil
}

// NewLoggingClient returns an *http.Client that logs all requests when debug is true.
// When debug is false, returns a plain client.
func NewLoggingClient(logger *slog.Logger, debug bool) *http.Client {
	if !debug {
		return &http.Client{}
	}
	return &http.Client{
		Transport: &Transport{
			Base:   http.DefaultTransport,
			Logger: logger,
		},
	}
}

// ReadAndRestore reads a response body fully, logs it, and replaces the body
// with a new reader so the caller can still read it. Only use in debug mode.
func ReadAndRestore(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
