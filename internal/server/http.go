package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/feedback"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// IngestFragment is a single fragment in the ingest request (without ID or embedding).
type IngestFragment struct {
	Content      string    `json:"content"`
	SourceType   string    `json:"source_type"`
	SourceName   string    `json:"source_name,omitempty"`
	SourcePath   string    `json:"source_path"`
	SourceURI    string    `json:"source_uri"`
	LastModified time.Time `json:"last_modified"`
	Author       string    `json:"author"`
	FileType     string    `json:"file_type"`
	Checksum     string    `json:"checksum"`
}

// IngestRequest is the JSON body for POST /v1/ingest.
type IngestRequest struct {
	Fragments []IngestFragment `json:"fragments"`
	Deleted   []DeletedPath    `json:"deleted,omitempty"`
}

// DeletedPath identifies a source type and path to delete.
type DeletedPath struct {
	SourceType string `json:"source_type"`
	Path       string `json:"path"`
}

// HTTPServer serves the Knowledge Broker HTTP API.
type HTTPServer struct {
	engine   *query.Engine
	feedback *feedback.Service
	embedder embedding.Embedder
	store    store.Store
	logger   *slog.Logger
	mux      *http.ServeMux
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(engine *query.Engine, fb *feedback.Service, emb embedding.Embedder, st store.Store, logger *slog.Logger) *HTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &HTTPServer{
		engine:   engine,
		feedback: fb,
		embedder: emb,
		store:    st,
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
	s.mux.HandleFunc("/v1/ingest", s.handleIngest)
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

// handleIngest receives text fragments, embeds them, and stores them.
func (s *HTTPServer) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Fragments) == 0 && len(req.Deleted) == 0 {
		http.Error(w, "fragments array or deleted array is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Handle deletions.
	if len(req.Deleted) > 0 {
		// Group deletions by source type.
		byType := make(map[string][]string)
		for _, d := range req.Deleted {
			byType[d.SourceType] = append(byType[d.SourceType], d.Path)
		}
		for sourceType, paths := range byType {
			if err := s.store.DeleteByPaths(ctx, sourceType, paths); err != nil {
				s.logger.Error("delete failed", "error", err, "source_type", sourceType)
				http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	if len(req.Fragments) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"ingested": 0})
		return
	}

	// Collect texts for batch embedding.
	texts := make([]string, len(req.Fragments))
	for i, f := range req.Fragments {
		texts[i] = f.Content
	}

	embeddings, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		s.logger.Error("embedding failed", "error", err)
		http.Error(w, fmt.Sprintf("embedding failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Build SourceFragment objects with generated IDs.
	fragments := make([]model.SourceFragment, len(req.Fragments))
	for i, f := range req.Fragments {
		// Generate ID the same way as the ingest pipeline:
		// sha256(source_type:source_path:index)[:16]
		idInput := fmt.Sprintf("%s:%s:%d", f.SourceType, f.SourcePath, i)
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(idInput)))[:16]

		// Use provided FileType, or derive from path.
		fileType := f.FileType
		if fileType == "" {
			fileType = filepath.Ext(f.SourcePath)
		}

		fragments[i] = model.SourceFragment{
			ID:           id,
			Content:      f.Content,
			SourceType:   f.SourceType,
			SourceName:   f.SourceName,
			SourcePath:   f.SourcePath,
			SourceURI:    f.SourceURI,
			LastModified: f.LastModified,
			Author:       f.Author,
			FileType:     fileType,
			Checksum:     f.Checksum,
			Embedding:    embeddings[i],
		}
	}

	if err := s.store.UpsertFragments(ctx, fragments); err != nil {
		s.logger.Error("upsert failed", "error", err)
		http.Error(w, fmt.Sprintf("store failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"ingested": len(fragments)})
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
