package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func chatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Interactive multi-turn query session",
		Long:  "Start a REPL-style conversational session with the knowledge base. Each turn sends the full conversation history to the query engine for context-aware answers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			debugMode := isDebug(cmd)
			llmFlag, _ := cmd.Flags().GetString("llm")
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = cfg.DefaultLimit
			}

			topicsRaw, _ := cmd.Flags().GetString("topics")
			var topics []string
			if topicsRaw != "" {
				for _, t := range strings.Split(topicsRaw, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						topics = append(topics, t)
					}
				}
			}

			sources, _ := cmd.Flags().GetStringArray("source")
			sourceTypes, _ := cmd.Flags().GetStringArray("source-type")
			noExpand, _ := cmd.Flags().GetBool("no-expand")

			remote, _ := cmd.Flags().GetString("remote")

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				return chatREPLRemote(ctx, remote, limit, topics, sources, sourceTypes, noExpand)
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			llmClient := newLLMClient(cfg, llmFlag, client, logger)
			if llmClient == nil {
				return fmt.Errorf("chat mode requires an LLM provider. Set ANTHROPIC_API_KEY in .env, or use --llm to select a provider")
			}

			engine := query.NewEngine(s, emb, llmClient, limit, logger)
			engine.SetDiskCache(s)

			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
				return err
			}

			return chatREPL(ctx, engine, limit, topics, sources, sourceTypes, noExpand)
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().Int("limit", 0, "Max fragments to retrieve (default from KB_DEFAULT_LIMIT)")
	cmd.Flags().String("topics", "", "Comma-separated topics to boost relevance (e.g., 'authentication,deployment')")
	cmd.Flags().StringArray("source", nil, "Filter results to this source name (repeatable, e.g., --source owner/repo)")
	cmd.Flags().StringArray("source-type", nil, "Filter results to this source type (repeatable: filesystem, git, confluence, slack, github_wiki)")
	cmd.Flags().String("llm", "", "LLM provider override: claude, openai, ollama (default from KB_LLM_PROVIDER or claude)")
	cmd.Flags().Bool("no-expand", false, "Disable multi-query expansion (useful for precise queries)")
	cmd.Flags().String("remote", "", "URL of a remote KB server")
	return cmd
}

// chatREPL runs the interactive conversation loop using a local query engine.
func chatREPL(ctx context.Context, engine *query.Engine, limit int, topics, sources, sourceTypes []string, noExpand bool) error {
	var messages []model.Message
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Fprintln(os.Stderr, "Knowledge Broker — interactive chat (type 'exit' or 'quit' to end)")
	for {
		fmt.Fprint(os.Stderr, "kb> ")

		if !scanner.Scan() {
			// EOF or scanner error — exit cleanly.
			fmt.Fprintln(os.Stderr)
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		messages = append(messages, model.Message{Role: model.RoleUser, Content: line})

		req := model.QueryRequest{
			Messages:    messages,
			Limit:       limit,
			Concise:     false,
			Topics:      topics,
			Sources:     sources,
			SourceTypes: sourceTypes,
			NoExpand:    noExpand,
		}

		answer, err := engine.Query(ctx, req, func(text string) {
			fmt.Print(text)
		})
		if err != nil {
			// Print error but keep the session alive; remove the failed user message.
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
			messages = messages[:len(messages)-1]
			continue
		}
		fmt.Println()

		// Print confidence to stderr.
		fmt.Fprintf(os.Stderr, "\n--- Confidence: %.2f ---\n\n", answer.Confidence.Overall)

		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: answer.Content})
	}
}

// chatREPLRemote runs the interactive conversation loop against a remote KB server.
func chatREPLRemote(ctx context.Context, remote string, limit int, topics, sources, sourceTypes []string, noExpand bool) error {
	var messages []model.Message
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Fprintln(os.Stderr, "Knowledge Broker — interactive chat (remote: "+remote+") (type 'exit' or 'quit' to end)")
	for {
		fmt.Fprint(os.Stderr, "kb> ")

		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr)
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		messages = append(messages, model.Message{Role: model.RoleUser, Content: line})

		req := model.QueryRequest{
			Messages:    messages,
			Limit:       limit,
			Concise:     false,
			Topics:      topics,
			Sources:     sources,
			SourceTypes: sourceTypes,
			NoExpand:    noExpand,
			Stream:      boolPtr(true),
		}

		answer, err := chatRemoteStream(ctx, remote, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
			messages = messages[:len(messages)-1]
			continue
		}
		fmt.Println()

		if answer.Confidence.Overall > 0 {
			fmt.Fprintf(os.Stderr, "\n--- Confidence: %.2f ---\n\n", answer.Confidence.Overall)
		}

		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: answer.Content})
	}
}

// chatRemoteStream sends a streaming query to a remote server and prints text as it arrives.
// Returns the final answer for appending to conversation history.
func chatRemoteStream(ctx context.Context, remote string, req model.QueryRequest) (*model.Answer, error) {
	resp, err := remoteRequest(ctx, "POST", remote+"/v1/query", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote returned %d: %s", resp.StatusCode, string(body))
	}

	answer := &model.Answer{}
	scanner := bufio.NewScanner(resp.Body)
	var contentBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Parse SSE events — same format as queryRemote in cmd_query.go.
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		switch event["type"] {
		case "text":
			if content, ok := event["content"].(string); ok {
				fmt.Print(content)
				contentBuf.WriteString(content)
			}
		case "done":
			answer.Content = contentBuf.String()
			if conf, ok := event["confidence"].(map[string]interface{}); ok {
				if overall, ok := conf["overall"].(float64); ok {
					answer.Confidence.Overall = overall
				}
			}
		case "error":
			if content, ok := event["content"].(string); ok {
				return nil, fmt.Errorf("remote error: %s", content)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if answer.Content == "" {
		answer.Content = contentBuf.String()
	}

	return answer, nil
}

func boolPtr(v bool) *bool { return &v }
