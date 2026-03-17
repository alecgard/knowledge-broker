package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func queryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [text]",
		Short: "Query the knowledge base",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			debugMode := isDebug(cmd)
			human, _ := cmd.Flags().GetBool("human")
			rawMode, _ := cmd.Flags().GetBool("raw")
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

			question := strings.Join(args, " ")
			req := model.QueryRequest{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: question},
				},
				Limit:       limit,
				Concise:     !human,
				Topics:      topics,
				Sources:     sources,
				SourceTypes: sourceTypes,
				NoExpand:    noExpand,
			}

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				return queryRemote(context.Background(), remote, req, rawMode, human)
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			// When --raw is set, LLM is not needed.
			var llmClient query.LLM
			if !rawMode {
				llmClient = newLLMClient(cfg, llmFlag, client, logger)
				if llmClient == nil {
					return fmt.Errorf("synthesis mode requires ANTHROPIC_API_KEY. Set it in .env, or use --raw for retrieval without LLM")
				}
			}

			engine := query.NewEngine(s, emb, llmClient, limit, logger)
			engine.SetDiskCache(s)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
				return err
			}

			if rawMode {
				return queryRaw(ctx, engine, req)
			}
			if human {
				return queryHuman(ctx, engine, req)
			}
			return queryCompact(ctx, engine, req)
		},
	}
	cmd.Flags().String("db", "", "Path to SQLite database (default: ~/.local/share/kb/kb.db)")
	cmd.Flags().Int("limit", 0, "Max fragments to retrieve (default from KB_DEFAULT_LIMIT)")
	cmd.Flags().Bool("human", false, "Human-readable output (streamed text + formatted metadata)")
	cmd.Flags().Bool("raw", false, "Raw retrieval mode: return fragments as JSON without LLM synthesis (no API key needed)")
	cmd.Flags().String("topics", "", "Comma-separated topics to boost relevance (e.g., 'authentication,deployment')")
	cmd.Flags().StringArray("source", nil, "Filter results to this source name (repeatable, e.g., --source owner/repo)")
	cmd.Flags().StringArray("source-type", nil, "Filter results to this source type (repeatable: filesystem, git, confluence, slack, github_wiki)")
	cmd.Flags().String("llm", "", "LLM provider override: claude, openai, ollama (default from KB_LLM_PROVIDER or claude)")
	cmd.Flags().Bool("no-expand", false, "Disable multi-query expansion (useful for precise queries)")
	cmd.Flags().String("remote", "", "URL of a remote KB server")
	return cmd
}

// queryCompact outputs a single JSON object — optimised for AI consumption.
func queryCompact(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	answer, err := engine.Query(ctx, req, nil)
	if err != nil {
		return err
	}

	out, _ := json.Marshal(answer)
	fmt.Println(string(out))
	return nil
}

// queryHuman streams the answer text and prints formatted metadata — for humans.
func queryHuman(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	answer, err := engine.Query(ctx, req, func(text string) {
		fmt.Print(text)
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Fprintf(os.Stderr, "\n--- Confidence ---\n")
	fmt.Fprintf(os.Stderr, "Overall:       %.2f\n", answer.Confidence.Overall)
	fmt.Fprintf(os.Stderr, "Freshness:     %.2f\n", answer.Confidence.Breakdown.Freshness)
	fmt.Fprintf(os.Stderr, "Corroboration: %.2f\n", answer.Confidence.Breakdown.Corroboration)
	fmt.Fprintf(os.Stderr, "Consistency:   %.2f\n", answer.Confidence.Breakdown.Consistency)
	fmt.Fprintf(os.Stderr, "Authority:     %.2f\n", answer.Confidence.Breakdown.Authority)

	if len(answer.Sources) > 0 {
		fmt.Fprintf(os.Stderr, "\n--- Sources ---\n")
		for _, src := range answer.Sources {
			fmt.Fprintf(os.Stderr, "  [%s] %s\n", src.FragmentID, src.SourcePath)
		}
	}

	if len(answer.Contradictions) > 0 {
		fmt.Fprintf(os.Stderr, "\n--- Contradictions ---\n")
		for _, c := range answer.Contradictions {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", c.Claim, c.Explanation)
		}
	}

	return nil
}

// queryRaw outputs raw retrieval results as JSON without LLM synthesis.
func queryRaw(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	result, err := engine.QueryRaw(ctx, req)
	if err != nil {
		return err
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	return nil
}

// queryRemote sends a query to a remote KB server and prints the response.
func queryRemote(ctx context.Context, remote string, req model.QueryRequest, rawMode, human bool) error {
	if rawMode {
		req.Mode = model.ModeRaw
		var result model.RawResult
		if err := remoteJSON(ctx, http.MethodPost, remote+"/v1/query", req, &result); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	if human {
		// Use streaming (SSE) for human mode.
		streamTrue := true
		req.Stream = &streamTrue
		resp, err := remoteRequest(ctx, http.MethodPost, remote+"/v1/query", req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("remote returned %d: %s", resp.StatusCode, string(body))
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event map[string]interface{}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			switch event["type"] {
			case "text":
				if content, ok := event["content"].(string); ok {
					fmt.Print(content)
				}
			case "done":
				fmt.Println()
				if conf, ok := event["confidence"].(map[string]interface{}); ok {
					fmt.Fprintf(os.Stderr, "\n--- Confidence ---\n")
					if overall, ok := conf["overall"].(float64); ok {
						fmt.Fprintf(os.Stderr, "Overall:       %.2f\n", overall)
					}
					if bd, ok := conf["breakdown"].(map[string]interface{}); ok {
						if v, ok := bd["freshness"].(float64); ok {
							fmt.Fprintf(os.Stderr, "Freshness:     %.2f\n", v)
						}
						if v, ok := bd["corroboration"].(float64); ok {
							fmt.Fprintf(os.Stderr, "Corroboration: %.2f\n", v)
						}
						if v, ok := bd["consistency"].(float64); ok {
							fmt.Fprintf(os.Stderr, "Consistency:   %.2f\n", v)
						}
						if v, ok := bd["authority"].(float64); ok {
							fmt.Fprintf(os.Stderr, "Authority:     %.2f\n", v)
						}
					}
				}
				if srcs, ok := event["sources"].([]interface{}); ok && len(srcs) > 0 {
					fmt.Fprintf(os.Stderr, "\n--- Sources ---\n")
					for _, s := range srcs {
						if sm, ok := s.(map[string]interface{}); ok {
							fid, _ := sm["fragment_id"].(string)
							sp, _ := sm["source_path"].(string)
							fmt.Fprintf(os.Stderr, "  [%s] %s\n", fid, sp)
						}
					}
				}
			case "error":
				if content, ok := event["content"].(string); ok {
					return fmt.Errorf("remote error: %s", content)
				}
			}
		}
		return scanner.Err()
	}

	// Compact mode (default).
	var answer model.Answer
	if err := remoteJSON(ctx, http.MethodPost, remote+"/v1/query", req, &answer); err != nil {
		return err
	}
	out, _ := json.Marshal(answer)
	fmt.Println(string(out))
	return nil
}
