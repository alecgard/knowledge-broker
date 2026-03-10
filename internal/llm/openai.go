package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

const defaultOpenAIModel = "gpt-4o"
const openAIAPIURL = "https://api.openai.com/v1/chat/completions"

// OpenAIClient implements the query.LLM interface using the OpenAI-compatible
// REST API (no SDK dependency).
type OpenAIClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAI API client.
// If model is empty, it defaults to gpt-4o.
// If httpClient is nil, http.DefaultClient is used.
func NewOpenAIClient(apiKey, model string, httpClient *http.Client) *OpenAIClient {
	if model == "" {
		model = defaultOpenAIModel
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: httpClient,
	}
}

// openAIRequest is the JSON body for the OpenAI chat completions API.
type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIStreamChunk represents a single SSE chunk from the streaming API.
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// StreamAnswer sends messages to the OpenAI API using streaming and returns the
// full response text. The onText callback is invoked with each text delta.
func (c *OpenAIClient) StreamAnswer(ctx context.Context, systemPrompt string, messages []model.Message, onText func(string)) (string, error) {
	oaiMessages := convertToOpenAI(systemPrompt, messages)

	reqBody := openAIRequest{
		Model:    c.model,
		Messages: oaiMessages,
		Stream:   true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai returned %d: %s", resp.StatusCode, string(body))
	}

	return parseOpenAISSE(resp.Body, onText)
}

// parseOpenAISSE reads SSE events from the response body, extracts content
// deltas, calls onText for each, and returns the full assembled text.
func parseOpenAISSE(r io.Reader, onText func(string)) (string, error) {
	var fullText strings.Builder
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {...}" or "data: [DONE]"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				fullText.WriteString(choice.Delta.Content)
				if onText != nil {
					onText(choice.Delta.Content)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fullText.String(), fmt.Errorf("openai stream read: %w", err)
	}

	return fullText.String(), nil
}

// convertToOpenAI transforms the system prompt and model messages into
// OpenAI-format messages.
func convertToOpenAI(systemPrompt string, messages []model.Message) []openAIMessage {
	result := make([]openAIMessage, 0, len(messages)+1)
	result = append(result, openAIMessage{Role: "system", Content: systemPrompt})
	for _, msg := range messages {
		switch msg.Role {
		case model.RoleUser:
			result = append(result, openAIMessage{Role: "user", Content: msg.Content})
		case model.RoleAssistant:
			result = append(result, openAIMessage{Role: "assistant", Content: msg.Content})
		}
	}
	return result
}
