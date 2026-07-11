// Package agent implements the beagrid agent daemon and engine detection.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

// engineProbe defines how to detect an inference engine.
type engineProbe struct {
	Label string
	Port  int
	Kind  string // "ollama", "openai", "comfyui"
}

// Probes are tried in priority order (same as autonomous-grid).
var defaultProbes = []engineProbe{
	{Label: "ollama", Port: 11434, Kind: "ollama"},
	{Label: "lm-studio", Port: 1234, Kind: "openai"},
	{Label: "vllm", Port: 8000, Kind: "openai"},
	{Label: "mlx", Port: 8080, Kind: "openai"},
	{Label: "llama.cpp", Port: 8081, Kind: "openai"},
	{Label: "comfyui", Port: 8188, Kind: "comfyui"},
}

// Detector implements EngineDetector by probing well-known local ports.
type Detector struct {
	client  *http.Client
	timeout time.Duration
}

func NewDetector() *Detector {
	return &Detector{
		client:  &http.Client{Timeout: 2 * time.Second},
		timeout: 750 * time.Millisecond,
	}
}

func (d *Detector) Detect(ctx context.Context) ([]domain.DetectedEngine, error) {
	var found []domain.DetectedEngine

	for _, probe := range defaultProbes {
		switch probe.Kind {
		case "comfyui":
			if d.isComfyUIReachable(ctx, probe.Port) {
				found = append(found, domain.DetectedEngine{
					Label:       probe.Label,
					EndpointURL: fmt.Sprintf("http://127.0.0.1:%d", probe.Port),
					Models:      []string{},
					Media:       true,
				})
			}
		case "ollama":
			models := d.probeOllama(ctx, probe.Port)
			if models != nil {
				found = append(found, domain.DetectedEngine{
					Label:       probe.Label,
					EndpointURL: fmt.Sprintf("http://127.0.0.1:%d/v1", probe.Port),
					Models:      models,
					Media:       false,
				})
			}
		case "openai":
			models := d.probeOpenAI(ctx, probe.Port)
			if models != nil {
				found = append(found, domain.DetectedEngine{
					Label:       probe.Label,
					EndpointURL: fmt.Sprintf("http://127.0.0.1:%d/v1", probe.Port),
					Models:      models,
					Media:       false,
				})
			}
		}
	}
	return found, nil
}

func (d *Detector) probeOllama(ctx context.Context, port int) []string {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/tags", port)
	models := d.readJSONList(ctx, url, "models", "name")
	if models != nil {
		return models
	}
	// Fallback to OpenAI-compatible endpoint
	return d.probeOpenAI(ctx, port)
}

func (d *Detector) probeOpenAI(ctx context.Context, port int) []string {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)
	return d.readJSONList(ctx, url, "data", "id")
}

func (d *Detector) isComfyUIReachable(ctx context.Context, port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/system_stats", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (d *Detector) readJSONList(ctx context.Context, url, container, key string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}

	items, ok := payload[container]
	if !ok {
		return nil
	}

	list, ok := items.([]any)
	if !ok {
		return nil
	}

	var models []string
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := obj[key].(string); ok && v != "" {
			models = append(models, v)
		}
	}
	return models
}

// DetectLocalIP finds the machine's LAN IP.
func DetectLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}
