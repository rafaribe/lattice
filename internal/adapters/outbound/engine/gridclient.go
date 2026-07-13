package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rafaribe/lattice/internal/domain"
)

// GridClient implements the application.GridServer port.
// It communicates with the lattice server to register/heartbeat/deregister nodes.
type GridClient struct {
	serverURL string
	client    *http.Client
}

// NewGridClient creates a new grid server client adapter.
func NewGridClient(serverURL string) *GridClient {
	return &GridClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GridClient) Register(ctx context.Context, nodeID string, payload domain.NodeUpdateRequest) error {
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/nodes/%s", g.serverURL, nodeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to server %s: %w", g.serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("registration returned %d", resp.StatusCode)
	}
	return nil
}

func (g *GridClient) Heartbeat(ctx context.Context, req domain.HeartbeatRequest) error {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.serverURL+"/nodes/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("node not found on server")
	}
	return nil
}

func (g *GridClient) Deregister(ctx context.Context, nodeID string) error {
	url := fmt.Sprintf("%s/nodes/%s", g.serverURL, nodeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// WaitForEndpoint polls until the server is reachable or the timeout expires.
func (g *GridClient) WaitForEndpoint(ctx context.Context, url string) error {
	for i := 0; i < 30; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := g.client.Do(req)
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
