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

const defaultOllamaLLMURL = "http://localhost:11434"
const defaultOllamaLLMModel = "llama3.2"

// OllamaLLMClient implements the query.LLM interface using the Ollama chat API.
type OllamaLLMClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewOllamaLLMClient creates a new Ollama LLM client.
// If baseURL is empty, it defaults to http://localhost:11434.
// If model is empty, it defaults to llama3.2.
// If httpClient is nil, http.DefaultClient is used.
func NewOllamaLLMClient(baseURL, model string, httpClient *http.Client) *OllamaLLMClient {
	if baseURL == "" {
		baseURL = defaultOllamaLLMURL
	}
	if model == "" {
		model = defaultOllamaLLMModel
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OllamaLLMClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		httpClient: httpClient,
	}
}

// ollamaChatRequest is the JSON body for the Ollama chat API.
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatChunk represents a single NDJSON line from the streaming Ollama chat API.
type ollamaChatChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// StreamAnswer sends messages to the Ollama chat API using streaming and returns
// the full response text. The onText callback is invoked with each text delta.
func (c *OllamaLLMClient) StreamAnswer(ctx context.Context, systemPrompt string, messages []model.Message, onText func(string)) (string, error) {
	ollamaMessages := convertToOllama(systemPrompt, messages)

	reqBody := ollamaChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Stream:   true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(body))
	}

	return parseOllamaNDJSON(resp.Body, onText)
}

// parseOllamaNDJSON reads NDJSON lines from the Ollama streaming response,
// extracts content from each chunk, calls onText, and returns the full text.
func parseOllamaNDJSON(r io.Reader, onText func(string)) (string, error) {
	var fullText strings.Builder
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk ollamaChatChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue // skip malformed lines
		}

		if chunk.Message.Content != "" {
			fullText.WriteString(chunk.Message.Content)
			if onText != nil {
				onText(chunk.Message.Content)
			}
		}

		if chunk.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fullText.String(), fmt.Errorf("ollama stream read: %w", err)
	}

	return fullText.String(), nil
}

// convertToOllama transforms the system prompt and model messages into
// Ollama chat format messages.
func convertToOllama(systemPrompt string, messages []model.Message) []ollamaChatMessage {
	result := make([]ollamaChatMessage, 0, len(messages)+1)
	result = append(result, ollamaChatMessage{Role: "system", Content: systemPrompt})
	for _, msg := range messages {
		switch msg.Role {
		case model.RoleUser:
			result = append(result, ollamaChatMessage{Role: "user", Content: msg.Content})
		case model.RoleAssistant:
			result = append(result, ollamaChatMessage{Role: "assistant", Content: msg.Content})
		}
	}
	return result
}
