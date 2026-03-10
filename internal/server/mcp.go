package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
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
		mcp.WithDescription("Ask a question and get an answer from the knowledge base. By default uses raw mode (no LLM) returning relevant fragments with confidence signals. Set raw=false for LLM-synthesised answers."),
		mcp.WithString("question",
			mcp.Description("The question to ask"),
			mcp.Required(),
		),
		mcp.WithString("topics",
			mcp.Description("Optional comma-separated topics to boost relevance (e.g., 'authentication,octroi')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of fragments to retrieve (default 20)"),
		),
		mcp.WithBoolean("raw",
			mcp.Description("If true (default), return raw fragments without LLM synthesis. If false, use LLM to synthesise an answer."),
		),
	), s.handleQuery)

	s.server.AddTool(mcp.NewTool("list-sources",
		mcp.WithDescription("List all registered knowledge sources with their fragment counts and last ingestion time."),
	), s.handleListSources)

	return s
}

// ServeStdio starts the MCP server on stdio.
func (s *MCPServer) ServeStdio() error {
	s.logger.Info("starting MCP server on stdio")
	return server.ServeStdio(s.server)
}

func (s *MCPServer) handleQuery(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return mcp.NewToolResultError("question is required"), nil
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

	// Default to raw mode (true) unless explicitly set to false.
	rawMode := true
	if rawVal, ok := args["raw"].(bool); ok {
		rawMode = rawVal
	}

	req := model.QueryRequest{
		Messages: []model.Message{
			{Role: "user", Content: question},
		},
		Limit:   limit,
		Concise: true,
		Topics:  topics,
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
