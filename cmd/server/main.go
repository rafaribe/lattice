package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	// Adapters (wired here in the composition root)
	httpAdapter "github.com/rafaribe/beagrid/internal/adapters/inbound/http"
	authAdapter "github.com/rafaribe/beagrid/internal/adapters/inbound/http/auth"
	"github.com/rafaribe/beagrid/internal/adapters/outbound/engine"
	"github.com/rafaribe/beagrid/internal/adapters/outbound/metrics"
	oidcAdapter "github.com/rafaribe/beagrid/internal/adapters/outbound/oidc"
	"github.com/rafaribe/beagrid/internal/adapters/outbound/registry"
	sqliteAdapter "github.com/rafaribe/beagrid/internal/adapters/outbound/sqlite"
	"github.com/rafaribe/beagrid/internal/auth"
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

	// Auth flags
	authEnabled := flag.Bool("auth-enabled", false, "Enable authentication (SQLite + optional OIDC)")
	dbPath := flag.String("db-path", "beagrid.db", "Path to SQLite database file")
	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer URL (e.g., https://idm.example.com/oauth2/openid/beagrid)")
	oidcClientID := flag.String("oidc-client-id", "", "OIDC client ID")
	oidcClientSecret := flag.String("oidc-client-secret", "", "OIDC client secret")
	oidcRedirectURL := flag.String("oidc-redirect-url", "", "OIDC callback URL (e.g., http://localhost:8090/auth/oidc/callback)")

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

	// --- Composition Root ---

	// Metrics (OTEL → Prometheus)
	m, metricsHandler, err := metrics.New()
	if err != nil {
		logger.Error("failed to initialize metrics", "err", err)
		os.Exit(1)
	}

	// Outbound adapters
	reg := registry.New(gid, *gridName, *nodeTTL)
	proxy := engine.NewProxy(logger)

	// Inbound adapter (HTTP handler)
	handler := httpAdapter.NewHandler(reg, proxy, m, logger, version)

	// --- HTTP Mux ---
	mux := http.NewServeMux()

	// API routes
	handler.RegisterRoutes(mux)

	// Metrics endpoint
	mux.Handle("GET /metrics", metricsHandler)

	// --- Auth (optional) ---
	var authHandler *authAdapter.Handler
	if *authEnabled {
		logger.Info("authentication enabled", "db", *dbPath)

		db, err := sqliteAdapter.New(*dbPath)
		if err != nil {
			logger.Error("failed to open database", "err", err)
			os.Exit(1)
		}
		defer db.Close()

		// OIDC provider (optional — only if issuer configured)
		var oidcProvider auth.OIDCProvider
		if *oidcIssuer != "" {
			redirectURL := *oidcRedirectURL
			if redirectURL == "" {
				redirectURL = fmt.Sprintf("http://localhost:%d/auth/oidc/callback", *port)
			}
			cfg := auth.OIDCConfig{
				Issuer:       *oidcIssuer,
				ClientID:     *oidcClientID,
				ClientSecret: *oidcClientSecret,
				RedirectURL:  redirectURL,
			}
			p, err := oidcAdapter.New(cfg)
			if err != nil {
				logger.Warn("OIDC provider unavailable (will use local auth only)", "err", err)
			} else {
				oidcProvider = p
				logger.Info("OIDC provider configured", "issuer", *oidcIssuer)
			}
		}

		authHandler = authAdapter.NewHandler(db.Users(), db.Sessions(), oidcProvider, logger)
		authHandler.RegisterRoutes(mux)
		logger.Info("auth routes registered: /auth/register, /auth/login, /auth/logout, /auth/me")
		if oidcProvider != nil {
			logger.Info("OIDC routes registered: /auth/oidc/login, /auth/oidc/callback")
		}
	}

	// Serve embedded web UI at / (catch-all)
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to load web assets", "err", err)
		os.Exit(1)
	}
	mux.Handle("GET /", http.FileServer(http.FS(webContent)))

	// Middleware chain: CORS → Metrics → Request ID → Mux
	wrapped := corsMiddleware(handler.MetricsMiddleware(httpAdapter.RequestIDMiddleware(mux)))

	addr := fmt.Sprintf("%s:%d", *host, *port)
	logger.Info("beagrid server starting",
		"addr", addr, "grid_id", gid, "grid_name", *gridName,
		"node_ttl", *nodeTTL, "version", version,
		"auth_enabled", *authEnabled,
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
