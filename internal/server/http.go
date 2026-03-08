package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/knowledge-broker/knowledge-broker/internal/feedback"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
)

// HTTPServer serves the Knowledge Broker HTTP API.
type HTTPServer struct {
	engine   *query.Engine
	feedback *feedback.Service
	logger   *slog.Logger
	mux      *http.ServeMux
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(engine *query.Engine, fb *feedback.Service, logger *slog.Logger) *HTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &HTTPServer{
		engine:   engine,
		feedback: fb,
		logger:   logger,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *HTTPServer) routes() {
	s.mux.HandleFunc("/v1/query", s.handleQuery)
	s.mux.HandleFunc("/v1/feedback", s.handleFeedback)
	s.mux.HandleFunc("/v1/health", s.handleHealth)
}

// Handler returns the http.Handler.
func (s *HTTPServer) Handler() http.Handler {
	return s.mux
}

// handleQuery streams a query response as SSE.
func (s *HTTPServer) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req model.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "messages array is required", http.StatusBadRequest)
		return
	}

	// Set up SSE headers.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	onText := func(text string) {
		data, _ := json.Marshal(map[string]string{"type": "text", "content": text})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	answer, err := s.engine.Query(ctx, req, onText)
	if err != nil {
		s.logger.Error("query failed", "error", err)
		data, _ := json.Marshal(map[string]string{"type": "error", "content": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Send the final answer with metadata.
	data, _ := json.Marshal(map[string]interface{}{
		"type":           "done",
		"confidence":     answer.Confidence,
		"sources":        answer.Sources,
		"contradictions": answer.Contradictions,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// handleFeedback records feedback on a fragment.
func (s *HTTPServer) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var fb model.Feedback
	if err := json.NewDecoder(r.Body).Decode(&fb); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.feedback.Submit(r.Context(), fb); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleHealth returns health status.
func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ListenAndServe starts the HTTP server.
func (s *HTTPServer) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	s.logger.Info("starting HTTP server", "addr", addr)
	return srv.ListenAndServe()
}
