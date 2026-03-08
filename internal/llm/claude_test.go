package llm

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
)

func TestConvertMessages(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "Hello, what is X?"},
		{Role: "assistant", Content: "X is a thing."},
		{Role: "user", Content: "Can you elaborate?"},
	}

	result := convertMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	// Verify roles
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("message 0: expected role user, got %s", result[0].Role)
	}
	if result[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("message 1: expected role assistant, got %s", result[1].Role)
	}
	if result[2].Role != anthropic.MessageParamRoleUser {
		t.Errorf("message 2: expected role user, got %s", result[2].Role)
	}

	// Verify content blocks
	for i, msg := range result {
		if len(msg.Content) != 1 {
			t.Fatalf("message %d: expected 1 content block, got %d", i, len(msg.Content))
		}
		if msg.Content[0].OfText == nil {
			t.Fatalf("message %d: expected text block, got nil", i)
		}
	}

	if result[0].Content[0].OfText.Text != "Hello, what is X?" {
		t.Errorf("message 0: unexpected content %q", result[0].Content[0].OfText.Text)
	}
	if result[1].Content[0].OfText.Text != "X is a thing." {
		t.Errorf("message 1: unexpected content %q", result[1].Content[0].OfText.Text)
	}
	if result[2].Content[0].OfText.Text != "Can you elaborate?" {
		t.Errorf("message 2: unexpected content %q", result[2].Content[0].OfText.Text)
	}
}

func TestConvertMessages_Empty(t *testing.T) {
	result := convertMessages(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestConvertMessages_SkipsUnknownRoles(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "Hello"},
		{Role: "system", Content: "This should be skipped"},
		{Role: "assistant", Content: "Hi"},
	}

	result := convertMessages(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (system skipped), got %d", len(result))
	}
	if result[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected first message to be user, got %s", result[0].Role)
	}
	if result[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("expected second message to be assistant, got %s", result[1].Role)
	}
}

func TestNewClaudeClient_Defaults(t *testing.T) {
	client := NewClaudeClient("", "")
	if client.model != defaultModel {
		t.Errorf("expected default model %q, got %q", defaultModel, client.model)
	}
}

func TestNewClaudeClient_CustomModel(t *testing.T) {
	client := NewClaudeClient("", "claude-opus-4-5")
	if client.model != "claude-opus-4-5" {
		t.Errorf("expected model claude-opus-4-5, got %q", client.model)
	}
}
