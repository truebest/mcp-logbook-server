package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/truebest/mcp-logbook-server/pkg/config"
	"github.com/truebest/mcp-logbook-server/pkg/ingestion"
	"github.com/truebest/mcp-logbook-server/pkg/mcp"
	"github.com/truebest/mcp-logbook-server/pkg/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	store, err := storage.NewSQLiteStorage(cfg.Storage.ConnectionString)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	httpServer := ingestion.NewServer(cfg.Server.IngestionPort, store)
	mcpServer := mcp.NewServer(cfg.Server.MCPPort, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := httpServer.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	go func() {
		if err := mcpServer.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("MCP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down servers...")
	cancel()
}
