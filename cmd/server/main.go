package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/rafaribe/beagrid/internal/server"
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
		fmt.Printf("beagrid-server %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gid := *gridID
	if gid == "" {
		gid = fmt.Sprintf("bg-%s-%s", *gridName, "local")
	}

	registry := server.NewRegistry(gid, *gridName, *nodeTTL)
	handler := server.NewHandler(registry, logger)

	mux := http.NewServeMux()

	// Root serves grid info (like autonomous-grid)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		handler.RegisterRoutes(mux)
	})

	handler.RegisterRoutes(mux)

	// Serve embedded web UI at /ui/
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to load web assets", "err", err)
		os.Exit(1)
	}
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(webContent))))
	mux.Handle("GET /ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))

	wrapped := corsMiddleware(mux)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	logger.Info("beagrid server starting",
		"addr", addr, "grid_id", gid, "grid_name", *gridName,
		"node_ttl", *nodeTTL, "version", version,
	)
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
