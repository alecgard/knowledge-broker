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

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// PipelineConfig holds settings needed to configure the ingestion pipeline
// when source management is enabled via the HTTP server.
type PipelineConfig struct {
	OllamaURL      string
	EnrichModel    string
	WorkerCount    int
	SkipEnrichment bool
	MaxChunkSize   int
	ChunkOverlap   int
}

// HTTPServerOption configures optional HTTPServer dependencies.
type HTTPServerOption func(*HTTPServer)

// WithPipeline enables source connection and background ingestion.
func WithPipeline(extractors *extractor.Registry, cfg PipelineConfig, httpClient *http.Client, jobs *JobTracker) HTTPServerOption {
	return func(s *HTTPServer) {
		s.extractors = extractors
		s.pipelineCfg = &cfg
		s.httpClient = httpClient
		s.jobs = jobs
	}
}

// HTTPServer serves the Knowledge Broker HTTP API.
type HTTPServer struct {
	engine      *query.Engine
	embedder    embedding.Embedder
	store       store.Store
	logger      *slog.Logger
	mux         *http.ServeMux
	version     string
	extractors  *extractor.Registry   // nil = source mgmt disabled
	pipelineCfg *PipelineConfig
	httpClient  *http.Client
	jobs        *JobTracker
	serverCtx   context.Context      // cancelled on shutdown
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(engine *query.Engine, emb embedding.Embedder, st store.Store, logger *slog.Logger, version ...string) *HTTPServer {
	v := ""
	if len(version) > 0 {
		v = version[0]
	}
	return NewHTTPServerWithOptions(engine, emb, st, logger, v)
}

// NewHTTPServerWithOptions creates a new HTTP server with optional pipeline support.
func NewHTTPServerWithOptions(engine *query.Engine, emb embedding.Embedder, st store.Store, logger *slog.Logger, ver string, opts ...HTTPServerOption) *HTTPServer {
	if logger == nil {
		logger = slog.Default()
	}
	if ver == "" {
		ver = "0.1.0"
	}
	s := &HTTPServer{
		engine:    engine,
		embedder:  emb,
		store:     st,
		logger:    logger,
		mux:       http.NewServeMux(),
		version:   ver,
		serverCtx: context.Background(),
	}
	for _, opt := range opts {
		opt(s)
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
	if s.extractors != nil {
		s.mux.HandleFunc("/v1/sources/connect", s.handleConnectSource)
		s.mux.HandleFunc("/v1/sources/sync", s.handleSyncSource)
		s.mux.HandleFunc("/v1/sources/jobs", s.handleListJobs)
	}
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

// --- Source connection endpoints ---

// connectRequest is the JSON body for POST /v1/sources/connect.
type connectRequest struct {
	SourceType string            `json:"source_type"`
	Config     map[string]string `json:"config"`
}

// handleConnectSource validates config, registers the source, and launches background ingestion.
func (s *HTTPServer) handleConnectSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields per source type.
	switch req.SourceType {
	case "slack":
		if req.Config["token"] == "" || req.Config["channels"] == "" {
			http.Error(w, "slack requires token and channels", http.StatusBadRequest)
			return
		}
	case "git":
		if req.Config["url"] == "" {
			http.Error(w, "git requires url", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, fmt.Sprintf("unknown source_type: %s", req.SourceType), http.StatusBadRequest)
		return
	}

	// Set local mode so tokens are persisted.
	if req.Config == nil {
		req.Config = make(map[string]string)
	}
	req.Config["mode"] = model.SourceModeLocal

	// Create connector to validate config and derive source name.
	src := model.Source{
		SourceType: req.SourceType,
		Config:     req.Config,
	}
	conn, err := connector.FromSource(src)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid config: %v", err), http.StatusBadRequest)
		return
	}
	src.SourceName = conn.SourceName()
	src.Config = conn.Config(model.SourceModeLocal)

	// Strip secrets from the config stored in the database. The connector
	// already has the token in memory for this ingestion run.
	storedSrc := src
	storedCfg := make(map[string]string, len(src.Config))
	for k, v := range src.Config {
		storedCfg[k] = v
	}
	delete(storedCfg, "token")
	storedSrc.Config = storedCfg

	// Atomically claim the job slot — reject if already running.
	jobID, ok := s.jobs.Start(src.SourceType, src.SourceName)
	if !ok {
		http.Error(w, "ingestion already running for this source", http.StatusConflict)
		return
	}

	// Register source in store (without token).
	if err := s.store.RegisterSource(r.Context(), storedSrc); err != nil {
		s.logger.Error("register source failed", "error", err)
		s.jobs.Finish(jobID, 0, 0, 0, err)
		http.Error(w, fmt.Sprintf("register source failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Start background ingestion (with token still in src.Config for the connector).
	go s.runIngestion(s.serverCtx, src, jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"source": src,
		"job_id": jobID,
	})
}

// handleSyncSource triggers re-ingestion of an existing source.
func (s *HTTPServer) handleSyncSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SourceType string `json:"source_type"`
		SourceName string `json:"source_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	src, err := s.store.GetSource(r.Context(), req.SourceType, req.SourceName)
	if err != nil {
		s.logger.Error("get source failed", "error", err)
		http.Error(w, fmt.Sprintf("get source failed: %v", err), http.StatusInternalServerError)
		return
	}
	if src == nil {
		http.Error(w, "source not found", http.StatusNotFound)
		return
	}

	jobID, ok := s.jobs.Start(src.SourceType, src.SourceName)
	if !ok {
		http.Error(w, "ingestion already running for this source", http.StatusConflict)
		return
	}
	go s.runIngestion(s.serverCtx, *src, jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

// handleListJobs returns all job statuses.
func (s *HTTPServer) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.jobs.List())
}

// runIngestion runs the full ingest pipeline for a source in the background.
func (s *HTTPServer) runIngestion(ctx context.Context, src model.Source, jobID string) {
	conn, err := connector.FromSource(src)
	if err != nil {
		s.jobs.Finish(jobID, 0, 0, 0, err)
		return
	}

	workers := 4
	if s.pipelineCfg != nil && s.pipelineCfg.WorkerCount > 0 {
		workers = s.pipelineCfg.WorkerCount
	}

	pipeline := ingest.NewPipeline(s.store, s.embedder, s.extractors, workers, s.logger)
	pipeline.OnProgress = func(completed, total int) {
		s.jobs.Update(jobID, completed, total)
	}

	// Configure enrichment if available.
	if s.pipelineCfg != nil && !s.pipelineCfg.SkipEnrichment && s.pipelineCfg.EnrichModel != "" && s.httpClient != nil {
		enricher := enrich.NewOllamaEnricher(s.pipelineCfg.OllamaURL, s.pipelineCfg.EnrichModel, "", s.httpClient, s.logger)
		pipeline.SetEnrichment(ingest.EnrichmentConfig{Enricher: enricher})
	}

	result, err := pipeline.Run(ctx, conn)
	if result == nil {
		s.jobs.Finish(jobID, 0, 0, 0, err)
	} else {
		s.jobs.Finish(jobID, result.Added, result.Deleted, result.Skipped, err)
	}

	if err == nil {
		src.LastIngest = time.Now()
		if regErr := s.store.RegisterSource(ctx, src); regErr != nil {
			s.logger.Error("update source last_ingest failed", "error", regErr)
		}
	}
}

// ListenAndServe starts the HTTP server.
func (s *HTTPServer) ListenAndServe(ctx context.Context, addr string) error {
	s.serverCtx = ctx
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
