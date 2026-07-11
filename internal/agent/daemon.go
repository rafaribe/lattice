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

	"github.com/google/uuid"
	"github.com/rafaribe/beagrid/internal/domain"
)

// Daemon is the agent that registers engines with the beagrid server.
type Daemon struct {
	serverURL      string
	ollamaURL      string
	name           string
	nodeID         string
	interval       time.Duration
	advertiseHost  string
	models         []string   // explicit model list (or discovered)
	advertiseAs    []string   // aliases
	endpointURL    string     // explicit endpoint (or auto)
	autoDetect     bool       // detect all local engines
	detector       *Detector
	httpClient     *http.Client
	logger         *slog.Logger
}

// DaemonConfig holds all agent configuration.
type DaemonConfig struct {
	ServerURL     string
	OllamaURL     string
	Name          string
	EndpointURL   string
	Models        []string
	AdvertiseAs   []string
	AdvertiseHost string
	AutoDetect    bool
	Interval      time.Duration
}

func NewDaemon(cfg DaemonConfig, logger *slog.Logger) *Daemon {
	name := cfg.Name
	if name == "" {
		name, _ = os.Hostname()
	}
	interval := cfg.Interval
	if interval == 0 {
		interval = 15 * time.Second
	}

	return &Daemon{
		serverURL:     strings.TrimRight(cfg.ServerURL, "/"),
		ollamaURL:     cfg.OllamaURL,
		name:          name,
		nodeID:        "node-" + uuid.New().String()[:12],
		interval:      interval,
		advertiseHost: cfg.AdvertiseHost,
		models:        cfg.Models,
		advertiseAs:   cfg.AdvertiseAs,
		endpointURL:   cfg.EndpointURL,
		autoDetect:    cfg.AutoDetect,
		detector:      NewDetector(),
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		logger:        logger,
	}
}

// Run starts the agent: detect engines, register, heartbeat loop.
func (d *Daemon) Run(ctx context.Context) error {
	var engines []domain.DetectedEngine

	if d.autoDetect {
		d.logger.Info("detecting local engines...")
		detected, err := d.detector.Detect(ctx)
		if err != nil {
			return fmt.Errorf("detection failed: %w", err)
		}
		engines = detected
		d.logger.Info("detected engines", "count", len(engines))
	} else if d.endpointURL != "" {
		// Explicit external engine
		models := d.models
		if len(models) == 0 {
			// Try to discover from the endpoint
			openaiModels := d.detector.probeOpenAI(ctx, 0)
			if openaiModels == nil && d.ollamaURL != "" {
				// Try Ollama tags
				adapter := NewOllamaAdapter(d.ollamaURL)
				ollamaModels, _ := adapter.ListModels(ctx)
				for _, m := range ollamaModels {
					models = append(models, m.Name)
				}
			} else {
				models = openaiModels
			}
		}
		engines = []domain.DetectedEngine{{
			Label:       "external",
			EndpointURL: d.endpointURL,
			Models:      models,
		}}
	} else {
		// Default: probe Ollama at the configured URL
		d.logger.Info("probing ollama", "url", d.ollamaURL)
		if err := d.waitForEndpoint(ctx, d.ollamaURL); err != nil {
			return err
		}
		adapter := NewOllamaAdapter(d.ollamaURL)
		ollamaModels, err := adapter.ListModels(ctx)
		if err != nil {
			return fmt.Errorf("listing ollama models: %w", err)
		}
		models := make([]string, 0, len(ollamaModels))
		for _, m := range ollamaModels {
			models = append(models, m.Name)
		}

		endpoint := d.ollamaURL
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint = strings.TrimRight(endpoint, "/") + "/v1"
		}
		engines = []domain.DetectedEngine{{
			Label:       "ollama",
			EndpointURL: endpoint,
			Models:      models,
		}}
	}

	if len(engines) == 0 {
		return fmt.Errorf("no engines found to advertise")
	}

	// Aggregate all models and build upstream map
	allModels := []string{}
	upstream := map[string]string{}
	var primaryEndpoint string

	for _, engine := range engines {
		if engine.Media {
			// Media engines use their own endpoint
			continue
		}
		if primaryEndpoint == "" {
			primaryEndpoint = engine.EndpointURL
		}
		for i, m := range engine.Models {
			advertised := m
			if i < len(d.advertiseAs) {
				advertised = d.advertiseAs[i]
				upstream[advertised] = m
			} else {
				upstream[m] = m
			}
			allModels = append(allModels, advertised)
		}
	}

	if len(allModels) == 0 {
		return fmt.Errorf("no models to advertise")
	}

	// Register with PUT /nodes/{node_id}
	payload := domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      allModels,
		EndpointURL: primaryEndpoint,
		Load:        domain.Load{ActiveTasks: 0},
		Name:        d.name,
		Upstream:    upstream,
	}

	if err := d.register(ctx, payload); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	d.logger.Info("registered with grid",
		"node_id", d.nodeID,
		"server", d.serverURL,
		"models", allModels,
		"endpoint", primaryEndpoint,
	)

	// Heartbeat loop
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.deregister()
			return nil
		case <-ticker.C:
			if err := d.heartbeat(ctx, payload); err != nil {
				d.logger.Warn("heartbeat failed", "err", err)
			}
		}
	}
}

func (d *Daemon) waitForEndpoint(ctx context.Context, url string) error {
	for i := 0; i < 30; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := d.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("endpoint %s not available after 60s", url)
}

func (d *Daemon) register(ctx context.Context, payload domain.NodeUpdateRequest) error {
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/nodes/%s", d.serverURL, d.nodeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to server %s: %w", d.serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("registration returned %d", resp.StatusCode)
	}
	return nil
}

func (d *Daemon) heartbeat(ctx context.Context, registrationPayload domain.NodeUpdateRequest) error {
	hb := domain.HeartbeatRequest{
		NodeID: d.nodeID,
		Load:   domain.Load{ActiveTasks: 0},
	}
	body, _ := json.Marshal(hb)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.serverURL+"/nodes/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		d.logger.Warn("node lost on server, re-registering")
		return d.register(ctx, registrationPayload)
	}
	return nil
}

func (d *Daemon) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/nodes/%s", d.serverURL, d.nodeID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	resp, err := d.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	d.logger.Info("unregistered from grid")
}
