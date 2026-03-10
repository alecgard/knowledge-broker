package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// HTTPServer serves the Knowledge Broker HTTP API.
type HTTPServer struct {
	engine   *query.Engine
	embedder embedding.Embedder
	store    store.Store
	logger   *slog.Logger
	mux      *http.ServeMux
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(engine *query.Engine, emb embedding.Embedder, st store.Store, logger *slog.Logger) *HTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &HTTPServer{
		engine:   engine,
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
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/ingest", s.handleIngest)
}

// Handler returns the http.Handler.
func (s *HTTPServer) Handler() http.Handler {
	return s.mux
}

// handleQuery handles query requests. Streams SSE by default; set
// "stream": false in the request body for a single JSON response.
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

	// Raw retrieval mode: no LLM needed.
	if req.Mode == "raw" {
		s.handleQueryRaw(w, r, req)
		return
	}

	// Default to non-streaming; stream only when explicitly requested.
	if req.Stream != nil && *req.Stream {
		s.handleQueryStream(w, r, req)
		return
	}
	s.handleQuerySync(w, r, req)
}

func (s *HTTPServer) handleQueryRaw(w http.ResponseWriter, r *http.Request, req model.QueryRequest) {
	result, err := s.engine.QueryRaw(r.Context(), req)
	if err != nil {
		s.logger.Error("raw query failed", "error", err)
		http.Error(w, fmt.Sprintf("query failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *HTTPServer) handleQueryStream(w http.ResponseWriter, r *http.Request, req model.QueryRequest) {
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

	data, _ := json.Marshal(map[string]interface{}{
		"type":           "done",
		"confidence":     answer.Confidence,
		"sources":        answer.Sources,
		"contradictions": answer.Contradictions,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func (s *HTTPServer) handleQuerySync(w http.ResponseWriter, r *http.Request, req model.QueryRequest) {
	answer, err := s.engine.Query(r.Context(), req, nil)
	if err != nil {
		s.logger.Error("query failed", "error", err)
		http.Error(w, fmt.Sprintf("query failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)
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

	var req model.IngestRequest
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
		// Group deletions by (source_type, source_name).
		type sourceKey struct{ typ, name string }
		bySource := make(map[sourceKey][]string)
		for _, d := range req.Deleted {
			k := sourceKey{d.SourceType, d.SourceName}
			bySource[k] = append(bySource[k], d.Path)
		}
		for k, paths := range bySource {
			if err := s.store.DeleteByPaths(ctx, k.typ, k.name, paths); err != nil {
				s.logger.Error("delete failed", "error", err, "source_type", k.typ, "source_name", k.name)
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

	// Collect texts for batch embedding, processing in groups of 50 to
	// avoid overwhelming the embedding service on large ingests.
	const embedBatchSize = 50
	texts := make([]string, len(req.Fragments))
	for i, f := range req.Fragments {
		texts[i] = f.Content
	}

	embeddings := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := s.embedder.EmbedBatch(ctx, texts[start:end])
		if err != nil {
			s.logger.Error("embedding failed", "error", err)
			http.Error(w, fmt.Sprintf("embedding failed: %v", err), http.StatusInternalServerError)
			return
		}
		copy(embeddings[start:end], batch)
	}

	// Build SourceFragment objects with generated IDs.
	fragments := make([]model.SourceFragment, len(req.Fragments))
	for i, f := range req.Fragments {
		// Use provided FileType, or derive from path.
		fileType := f.FileType
		if fileType == "" {
			fileType = filepath.Ext(f.SourcePath)
		}

		fragments[i] = model.SourceFragment{
			ID:           model.FragmentID(f.SourceType, f.SourcePath, i),
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
