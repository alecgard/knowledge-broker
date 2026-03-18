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
func NewMCPServer(engine *query.Engine, st store.Store, logger *slog.Logger, version string) *MCPServer {
	if logger == nil {
		logger = slog.Default()
	}

	s := &MCPServer{
		engine: engine,
		store:  st,
		logger: logger,
	}

	if version == "" {
		version = "0.1.0"
	}

	s.server = server.NewMCPServer(
		"knowledge-broker",
		version,
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(false),
	)

	s.server.AddTool(mcp.NewTool("query",
		mcp.WithDescription("Search the organization's knowledge base for answers. Use this whenever you need context about codebases, architecture, APIs, project decisions, or documentation that you don't already have. Returns synthesized answers with confidence signals and source citations by default. Set raw=true for raw fragments."),
		mcp.WithString("query",
			mcp.Description("The query to search for"),
			mcp.Required(),
		),
		mcp.WithString("topics",
			mcp.Description("Optional comma-separated topics to boost relevance (e.g., 'authentication,deployment')"),
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
			mcp.Description("Optional comma-separated source types to filter results. Valid types: filesystem, git, confluence, slack, github_wiki"),
		),
		mcp.WithBoolean("no_expand",
			mcp.Description("If true, disable multi-query expansion. Useful for precise queries where you know the exact terms."),
		),
	), s.handleQuery)

	s.server.AddTool(mcp.NewTool("list-sources",
		mcp.WithDescription("List all knowledge sources available for querying — shows what documentation, repos, and systems have been indexed, with fragment counts and last sync times. Call this to understand what knowledge is available."),
	), s.handleListSources)

	s.server.AddPrompt(mcp.NewPrompt("kb-instructions",
		mcp.WithPromptDescription("Instructions for using the Knowledge Broker knowledge base"),
	), s.handleKBInstructions)

	return s
}

// MCPTransports controls which MCP transports to start.
type MCPTransports struct {
	Stdio bool
	SSE   bool
}

// ServeStdio starts the MCP server on stdio only.
func (s *MCPServer) ServeStdio() error {
	s.logger.Info("starting MCP server on stdio")
	return server.ServeStdio(s.server)
}

// Serve starts the requested MCP transports concurrently, sharing the same
// underlying MCPServer instance. It blocks until ctx is cancelled or a
// transport returns an error, then cleans up.
func (s *MCPServer) Serve(ctx context.Context, sseAddr string, t MCPTransports) error {
	if !t.Stdio && !t.SSE {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		once      sync.Once
		first     error
		sseServer *server.SSEServer
	)
	setErr := func(err error) {
		once.Do(func() { first = err })
		cancel()
	}

	if t.SSE {
		sseServer = server.NewSSEServer(s.server,
			server.WithBaseURL("http://"+sseAddr),
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.logger.Info("starting MCP SSE transport", "addr", sseAddr)
			if err := sseServer.Start(sseAddr); err != nil {
				setErr(fmt.Errorf("sse: %w", err))
			}
		}()
	}

	if t.Stdio {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.logger.Info("starting MCP stdio transport")
			if err := server.ServeStdio(s.server); err != nil {
				setErr(fmt.Errorf("stdio: %w", err))
			}
		}()
	}

	<-ctx.Done()
	s.logger.Info("shutting down MCP transports")
	if sseServer != nil {
		if err := sseServer.Shutdown(context.Background()); err != nil {
			s.logger.Warn("SSE shutdown error", "error", err)
		}
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

	topics := splitCSV(args, "topics")

	limit := 20
	if limitRaw, ok := args["limit"].(float64); ok && limitRaw > 0 {
		limit = int(limitRaw)
	}

	sources := splitCSV(args, "sources")
	sourceTypes := splitCSV(args, "source_types")

	// Default to synthesis mode (rawMode=false) unless explicitly set to true.
	rawMode := false
	if rawVal, ok := args["raw"].(bool); ok {
		rawMode = rawVal
	}

	noExpand := false
	if v, ok := args["no_expand"].(bool); ok {
		noExpand = v
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
		NoExpand:    noExpand,
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

// splitCSV extracts a comma-separated string argument and returns the trimmed, non-empty parts.
func splitCSV(args map[string]interface{}, key string) []string {
	raw, ok := args[key].(string)
	if !ok || raw == "" {
		return nil
	}
	var result []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
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
		Description   string `json:"description,omitempty"`
		FragmentCount int    `json:"fragment_count"`
		LastIngest    string `json:"last_ingest,omitempty"`
	}

	result := make([]sourceInfo, 0, len(sources))
	for _, src := range sources {
		key := src.SourceType + "/" + src.SourceName
		info := sourceInfo{
			SourceType:    src.SourceType,
			SourceName:    src.SourceName,
			Description:   src.Description,
			FragmentCount: counts[key],
		}
		if src.LastIngest != nil {
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

func (s *MCPServer) handleKBInstructions(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}

	counts, err := s.store.CountFragmentsBySource(ctx)
	if err != nil {
		return nil, fmt.Errorf("count fragments: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(`You have access to Knowledge Broker, a shared knowledge base for this organization. It is available via the "query" and "list-sources" MCP tools.

Use Knowledge Broker:
- Before making assumptions about the codebase, architecture, or project conventions
- When you need context you don't already have about how things work or why they were built a certain way
- When the user asks questions that span multiple repos, docs, or systems

Confidence scores: every response includes a confidence score between 0 and 1. If the score is below 0.5, flag the uncertainty to the user rather than treating the answer as fact. If sources contradict each other, surface both claims with their dates.

`)

	if len(sources) > 0 {
		sb.WriteString("Available sources:\n")
		for _, src := range sources {
			key := src.SourceType + "/" + src.SourceName
			count := counts[key]
			desc := src.Description
			if desc == "" {
				desc = deriveSourceLabel(src.SourceType, src.SourceName)
			}
			fmt.Fprintf(&sb, "- %s/%s: %q (%d fragments)\n", src.SourceType, src.SourceName, desc, count)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`Use synthesis mode (default) for direct answers. Use raw=true when you need to examine the underlying fragments yourself. Use the topics parameter to boost relevance for specific domains (e.g., "authentication,deployment"). Use sources or source_types to narrow results to specific repos or connector types.`)

	return &mcp.GetPromptResult{
		Description: "Instructions for using the Knowledge Broker knowledge base",
		Messages: []mcp.PromptMessage{
			{
				Role:    mcp.RoleUser,
				Content: mcp.NewTextContent(sb.String()),
			},
		},
	}, nil
}

// deriveSourceLabel creates a human-readable label from source type and name.
func deriveSourceLabel(sourceType, sourceName string) string {
	switch sourceType {
	case "git":
		return "Git repository: " + sourceName
	case "filesystem":
		return "Local directory: " + sourceName
	case "confluence":
		return "Confluence space: " + sourceName
	case "slack":
		return "Slack channels: " + sourceName
	case "github_wiki":
		return "GitHub wiki: " + sourceName
	default:
		return sourceType + ": " + sourceName
	}
}
