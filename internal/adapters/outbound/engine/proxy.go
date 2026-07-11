// Package engine provides outbound adapters for inference engine communication.
package engine

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

// Proxy implements the application.EngineProxy port.
// It forwards OpenAI-compatible requests to the target engine node.
type Proxy struct {
	client *http.Client
	logger *slog.Logger
}

// NewProxy creates a new engine proxy adapter.
func NewProxy(logger *slog.Logger) *Proxy {
	return &Proxy{
		client: &http.Client{Timeout: 600 * time.Second},
		logger: logger,
	}
}

// Forward sends a request to the target engine and writes the response back.
func (p *Proxy) Forward(w http.ResponseWriter, r *http.Request, target *domain.Node, endpointPath string, body []byte, stream bool) {
	url := trimSlash(target.EndpointURL) + "/" + endpointPath

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		writeOpenAIError(w, 502, "Failed to create proxy request: "+err.Error(), "engine_error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	if stream {
		p.proxyStream(w, proxyReq)
	} else {
		p.proxyDirect(w, proxyReq)
	}
}

// ForwardMedia forwards media requests to a target engine's media URL.
func (p *Proxy) ForwardMedia(w http.ResponseWriter, r *http.Request, target *domain.Node, endpointPath string, body []byte, contentType string) {
	url := trimSlash(target.MediaURL) + "/" + endpointPath

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		writeOpenAIError(w, 502, "Failed to create media proxy request: "+err.Error(), "engine_error")
		return
	}
	proxyReq.Header.Set("Content-Type", contentType)
	p.proxyStream(w, proxyReq)
}

func (p *Proxy) proxyDirect(w http.ResponseWriter, proxyReq *http.Request) {
	resp, err := p.client.Do(proxyReq)
	if err != nil {
		writeOpenAIError(w, 502, "Engine request failed: "+err.Error(), "engine_error")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func (p *Proxy) proxyStream(w http.ResponseWriter, proxyReq *http.Request) {
	transport := &http.Transport{}
	streamClient := &http.Client{Transport: transport, Timeout: 0}
	resp, err := streamClient.Do(proxyReq)
	if err != nil {
		writeOpenAIError(w, 502, "Engine stream request failed: "+err.Error(), "engine_error")
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/event-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func writeOpenAIError(w http.ResponseWriter, status int, message, code string) {
	errType := "invalid_request_error"
	if status >= 500 {
		errType = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, `{"error":{"message":"`+message+`","type":"`+errType+`","param":null,"code":"`+code+`"}}`)
}
