package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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
	version  string
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(engine *query.Engine, emb embedding.Embedder, st store.Store, logger *slog.Logger, version ...string) *HTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	v := "0.1.0"
	if len(version) > 0 && version[0] != "" {
		v = version[0]
	}
	s := &HTTPServer{
		engine:   engine,
		embedder: emb,
		store:    st,
		logger:   logger,
		mux:      http.NewServeMux(),
		version:  v,
	}
	s.routes()
	return s
}

func (s *HTTPServer) routes() {
	s.mux.HandleFunc("/v1/query", s.handleQuery)
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/ingest", s.handleIngest)
	s.mux.HandleFunc("/v1/sources", s.handleSources)
	s.mux.HandleFunc("/v1/sources/import", s.handleSourcesImport)
	s.mux.HandleFunc("/v1/version", s.handleVersion)
	s.mux.HandleFunc("/v1/export", s.handleExport)
}

// Handler returns the http.Handler with CORS support for cross-origin UI access.
func (s *HTTPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, ngrok-skip-browser-warning")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.mux.ServeHTTP(w, r)
	})
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
	if req.Mode == model.ModeRaw {
		s.handleQueryRaw(w, r, req)
		return
	}

	// Synthesis is the default. If no LLM is configured, return a clear error
	// instead of silently falling back to raw mode.
	if !s.engine.HasLLM() {
		http.Error(w, "Synthesis mode requires ANTHROPIC_API_KEY. Set it in .env, or use \"mode\":\"raw\" for retrieval without LLM.", http.StatusBadRequest)
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

// handleHealth returns health status including embedder connectivity.
func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := s.embedder.CheckHealth(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}
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
		json.NewEncoder(w).Encode(map[string]int{"ingested": 0, "skipped": 0})
		return
	}

	// Deduplicate: skip fragments whose checksum already exists in the store,
	// avoiding unnecessary embedding calls and write contention.
	type sourceKey struct{ typ, name string }
	checksumCache := make(map[sourceKey]map[string]string)
	getChecksums := func(typ, name string) (map[string]string, error) {
		k := sourceKey{typ, name}
		if m, ok := checksumCache[k]; ok {
			return m, nil
		}
		m, err := s.store.GetChecksums(ctx, typ, name)
		if err != nil {
			return nil, err
		}
		checksumCache[k] = m
		return m, nil
	}

	// Identify which fragments actually need embedding.
	type indexedFragment struct {
		origIdx int
		frag    model.IngestFragment
	}
	var needEmbed []indexedFragment
	skipped := 0
	for i, f := range req.Fragments {
		existing, err := getChecksums(f.SourceType, f.SourceName)
		if err != nil {
			s.logger.Error("get checksums failed", "error", err)
			http.Error(w, fmt.Sprintf("checksum lookup failed: %v", err), http.StatusInternalServerError)
			return
		}
		if existing[f.SourcePath] == f.Checksum && f.Checksum != "" {
			skipped++
			continue
		}
		needEmbed = append(needEmbed, indexedFragment{origIdx: i, frag: f})
	}

	if len(needEmbed) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"ingested": 0, "skipped": skipped})
		return
	}

	// Collect texts for batch embedding, processing in groups of 50 to
	// avoid overwhelming the embedding service on large ingests.
	const embedBatchSize = 50
	texts := make([]string, len(needEmbed))
	for i, nf := range needEmbed {
		texts[i] = nf.frag.Content
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
	fragments := make([]model.SourceFragment, len(needEmbed))
	for i, nf := range needEmbed {
		f := nf.frag
		// Use provided FileType, or derive from path.
		fileType := f.FileType
		if fileType == "" {
			fileType = filepath.Ext(f.SourcePath)
		}

		fragments[i] = model.SourceFragment{
			ID:           model.FragmentID(f.SourceType, f.SourcePath, nf.origIdx),
			RawContent:   f.Content,
			SourceType:   f.SourceType,
			SourceName:   f.SourceName,
			SourcePath:   f.SourcePath,
			SourceURI:    f.SourceURI,
			ContentDate: f.ContentDate,
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
	json.NewEncoder(w).Encode(map[string]int{"ingested": len(fragments), "skipped": skipped})
}

// handleSources routes /v1/sources to GET (list), PATCH (update), or DELETE (remove).
func (s *HTTPServer) handleSources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSources(w, r)
	case http.MethodPatch:
		s.handleUpdateSource(w, r)
	case http.MethodDelete:
		s.handleDeleteSource(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListSources returns all registered sources.
func (s *HTTPServer) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources(r.Context())
	if err != nil {
		s.logger.Error("list sources failed", "error", err)
		http.Error(w, fmt.Sprintf("list sources failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

// handleUpdateSource handles PATCH /v1/sources for updating source descriptions.
func (s *HTTPServer) handleUpdateSource(w http.ResponseWriter, r *http.Request) {

	var req struct {
		SourceType  string `json:"source_type"`
		SourceName  string `json:"source_name"`
		Description string `json:"description"`
		Force       bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.SourceType == "" || req.SourceName == "" {
		http.Error(w, "source_type and source_name are required", http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateSourceDescription(r.Context(), req.SourceType, req.SourceName, req.Description, req.Force); err != nil {
		if strings.Contains(err.Error(), "already has a description") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.logger.Error("update source failed", "error", err)
		http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDeleteSource handles DELETE /v1/sources for removing a source and its fragments.
func (s *HTTPServer) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceType string `json:"source_type"`
		SourceName string `json:"source_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.SourceType == "" || req.SourceName == "" {
		http.Error(w, "source_type and source_name are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Count fragments before deletion.
	counts, err := s.store.CountFragmentsBySource(ctx)
	if err != nil {
		s.logger.Error("count fragments failed", "error", err)
		http.Error(w, fmt.Sprintf("count fragments failed: %v", err), http.StatusInternalServerError)
		return
	}
	key := req.SourceType + "/" + req.SourceName
	fragCount := counts[key]

	if err := s.store.DeleteFragmentsBySource(ctx, req.SourceType, req.SourceName); err != nil {
		s.logger.Error("delete fragments failed", "error", err)
		http.Error(w, fmt.Sprintf("delete fragments failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := s.store.DeleteSource(ctx, req.SourceType, req.SourceName); err != nil {
		s.logger.Error("delete source failed", "error", err)
		http.Error(w, fmt.Sprintf("delete source failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"deleted_fragments": fragCount,
	})
}

// handleSourcesImport handles POST /v1/sources/import for importing sources from JSON.
func (s *HTTPServer) handleSourcesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var sources []model.Source
	if err := json.NewDecoder(r.Body).Decode(&sources); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for i := range sources {
		sources[i].LastIngest = time.Time{}
		if err := s.store.RegisterSource(ctx, sources[i]); err != nil {
			s.logger.Error("register source failed", "error", err)
			http.Error(w, fmt.Sprintf("register source failed: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"imported": len(sources)})
}

// handleVersion handles GET /v1/version.
func (s *HTTPServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"version": s.version})
}

// ExportFragment is the JSON representation of a fragment for the export endpoint.
type ExportFragment struct {
	SourceType string    `json:"source_type"`
	SourceName string    `json:"source_name"`
	SourcePath string    `json:"source_path"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"embedding"`
}

// handleExport handles GET /v1/export for exporting fragment embeddings.
func (s *HTTPServer) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fragments, err := s.store.ExportFragments(r.Context())
	if err != nil {
		s.logger.Error("export fragments failed", "error", err)
		http.Error(w, fmt.Sprintf("export failed: %v", err), http.StatusInternalServerError)
		return
	}

	out := make([]ExportFragment, len(fragments))
	for i, f := range fragments {
		out[i] = ExportFragment{
			SourceType: f.SourceType,
			SourceName: f.SourceName,
			SourcePath: f.SourcePath,
			Content:    f.RawContent,
			Embedding:  f.Embedding,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ListenAndServe starts the HTTP server.
func (s *HTTPServer) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
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

	s.logger.Info("starting HTTP server", "addr", addr)
	return srv.ListenAndServe()
}
