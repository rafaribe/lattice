package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rafaribe/beagrid/internal/proxy"
	"github.com/rafaribe/beagrid/internal/server"
)

//go:embed all:web
var webFS embed.FS

var version = "dev"

func main() {
	port := flag.Int("port", 8080, "Server listen port")
	heartbeatTimeout := flag.Duration("heartbeat-timeout", 30*time.Second, "Duration before marking a node offline")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("beagrid-server %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	registry := server.NewMemoryRegistry(*heartbeatTimeout)
	router := server.NewPriorityRouter(registry)
	ollamaProxy := proxy.NewOllamaProxy()
	handler := server.NewHandler(registry, router, ollamaProxy, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Serve embedded web UI
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to load web assets", "err", err)
		os.Exit(1)
	}
	mux.Handle("GET /", http.FileServer(http.FS(webContent)))

	// CORS middleware for the UI
	wrapped := corsMiddleware(mux)

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("beagrid server starting", "addr", addr, "version", version)
	if err := http.ListenAndServe(addr, wrapped); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
