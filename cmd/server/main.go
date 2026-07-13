package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	httpAdapter "github.com/rafaribe/lattice/internal/adapters/inbound/http"
	"github.com/rafaribe/lattice/internal/adapters/outbound/engine"
	"github.com/rafaribe/lattice/internal/adapters/outbound/metrics"
	"github.com/rafaribe/lattice/internal/adapters/outbound/registry"
)

//go:embed all:web
var webFS embed.FS

var version = "dev"

func main() {
	port := flag.Int("port", 8090, "Server listen port")
	host := flag.String("host", "0.0.0.0", "Server listen host")
	gridName := flag.String("name", "home", "Grid name")
	gridID := flag.String("grid-id", "", "Grid ID (auto-generated if empty)")
	nodeTTL := flag.Int("node-ttl", 60, "Seconds before a node is considered stale")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("lattice-server %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gid := *gridID
	if gid == "" {
		gid = fmt.Sprintf("bg-%s-%s", *gridName, "local")
	}

	// --- Composition Root ---
	m, metricsHandler, err := metrics.New()
	if err != nil {
		logger.Error("failed to initialize metrics", "err", err)
		os.Exit(1)
	}

	reg := registry.New(gid, *gridName, *nodeTTL)
	proxy := engine.NewProxy(logger)
	handler := httpAdapter.NewHandler(reg, proxy, m, logger, version)

	// --- HTTP Mux ---
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	mux.Handle("GET /metrics", metricsHandler)

	// Serve embedded web UI at / (catch-all)
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to load web assets", "err", err)
		os.Exit(1)
	}
	mux.Handle("GET /", http.FileServer(http.FS(webContent)))

	// Middleware: CORS → Metrics → Request ID
	wrapped := corsMiddleware(handler.MetricsMiddleware(httpAdapter.RequestIDMiddleware(mux)))

	addr := fmt.Sprintf("%s:%d", *host, *port)
	logger.Info("lattice server starting", "addr", addr, "grid_id", gid, "name", *gridName, "node_ttl", *nodeTTL, "version", version)
	if err := http.ListenAndServe(addr, wrapped); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
