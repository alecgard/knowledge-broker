package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// MCPServer serves Knowledge Broker as an MCP tool provider.
type MCPServer struct {
	engine *query.Engine
	store  store.Store
	logger *slog.Logger
	server *server.MCPServer
}

// NewMCPServer creates a new MCP server.
func NewMCPServer(engine *query.Engine, st store.Store, logger *slog.Logger) *MCPServer {
	if logger == nil {
		logger = slog.Default()
	}

	s := &MCPServer{
		engine: engine,
		store:  st,
		logger: logger,
	}

	s.server = server.NewMCPServer(
		"knowledge-broker",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	s.server.AddTool(mcp.NewTool("query",
		mcp.WithDescription("Ask a question and get an answer from the knowledge base. By default uses synthesis mode (LLM-synthesised answers with confidence signals). Set raw=true for raw fragment retrieval without LLM."),
		mcp.WithString("query",
			mcp.Description("The query to search for"),
			mcp.Required(),
		),
		mcp.WithString("topics",
			mcp.Description("Optional comma-separated topics to boost relevance (e.g., 'authentication,octroi')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of fragments to retrieve (default 20)"),
		),
		mcp.WithBoolean("raw",
			mcp.Description("If true, return raw fragments without LLM synthesis. Defaults to false (synthesis mode)."),
		),
		mcp.WithString("sources",
			mcp.Description("Optional comma-separated source names to filter results (e.g., 'owner/repo,other/repo')"),
		),
		mcp.WithString("source_types",
			mcp.Description("Optional comma-separated source types to filter results (e.g., 'git,confluence')"),
		),
	), s.handleQuery)

	s.server.AddTool(mcp.NewTool("list-sources",
		mcp.WithDescription("List all registered knowledge sources with their fragment counts and last ingestion time."),
	), s.handleListSources)

	return s
}

// ServeStdio starts the MCP server on stdio only.
func (s *MCPServer) ServeStdio() error {
	s.logger.Info("starting MCP server on stdio")
	return server.ServeStdio(s.server)
}

// Serve starts both stdio and SSE transports concurrently, sharing the same
// underlying MCPServer instance. It blocks until ctx is cancelled or either
// transport returns an error, then cleans up both.
func (s *MCPServer) Serve(ctx context.Context, sseAddr string) error {
	sseServer := server.NewSSEServer(s.server,
		server.WithBaseURL("http://"+sseAddr),
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg   sync.WaitGroup
		once sync.Once
		first error
	)
	setErr := func(err error) {
		once.Do(func() { first = err })
		cancel()
	}

	// Start SSE transport.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.logger.Info("starting MCP SSE transport", "addr", sseAddr)
		if err := sseServer.Start(sseAddr); err != nil {
			setErr(fmt.Errorf("sse: %w", err))
		}
	}()

	// Start stdio transport.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.logger.Info("starting MCP stdio transport")
		if err := server.ServeStdio(s.server); err != nil {
			setErr(fmt.Errorf("stdio: %w", err))
		}
	}()

	// Wait for cancellation, then shut down SSE gracefully.
	<-ctx.Done()
	s.logger.Info("shutting down MCP transports")
	if err := sseServer.Shutdown(context.Background()); err != nil {
		s.logger.Warn("SSE shutdown error", "error", err)
	}

	wg.Wait()

	if first != nil {
		return first
	}
	return nil
}

func (s *MCPServer) handleQuery(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	question, ok := args["query"].(string)
	if !ok || question == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	var topics []string
	if topicsRaw, ok := args["topics"].(string); ok && topicsRaw != "" {
		for _, t := range strings.Split(topicsRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				topics = append(topics, t)
			}
		}
	}

	limit := 20
	if limitRaw, ok := args["limit"].(float64); ok && limitRaw > 0 {
		limit = int(limitRaw)
	}

	var sources []string
	if sourcesRaw, ok := args["sources"].(string); ok && sourcesRaw != "" {
		for _, s := range strings.Split(sourcesRaw, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				sources = append(sources, s)
			}
		}
	}

	var sourceTypes []string
	if typesRaw, ok := args["source_types"].(string); ok && typesRaw != "" {
		for _, t := range strings.Split(typesRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				sourceTypes = append(sourceTypes, t)
			}
		}
	}

	// Default to synthesis mode (rawMode=false) unless explicitly set to true.
	rawMode := false
	if rawVal, ok := args["raw"].(bool); ok {
		rawMode = rawVal
	}

	// If synthesis is requested but no LLM is configured, return a clear error.
	if !rawMode && !s.engine.HasLLM() {
		return mcp.NewToolResultError("Synthesis mode requires ANTHROPIC_API_KEY. Set it in .env, or use raw=true for retrieval without LLM."), nil
	}

	req := model.QueryRequest{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: question},
		},
		Limit:       limit,
		Concise:     true,
		Topics:      topics,
		Sources:     sources,
		SourceTypes: sourceTypes,
	}

	if rawMode {
		result, err := s.engine.QueryRaw(ctx, req)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
		}
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}

	// LLM synthesis mode — collect full response without streaming.
	var fullText string
	answer, err := s.engine.Query(ctx, req, func(text string) {
		fullText += text
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}

	jsonBytes, err := json.Marshal(answer)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(jsonBytes)), nil
}

func (s *MCPServer) handleListSources(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list sources failed: %v", err)), nil
	}

	counts, err := s.store.CountFragmentsBySource(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("count fragments failed: %v", err)), nil
	}

	type sourceInfo struct {
		SourceType    string `json:"source_type"`
		SourceName    string `json:"source_name"`
		FragmentCount int    `json:"fragment_count"`
		LastIngest    string `json:"last_ingest,omitempty"`
	}

	result := make([]sourceInfo, 0, len(sources))
	for _, src := range sources {
		key := src.SourceType + "/" + src.SourceName
		info := sourceInfo{
			SourceType:    src.SourceType,
			SourceName:    src.SourceName,
			FragmentCount: counts[key],
		}
		if !src.LastIngest.IsZero() {
			info.LastIngest = src.LastIngest.Format("2006-01-02T15:04:05Z")
		}
		result = append(result, info)
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(jsonBytes)), nil
}
