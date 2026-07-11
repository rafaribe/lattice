package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

// Daemon is the agent that registers with the beagrid server and sends heartbeats.
type Daemon struct {
	serverURL  string
	ollamaURL  string
	name       string
	priority   int
	nodeID     string
	interval   time.Duration
	ollama     *OllamaAdapter
	httpClient *http.Client
	logger     *slog.Logger
}

func NewDaemon(serverURL, ollamaURL, name string, priority int, logger *slog.Logger) *Daemon {
	if name == "" {
		name, _ = os.Hostname()
	}
	return &Daemon{
		serverURL:  strings.TrimRight(serverURL, "/"),
		ollamaURL:  ollamaURL,
		name:       name,
		priority:   priority,
		interval:   10 * time.Second,
		ollama:     NewOllamaAdapter(ollamaURL),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// Run starts the agent: register then loop heartbeats.
func (d *Daemon) Run(ctx context.Context) error {
	// Wait for Ollama to become available
	d.logger.Info("waiting for ollama", "url", d.ollamaURL)
	if err := d.waitForOllama(ctx); err != nil {
		return err
	}

	// Discover models
	models, err := d.ollama.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("discovering models: %w", err)
	}
	d.logger.Info("discovered models", "count", len(models))

	// Register with server
	if err := d.register(ctx, models); err != nil {
		return fmt.Errorf("registering with server: %w", err)
	}
	d.logger.Info("registered with grid", "node_id", d.nodeID, "server", d.serverURL)

	// Heartbeat loop
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.deregister()
			return ctx.Err()
		case <-ticker.C:
			if err := d.heartbeat(ctx); err != nil {
				d.logger.Warn("heartbeat failed", "err", err)
			}
		}
	}
}

func (d *Daemon) waitForOllama(ctx context.Context) error {
	for i := 0; i < 30; i++ {
		if d.ollama.IsHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("ollama not available at %s after 60s", d.ollamaURL)
}

func (d *Daemon) register(ctx context.Context, models []domain.Model) error {
	req := domain.RegisterRequest{
		Name:     d.name,
		Address:  d.ollamaURL,
		Models:   models,
		Priority: d.priority,
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.serverURL+"/api/v1/nodes/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("connecting to server %s: %w", d.serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	var regResp domain.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return err
	}

	d.nodeID = regResp.NodeID
	if regResp.HeartbeatInterval > 0 {
		d.interval = regResp.HeartbeatInterval
	}
	return nil
}

func (d *Daemon) heartbeat(ctx context.Context) error {
	models, _ := d.ollama.ListModels(ctx)

	req := domain.HeartbeatRequest{
		NodeID: d.nodeID,
		Models: models,
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.serverURL+"/api/v1/nodes/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Re-register if server lost us
		d.logger.Warn("node not found on server, re-registering")
		models, _ := d.ollama.ListModels(ctx)
		return d.register(ctx, models)
	}

	return nil
}

func (d *Daemon) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/nodes/%s", d.serverURL, d.nodeID), nil)
	resp, err := d.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
