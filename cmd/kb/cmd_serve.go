package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/apps/ui"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/server"
	"github.com/knowledge-broker/knowledge-broker/internal/config"
)

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API and MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			cfg.ListenAddr, _ = cmd.Flags().GetString("addr")
			mcpAddr, _ := cmd.Flags().GetString("mcp-addr")
			noHTTP, _ := cmd.Flags().GetBool("no-http")
			noSSE, _ := cmd.Flags().GetBool("no-sse")
			noStdio, _ := cmd.Flags().GetBool("no-stdio")
			noUI, _ := cmd.Flags().GetBool("no-ui")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			if noHTTP && noSSE && noStdio {
				return fmt.Errorf("all transports disabled; nothing to serve")
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
				return err
			}

			llmClient := newLLMClient(cfg, "", client, logger)
			engine := query.NewEngine(s, emb, llmClient, cfg.DefaultLimit, logger)
			engine.SetDiskCache(s)

			mcpTransports := server.MCPTransports{
				Stdio: !noStdio,
				SSE:   !noSSE,
			}

			// Start MCP transports in the background (if any enabled).
			mcpServer := server.NewMCPServer(engine, s, logger, version)
			if mcpTransports.Stdio || mcpTransports.SSE {
				go func() {
					if err := mcpServer.Serve(ctx, mcpAddr, mcpTransports); err != nil {
						logger.Error("MCP server error", "error", err)
					}
				}()
			}

			if noHTTP {
				// No HTTP server — block until ctx is cancelled.
				<-ctx.Done()
				return nil
			}

			// Wire pipeline deps for source management UI.
			reg := newExtractorRegistry(cfg)
			jobs := server.NewJobTracker()
			pipelineCfg := server.PipelineConfig{
				OllamaURL:      cfg.OllamaURL,
				EnrichModel:    cfg.EnrichModel,
				WorkerCount:    cfg.WorkerCount,
				SkipEnrichment: true,
				MaxChunkSize:   cfg.MaxChunkSize,
				ChunkOverlap:   cfg.ChunkOverlap,
			}
			httpServer := server.NewHTTPServerWithOptions(engine, emb, s, logger, version,
				server.WithPipeline(reg, pipelineCfg, client, jobs),
			)
			if noUI {
				return httpServer.ListenAndServe(ctx, cfg.ListenAddr)
			}
			uiServer := ui.NewServer(httpServer, logger)
			return uiServer.ListenAndServe(ctx, cfg.ListenAddr)
		},
	}
	cmd.Flags().String("addr", ":8080", "HTTP listen address")
	cmd.Flags().String("mcp-addr", ":8082", "MCP SSE listen address")
	cmd.Flags().String("db", "", config.DBFlagUsage)
	cmd.Flags().Bool("no-http", false, "Disable HTTP API server")
	cmd.Flags().Bool("no-sse", false, "Disable MCP SSE transport")
	cmd.Flags().Bool("no-stdio", false, "Disable MCP stdio transport")
	cmd.Flags().Bool("no-ui", false, "Disable embedded UI (API only)")
	return cmd
}
