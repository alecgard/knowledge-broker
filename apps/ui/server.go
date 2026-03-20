package ui

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/server"
)

//go:embed index.html
var staticFS embed.FS

// Server wraps a core KB HTTPServer and serves the UI.
type Server struct {
	core   *server.HTTPServer
	logger *slog.Logger
}

// NewServer creates a UI server wrapping the given core KB server.
func NewServer(core *server.HTTPServer, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{core: core, logger: logger}
}

// ListenAndServe starts the combined server.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// Serve index.html at root
	indexFile, _ := fs.ReadFile(staticFS, "index.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Delegate non-root paths to core handler
			s.core.Handler().ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexFile)
	})

	// Delegate all /v1/ paths to core
	mux.Handle("/v1/", s.core.Handler())
	mux.Handle("/metrics", s.core.Handler())

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	host := addr
	if host == "" || host[0] == ':' {
		host = "localhost" + host
	}
	s.logger.Info("starting UI server", "addr", addr, "ui", "http://"+host)
	return srv.ListenAndServe()
}
