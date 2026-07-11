package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rafaribe/beagrid/internal/agent"
)

var version = "dev"

func main() {
	serverURL := flag.String("server", "http://localhost:8090", "Beagrid server URL")
	ollamaURL := flag.String("ollama", "http://localhost:11434", "Ollama instance URL")
	name := flag.String("name", "", "Engine name (defaults to hostname)")
	endpointURL := flag.String("at", "", "URL of an existing OpenAI-compatible engine")
	detectAll := flag.Bool("all", false, "Detect and join all local engines")
	interval := flag.Float64("heartbeat-interval", 15.0, "Heartbeat interval in seconds")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("beagrid-agent %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("beagrid agent starting", "server", *serverURL, "version", version)

	cfg := agent.DaemonConfig{
		ServerURL:   *serverURL,
		OllamaURL:   *ollamaURL,
		Name:        *name,
		EndpointURL: *endpointURL,
		AutoDetect:  *detectAll,
		Interval:    time.Duration(*interval * float64(time.Second)),
	}

	daemon := agent.NewDaemon(cfg, logger)

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
