package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rafaribe/beagrid/internal/application"
)

// OllamaAdapter implements the application.OllamaClient port.
type OllamaAdapter struct {
	baseURL string
	client  *http.Client
}

// NewOllamaAdapter creates a new Ollama outbound adapter.
func NewOllamaAdapter(baseURL string) *OllamaAdapter {
	return &OllamaAdapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (o *OllamaAdapter) ListModels(ctx context.Context) ([]application.OllamaModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting ollama at %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			Digest     string `json:"digest"`
			ModifiedAt string `json:"modified_at"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w", err)
	}

	models := make([]application.OllamaModel, 0, len(result.Models))
	for _, m := range result.Models {
		models = append(models, application.OllamaModel{
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
		})
	}
	return models, nil
}

func (o *OllamaAdapter) IsHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/", nil)
	if err != nil {
		return false
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
