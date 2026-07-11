package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rafaribe/beagrid/internal/application"
	"github.com/rafaribe/beagrid/internal/domain"
)

// Daemon is the agent that registers engines with the beagrid server.
// It orchestrates detection, registration, and heartbeat via injected ports.
type Daemon struct {
	serverURL    string
	ollamaURL    string
	name         string
	nodeID       string
	interval     time.Duration
	pollInterval time.Duration
	models       []string
	advertiseAs  []string
	endpointURL  string
	autoDetect   bool

	// Outbound ports (injected)
	detector   application.EngineDetector
	ollama     application.OllamaClient
	gridClient application.GridServer

	logger *slog.Logger
}

// DaemonConfig holds all agent configuration.
type DaemonConfig struct {
	ServerURL    string
	OllamaURL    string
	Name         string
	EndpointURL  string
	Models       []string
	AdvertiseAs  []string
	AutoDetect   bool
	Interval     time.Duration
	PollInterval time.Duration
}

// NewDaemon creates a new agent daemon with injected outbound adapters.
func NewDaemon(
	cfg DaemonConfig,
	detector application.EngineDetector,
	ollama application.OllamaClient,
	gridClient application.GridServer,
	logger *slog.Logger,
) *Daemon {
	name := cfg.Name
	if name == "" {
		name, _ = os.Hostname()
	}
	interval := cfg.Interval
	if interval == 0 {
		interval = 15 * time.Second
	}
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Minute
	}

	return &Daemon{
		serverURL:    strings.TrimRight(cfg.ServerURL, "/"),
		ollamaURL:    cfg.OllamaURL,
		name:         name,
		nodeID:       "node-" + uuid.New().String()[:12],
		interval:     interval,
		pollInterval: pollInterval,
		models:       cfg.Models,
		advertiseAs:  cfg.AdvertiseAs,
		endpointURL:  cfg.EndpointURL,
		autoDetect:   cfg.AutoDetect,
		detector:     detector,
		ollama:       ollama,
		gridClient:   gridClient,
		logger:       logger,
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
		if len(models) == 0 && d.ollamaURL != "" {
			ollamaModels, _ := d.ollama.ListModels(ctx)
			for _, m := range ollamaModels {
				models = append(models, m.Name)
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
		if !d.ollama.IsHealthy(ctx) {
			return fmt.Errorf("ollama at %s not reachable", d.ollamaURL)
		}
		ollamaModels, err := d.ollama.ListModels(ctx)
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

	for _, eng := range engines {
		if eng.Media {
			continue
		}
		if primaryEndpoint == "" {
			primaryEndpoint = eng.EndpointURL
		}
		for i, m := range eng.Models {
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

	// Register with the grid server
	payload := domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      allModels,
		EndpointURL: primaryEndpoint,
		Load:        domain.Load{ActiveTasks: 0},
		Name:        d.name,
		Upstream:    upstream,
	}

	if err := d.gridClient.Register(ctx, d.nodeID, payload); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	d.logger.Info("registered with grid",
		"node_id", d.nodeID,
		"server", d.serverURL,
		"models", allModels,
		"endpoint", primaryEndpoint,
	)

	// Heartbeat loop (fast) + model poll (slow)
	heartbeatTicker := time.NewTicker(d.interval)
	defer heartbeatTicker.Stop()
	pollTicker := time.NewTicker(d.pollInterval)
	defer pollTicker.Stop()

	d.logger.Info("heartbeat every", "interval", d.interval, "model_poll_every", d.pollInterval)

	for {
		select {
		case <-ctx.Done():
			d.deregister()
			return nil
		case <-heartbeatTicker.C:
			if err := d.heartbeat(ctx, payload); err != nil {
				d.logger.Warn("heartbeat failed", "err", err)
			}
		case <-pollTicker.C:
			freshModels := d.probeCurrentModels(ctx)
			if freshModels != nil && !slicesEqual(freshModels, payload.Models) {
				payload.Models = freshModels
				newUpstream := map[string]string{}
				for _, m := range freshModels {
					newUpstream[m] = m
				}
				payload.Upstream = newUpstream
				d.logger.Info("model list changed, re-registering", "models", freshModels)
				if err := d.gridClient.Register(ctx, d.nodeID, payload); err != nil {
					d.logger.Warn("re-registration failed", "err", err)
				}
			}
		}
	}
}

func (d *Daemon) heartbeat(ctx context.Context, registrationPayload domain.NodeUpdateRequest) error {
	hb := domain.HeartbeatRequest{
		NodeID: d.nodeID,
		Load:   domain.Load{ActiveTasks: 0},
	}

	err := d.gridClient.Heartbeat(ctx, hb)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			d.logger.Warn("node lost on server, re-registering")
			return d.gridClient.Register(ctx, d.nodeID, registrationPayload)
		}
		return err
	}
	return nil
}

func (d *Daemon) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.gridClient.Deregister(ctx, d.nodeID)
	d.logger.Info("unregistered from grid")
}

func (d *Daemon) probeCurrentModels(ctx context.Context) []string {
	if d.autoDetect {
		engines, err := d.detector.Detect(ctx)
		if err != nil || len(engines) == 0 {
			return nil
		}
		var all []string
		for _, e := range engines {
			if !e.Media {
				all = append(all, e.Models...)
			}
		}
		return all
	}

	if d.ollamaURL != "" {
		models, err := d.ollama.ListModels(ctx)
		if err != nil {
			return nil
		}
		names := make([]string, 0, len(models))
		for _, m := range models {
			names = append(names, m.Name)
		}
		return names
	}

	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}
