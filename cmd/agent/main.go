package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rafaribe/beagrid/internal/agent"
)

var version = "dev"

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "Beagrid server URL")
	ollamaURL := flag.String("ollama", "http://localhost:11434", "Ollama instance URL")
	name := flag.String("name", "", "Node name (defaults to hostname)")
	priority := flag.Int("priority", 10, "Node priority (lower = higher priority)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("beagrid-agent %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("beagrid agent starting", "server", *serverURL, "ollama", *ollamaURL, "version", version)

	daemon := agent.NewDaemon(*serverURL, *ollamaURL, *name, *priority, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutting down agent")
		cancel()
	}()

	if err := daemon.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("agent failed", "err", err)
		os.Exit(1)
	}
}
