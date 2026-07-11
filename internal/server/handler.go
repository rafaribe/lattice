package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/rafaribe/beagrid/internal/domain"
)

// Handler aggregates all HTTP endpoints for the beagrid server.
type Handler struct {
	registry domain.NodeRegistry
	router   domain.Router
	proxy    domain.InferenceProxy
	logger   *slog.Logger
	requests atomic.Int64
}

func NewHandler(registry domain.NodeRegistry, router domain.Router, proxy domain.InferenceProxy, logger *slog.Logger) *Handler {
	return &Handler{
		registry: registry,
		router:   router,
		proxy:    proxy,
		logger:   logger,
	}
}

// RegisterRoutes wires all API routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Node management
	mux.HandleFunc("POST /api/v1/nodes/register", h.handleRegister)
	mux.HandleFunc("POST /api/v1/nodes/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", h.handleDeregister)
	mux.HandleFunc("GET /api/v1/nodes", h.handleListNodes)
	mux.HandleFunc("GET /api/v1/nodes/{id}", h.handleGetNode)

	// Grid info
	mux.HandleFunc("GET /api/v1/grid/info", h.handleGridInfo)

	// OpenAI-compatible inference endpoint
	mux.HandleFunc("POST /v1/chat/completions", h.handleInference)

	// Health
	mux.HandleFunc("GET /healthz", h.handleHealth)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.registry.Register(r.Context(), req)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("node registered", "node_id", resp.NodeID, "name", req.Name, "models", len(req.Models))
	h.jsonResponse(w, resp, http.StatusCreated)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req domain.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.registry.Heartbeat(r.Context(), req); err != nil {
		h.jsonError(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeregister(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if err := h.registry.Deregister(r.Context(), nodeID); err != nil {
		h.jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	h.logger.Info("node deregistered", "node_id", nodeID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.registry.ListNodes(r.Context())
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, nodes, http.StatusOK)
}

func (h *Handler) handleGetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	node, err := h.registry.GetNode(r.Context(), nodeID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	h.jsonResponse(w, node, http.StatusOK)
}

func (h *Handler) handleGridInfo(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.registry.ListNodes(r.Context())
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	modelSet := make(map[string]struct{})
	online := 0
	for _, n := range nodes {
		if n.Status == domain.StatusOnline {
			online++
		}
		for _, m := range n.Models {
			modelSet[m.Name] = struct{}{}
		}
	}

	models := make([]string, 0, len(modelSet))
	for m := range modelSet {
		models = append(models, m)
	}

	info := domain.GridInfo{
		TotalNodes:    len(nodes),
		OnlineNodes:   online,
		TotalModels:   len(modelSet),
		UniqueModels:  models,
		TotalRequests: h.requests.Load(),
	}
	h.jsonResponse(w, info, http.StatusOK)
}

func (h *Handler) handleInference(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.jsonError(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req domain.InferenceRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		h.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	decision, err := h.router.Route(r.Context(), req.Model)
	if err != nil {
		h.jsonError(w, fmt.Sprintf("routing failed: %s", err), http.StatusServiceUnavailable)
		return
	}

	h.requests.Add(1)
	h.logger.Info("routing request", "model", req.Model, "target", decision.TargetNode.Name, "reason", decision.Reason)

	if req.Stream {
		h.streamInference(w, r.Context(), decision.TargetNode, &req)
		return
	}

	respBytes, err := h.proxy.Forward(r.Context(), decision.TargetNode, &req)
	if err != nil {
		h.jsonError(w, fmt.Sprintf("inference failed: %s", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func (h *Handler) streamInference(w http.ResponseWriter, ctx context.Context, node *domain.Node, req *domain.InferenceRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.jsonError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	err := h.proxy.ForwardStream(ctx, node, req, func(chunk []byte) {
		w.Write(chunk)
		flusher.Flush()
	})
	if err != nil {
		h.logger.Error("stream error", "err", err)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) jsonResponse(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
