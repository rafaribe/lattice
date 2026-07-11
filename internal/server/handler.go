package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

// Handler provides all HTTP endpoints for the beagrid server.
type Handler struct {
	registry *Registry
	logger   *slog.Logger
	client   *http.Client
}

func NewHandler(registry *Registry, logger *slog.Logger) *Handler {
	return &Handler{
		registry: registry,
		logger:   logger,
		client:   &http.Client{Timeout: 600 * time.Second},
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Grid info
	mux.HandleFunc("GET /grid/info", h.handleGridInfo)

	// Node lifecycle (matches autonomous-grid exactly)
	mux.HandleFunc("POST /nodes", h.handleCreateNode)
	mux.HandleFunc("PUT /nodes/{node_id}", h.handleUpdateNode)
	mux.HandleFunc("POST /nodes/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("DELETE /nodes/{node_id}", h.handleDeleteNode)
	mux.HandleFunc("GET /nodes/discover", h.handleDiscover)

	// OpenAI-compatible endpoints
	mux.HandleFunc("GET /v1/models", h.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST /v1/completions", h.handleCompletions)

	// Media placeholder endpoints
	mux.HandleFunc("POST /v1/media/image/generate", h.handleMediaProxy)
	mux.HandleFunc("POST /v1/media/image/edit", h.handleMediaProxy)
	mux.HandleFunc("POST /v1/media/video/i2v", h.handleMediaProxy)

	// Health
	mux.HandleFunc("GET /healthz", h.handleHealth)

	// Legacy aliases for our own API consumers
	mux.HandleFunc("GET /api/v1/nodes", h.handleDiscoverLegacy)
	mux.HandleFunc("GET /api/v1/grid/info", h.handleGridInfo)
}

// --- Grid Info ---

func (h *Handler) handleGridInfo(w http.ResponseWriter, _ *http.Request) {
	h.json(w, h.registry.Info(), http.StatusOK)
}

// --- Node Lifecycle ---

func (h *Handler) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req domain.NodeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.openaiError(w, 400, "Request body is not valid JSON", "invalid_json")
		return
	}
	resp, err := h.registry.Create(r.Context(), req)
	if err != nil {
		h.openaiError(w, 500, err.Error(), "internal_error")
		return
	}
	h.logger.Info("node created", "node_id", resp.NodeID, "role", resp.Role)
	h.json(w, resp, http.StatusOK)
}

func (h *Handler) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("node_id")
	var req domain.NodeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.openaiError(w, 400, "Request body is not valid JSON", "invalid_json")
		return
	}

	if (req.Role == domain.RoleEngine || req.Role == domain.RoleBoth) && len(req.Models) == 0 {
		h.openaiError(w, 400, "at least one model is required for engines", "invalid_request")
		return
	}

	textModels := []string{}
	for _, m := range req.Models {
		if !strings.HasPrefix(m, "comfyui:") {
			textModels = append(textModels, m)
		}
	}
	if len(textModels) > 0 && req.EndpointURL == "" {
		h.openaiError(w, 400, "endpoint_url is required for text engines", "invalid_request")
		return
	}

	node, err := h.registry.Update(r.Context(), nodeID, req)
	if err != nil {
		h.openaiError(w, 500, err.Error(), "internal_error")
		return
	}
	h.logger.Info("node updated", "node_id", nodeID, "models", node.Models)
	h.json(w, map[string]any{"status": "updated", "node": node}, http.StatusOK)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req domain.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.openaiError(w, 400, "Request body is not valid JSON", "invalid_json")
		return
	}
	if err := h.registry.Heartbeat(r.Context(), req); err != nil {
		h.openaiError(w, 404, "node not found", "not_found")
		return
	}
	h.json(w, map[string]any{"ttl_seconds": h.registry.ttl}, http.StatusOK)
}

func (h *Handler) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("node_id")
	if err := h.registry.Delete(r.Context(), nodeID); err != nil {
		h.openaiError(w, 404, "node not found", "not_found")
		return
	}
	h.logger.Info("node unregistered", "node_id", nodeID)
	h.json(w, map[string]string{"status": "unregistered"}, http.StatusOK)
}

func (h *Handler) handleDiscover(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	engines, _ := h.registry.Discover(r.Context(), model)
	h.json(w, map[string]any{"engines": engines}, http.StatusOK)
}

func (h *Handler) handleDiscoverLegacy(w http.ResponseWriter, r *http.Request) {
	engines, _ := h.registry.Discover(r.Context(), "")
	h.json(w, engines, http.StatusOK)
}

// --- OpenAI Endpoints ---

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	engines, _ := h.registry.Discover(r.Context(), "")
	seen := map[string]struct{}{}
	data := []domain.OpenAIModel{}
	created := time.Now().Unix()

	for _, engine := range engines {
		for _, model := range engine.Models {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			data = append(data, domain.OpenAIModel{
				ID:      model,
				Object:  "model",
				Created: created,
				OwnedBy: "lan",
			})
		}
	}
	h.json(w, domain.OpenAIModelList{Object: "list", Data: data}, http.StatusOK)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAI(w, r, "chat/completions")
}

func (h *Handler) handleCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAI(w, r, "completions")
}

func (h *Handler) proxyOpenAI(w http.ResponseWriter, r *http.Request, endpointPath string) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.openaiError(w, 400, "Failed to read request body", "invalid_request")
		return
	}

	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil {
		h.openaiError(w, 400, "Request body is not valid JSON", "invalid_json")
		return
	}

	model, _ := body["model"].(string)
	if model == "" {
		h.openaiError(w, 400, "model is required", "invalid_request")
		return
	}

	engines, _ := h.registry.Discover(r.Context(), model)
	if len(engines) == 0 {
		h.openaiError(w, 503, "No active local engine for model "+model, "engine_unavailable")
		return
	}

	target := engines[0]
	h.logger.Info("routing", "model", model, "target", target.Name, "node_id", target.NodeID, "load", target.Load.ActiveTasks)

	// Rewrite model alias → upstream real name if needed
	if upstream, ok := target.Upstream[model]; ok && upstream != model {
		body["model"] = upstream
		rawBody, _ = json.Marshal(body)
	}

	url := trimSlash(target.EndpointURL) + "/" + endpointPath
	stream, _ := body["stream"].(bool)

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(rawBody)))
	if err != nil {
		h.openaiError(w, 502, "Failed to create proxy request: "+err.Error(), "engine_error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	if stream {
		h.proxyStream(w, proxyReq, target)
	} else {
		h.proxyDirect(w, proxyReq, target)
	}
}

func (h *Handler) proxyDirect(w http.ResponseWriter, proxyReq *http.Request, target *domain.Node) {
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		h.openaiError(w, 502, "Engine request failed: "+err.Error(), "engine_error")
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

func (h *Handler) proxyStream(w http.ResponseWriter, proxyReq *http.Request, target *domain.Node) {
	transport := &http.Transport{}
	streamClient := &http.Client{Transport: transport, Timeout: 0}
	resp, err := streamClient.Do(proxyReq)
	if err != nil {
		h.openaiError(w, 502, "Engine stream request failed: "+err.Error(), "engine_error")
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

// --- Media Proxy ---

func (h *Handler) handleMediaProxy(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.openaiError(w, 400, "Failed to read request body", "invalid_request")
		return
	}

	// Determine media model from endpoint path
	path := r.URL.Path
	var model string
	switch {
	case strings.Contains(path, "image/generate"):
		model = "comfyui:image_generation"
	case strings.Contains(path, "image/edit"):
		model = "comfyui:image_editing"
	case strings.Contains(path, "video/i2v"):
		model = "comfyui:i2v"
	default:
		h.openaiError(w, 400, "Unknown media endpoint", "invalid_request")
		return
	}

	engines, _ := h.registry.Discover(r.Context(), model)
	if len(engines) == 0 {
		h.openaiError(w, 503, "No active local media engine for "+model, "engine_unavailable")
		return
	}
	target := engines[0]
	if target.MediaURL == "" {
		h.openaiError(w, 503, "Engine did not advertise a media URL", "engine_unavailable")
		return
	}

	// Strip /v1/ prefix for media proxy
	endpointPath := strings.TrimPrefix(path, "/v1/")
	url := trimSlash(target.MediaURL) + "/" + endpointPath

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(rawBody)))
	if err != nil {
		h.openaiError(w, 502, "Failed to create media proxy request: "+err.Error(), "engine_error")
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	h.proxyStream(w, proxyReq, target)
}

// --- Health ---

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	h.json(w, map[string]string{"status": "ok"}, http.StatusOK)
}

// --- Helpers ---

func (h *Handler) json(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) openaiError(w http.ResponseWriter, status int, message, code string) {
	errType := "invalid_request_error"
	if status >= 500 {
		errType = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(domain.OpenAIError{
		Error: domain.OpenAIErrorBody{
			Message: message,
			Type:    errType,
			Param:   nil,
			Code:    code,
		},
	})
}
