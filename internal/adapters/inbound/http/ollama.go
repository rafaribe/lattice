package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/rafaribe/lattice/internal/domain"
)

// RegisterOllamaRoutes wires the Ollama-native API endpoints.
func (h *Handler) RegisterOllamaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tags", h.handleOllamaTags)
	mux.HandleFunc("POST /api/chat", h.handleOllamaChat)
	mux.HandleFunc("POST /api/generate", h.handleOllamaGenerate)
	mux.HandleFunc("POST /api/show", h.handleOllamaShow)
	mux.HandleFunc("GET /api/version", h.handleOllamaVersion)
	mux.HandleFunc("GET /api/ps", h.handleOllamaPs)
}

// --- /api/tags ---

// OllamaTagsResponse mirrors Ollama's GET /api/tags response.
type OllamaTagsResponse struct {
	Models []OllamaModelInfo `json:"models"`
}

// OllamaModelInfo represents a model in the Ollama tags response.
type OllamaModelInfo struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	ModifiedAt string             `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    OllamaModelDetails `json:"details"`
}

// OllamaModelDetails contains model metadata.
type OllamaModelDetails struct {
	ParentModel   string   `json:"parent_model"`
	Format        string   `json:"format"`
	Family        string   `json:"family"`
	Families      []string `json:"families"`
	ParameterSize string   `json:"parameter_size"`
	QuantLevel    string   `json:"quantization_level"`
}

func (h *Handler) handleOllamaTags(w http.ResponseWriter, r *http.Request) {
	engines, _ := h.registry.Discover(r.Context(), "")
	seen := map[string]struct{}{}
	models := []OllamaModelInfo{}

	for _, engine := range engines {
		for _, model := range engine.Models {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			models = append(models, OllamaModelInfo{
				Name:       model,
				Model:      model,
				ModifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Size:       0,
				Digest:     "",
				Details:    OllamaModelDetails{Format: "gguf"},
			})
		}
	}

	h.writeJSON(w, OllamaTagsResponse{Models: models}, http.StatusOK)
}

// --- /api/chat ---

func (h *Handler) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	h.proxyOllama(w, r, "api/chat")
}

// --- /api/generate ---

func (h *Handler) handleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	h.proxyOllama(w, r, "api/generate")
}

// --- /api/show ---

func (h *Handler) handleOllamaShow(w http.ResponseWriter, r *http.Request) {
	h.proxyOllama(w, r, "api/show")
}

// --- /api/ps ---

func (h *Handler) handleOllamaPs(w http.ResponseWriter, r *http.Request) {
	// Return running models across all engines — proxy to first available engine
	engines, _ := h.registry.Discover(r.Context(), "")
	if len(engines) == 0 {
		h.writeJSON(w, map[string]any{"models": []any{}}, http.StatusOK)
		return
	}

	// Forward to the first engine's Ollama /api/ps
	target := engines[0]
	ollamaBase := deriveOllamaBase(target.EndpointURL)
	h.proxy.Forward(w, r, &domain.Node{EndpointURL: ollamaBase}, "api/ps", nil, false)
}

// --- /api/version ---

func (h *Handler) handleOllamaVersion(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, map[string]string{"version": "0.9.0-lattice"}, http.StatusOK)
}

// --- HEAD / (Ollama health check) ---
// Handled by the file server at GET / in main.go — Ollama clients
// also accept a 200 from GET / as proof the server is alive.

// --- Ollama proxy logic ---

func (h *Handler) proxyOllama(w http.ResponseWriter, r *http.Request, endpointPath string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyCompletions)

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.ollamaError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil {
		h.ollamaError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	model, _ := body["model"].(string)
	if model == "" {
		h.ollamaError(w, http.StatusBadRequest, "model is required")
		return
	}

	engines, _ := h.registry.Discover(r.Context(), model)
	if len(engines) == 0 {
		h.metrics.ProxyErrors.Add(r.Context(), 1, metric.WithAttributes(attribute.String("reason", "no_engine")))
		h.ollamaError(w, http.StatusServiceUnavailable, "no engine available for model "+model)
		return
	}

	target := engines[0]
	h.logger.Info("routing (ollama)", "model", model, "target", target.Name, "node_id", target.NodeID, "load", target.Load.ActiveTasks)
	h.metrics.ProxyRequests.Add(r.Context(), 1, metric.WithAttributes(attribute.String("model", model)))

	// Rewrite model alias if needed
	if upstream, ok := target.Upstream[model]; ok && upstream != model {
		body["model"] = upstream
		rawBody, _ = json.Marshal(body)
	}

	// Derive Ollama base URL from the OpenAI endpoint URL (strip /v1 suffix)
	ollamaBase := deriveOllamaBase(target.EndpointURL)

	// Determine if streaming — Ollama defaults to stream=true if not set
	stream := true
	if s, ok := body["stream"]; ok {
		stream, _ = s.(bool)
	}

	// Create a temporary node with the Ollama base URL for forwarding
	ollamaTarget := &domain.Node{
		NodeID:      target.NodeID,
		Name:        target.Name,
		EndpointURL: ollamaBase,
	}
	h.proxy.Forward(w, r, ollamaTarget, endpointPath, rawBody, stream)
}

// deriveOllamaBase strips /v1 suffix from the OpenAI-compatible endpoint URL
// to get the Ollama base URL (e.g., http://host:11434/v1 → http://host:11434).
func deriveOllamaBase(endpointURL string) string {
	base := strings.TrimRight(endpointURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	return base
}

func (h *Handler) ollamaError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
