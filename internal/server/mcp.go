package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/knowledge-broker/knowledge-broker/internal/feedback"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
)

// MCPServer serves Knowledge Broker as an MCP tool provider.
type MCPServer struct {
	engine   *query.Engine
	feedback *feedback.Service
	logger   *slog.Logger
	server   *server.MCPServer
}

// NewMCPServer creates a new MCP server.
func NewMCPServer(engine *query.Engine, fb *feedback.Service, logger *slog.Logger) *MCPServer {
	if logger == nil {
		logger = slog.Default()
	}

	s := &MCPServer{
		engine:   engine,
		feedback: fb,
		logger:   logger,
	}

	s.server = server.NewMCPServer(
		"knowledge-broker",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	s.server.AddTool(mcp.NewTool("query",
		mcp.WithDescription("Ask a question and get an answer synthesised from the knowledge base, with confidence signals and source citations."),
		mcp.WithString("question",
			mcp.Description("The question to ask"),
			mcp.Required(),
		),
		mcp.WithString("topics",
			mcp.Description("Optional comma-separated topics to boost relevance (e.g., 'authentication,octroi')"),
		),
	), s.handleQuery)

	s.server.AddTool(mcp.NewTool("feedback",
		mcp.WithDescription("Submit feedback on a knowledge fragment. Use 'correction' to fix wrong info, 'challenge' to flag uncertainty, or 'confirmation' to validate accuracy."),
		mcp.WithString("fragment_id",
			mcp.Description("The ID of the fragment to give feedback on"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Feedback type: correction, challenge, or confirmation"),
			mcp.Required(),
		),
		mcp.WithString("content",
			mcp.Description("The correction content (required for corrections)"),
		),
		mcp.WithString("evidence",
			mcp.Description("Optional supporting evidence"),
		),
	), s.handleFeedback)

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

	req := model.QueryRequest{
		Messages: []model.Message{
			{Role: "user", Content: question},
		},
		Limit:   20,
		Concise: true,
		Topics:  topics,
	}

	// For MCP, we don't stream — just collect the full response.
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

func (s *MCPServer) handleFeedback(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()

	fragmentID, _ := args["fragment_id"].(string)
	fbType, _ := args["type"].(string)
	content, _ := args["content"].(string)
	evidence, _ := args["evidence"].(string)

	fb := model.Feedback{
		FragmentID: fragmentID,
		Type:       model.FeedbackType(fbType),
		Content:    content,
		Evidence:   evidence,
	}

	if err := s.feedback.Submit(ctx, fb); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText("Feedback recorded successfully."), nil
}
