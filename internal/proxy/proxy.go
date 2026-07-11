package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rafaribe/beagrid/internal/domain"
)

// OllamaProxy implements the InferenceProxy port for Ollama backends.
type OllamaProxy struct {
	client *http.Client
}

func NewOllamaProxy() *OllamaProxy {
	return &OllamaProxy{
		client: &http.Client{},
	}
}

func (p *OllamaProxy) Forward(ctx context.Context, node *domain.Node, req *domain.InferenceRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := strings.TrimRight(node.Address, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("forwarding to %s: %w", node.Address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("node %s returned %d: %s", node.ID, resp.StatusCode, string(respBody))
	}

	return io.ReadAll(resp.Body)
}

func (p *OllamaProxy) ForwardStream(ctx context.Context, node *domain.Node, req *domain.InferenceRequest, flush func([]byte)) error {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	url := strings.TrimRight(node.Address, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("forwarding stream to %s: %w", node.Address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node %s returned %d: %s", node.ID, resp.StatusCode, string(respBody))
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			flush(buf[:n])
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}
