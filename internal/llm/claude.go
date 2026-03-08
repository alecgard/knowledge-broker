package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

const defaultModel = "claude-sonnet-4-20250514"
const defaultMaxTokens = 4096

// ClaudeClient implements the query.LLM interface using the Anthropic API.
type ClaudeClient struct {
	client anthropic.Client
	model  string
}

// NewClaudeClient creates a new Claude API client.
// If apiKey is empty, the SDK reads ANTHROPIC_API_KEY from the environment.
// If model is empty, it defaults to claude-sonnet-4-20250514.
// If httpClient is provided, it is used for all API requests (useful for debug logging).
func NewClaudeClient(apiKey string, model string, httpClient ...*http.Client) *ClaudeClient {
	if model == "" {
		model = defaultModel
	}

	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if len(httpClient) > 0 && httpClient[0] != nil {
		opts = append(opts, option.WithHTTPClient(httpClient[0]))
	}

	client := anthropic.NewClient(opts...)

	return &ClaudeClient{
		client: client,
		model:  model,
	}
}

// StreamAnswer sends messages to the Claude API using streaming and returns the
// full response text. The onText callback is invoked with each text delta as it
// arrives, enabling real-time streaming to the user.
func (c *ClaudeClient) StreamAnswer(ctx context.Context, systemPrompt string, messages []model.Message, onText func(string)) (string, error) {
	anthropicMsgs := convertMessages(messages)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: defaultMaxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: anthropicMsgs,
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var fullText strings.Builder

	for stream.Next() {
		event := stream.Current()

		if event.Type == "content_block_delta" {
			delta := event.Delta
			if delta.Type == "text_delta" && delta.Text != "" {
				fullText.WriteString(delta.Text)
				if onText != nil {
					onText(delta.Text)
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fullText.String(), fmt.Errorf("claude stream error: %w", err)
	}

	return fullText.String(), nil
}

// convertMessages transforms model.Message slice to anthropic.MessageParam slice.
func convertMessages(messages []model.Message) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			result = append(result, anthropic.NewUserMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		case "assistant":
			result = append(result, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		}
	}
	return result
}
