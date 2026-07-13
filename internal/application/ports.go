// Package application defines use-case orchestration and outbound port interfaces.
// Adapters implement these interfaces; the composition root wires them together.
package application

import (
	"context"
	"net/http"

	"github.com/rafaribe/lattice/internal/domain"
)

// NodeRegistry is the outbound port for node/engine persistence and discovery.
// The in-memory registry adapter implements this interface.
type NodeRegistry interface {
	Create(ctx context.Context, req domain.NodeCreateRequest) (*domain.NodeCreateResponse, error)
	Update(ctx context.Context, nodeID string, req domain.NodeUpdateRequest) (*domain.Node, error)
	Heartbeat(ctx context.Context, req domain.HeartbeatRequest) error
	Delete(ctx context.Context, nodeID string) error
	Get(ctx context.Context, nodeID string) (*domain.Node, error)
	Discover(ctx context.Context, model string) ([]*domain.Node, error)
	Info() *domain.GridInfo
	TTL() int
}

// EngineProxy is the outbound port for forwarding inference requests to engines.
type EngineProxy interface {
	// Forward sends a request to the target engine and writes the response.
	Forward(w http.ResponseWriter, r *http.Request, target *domain.Node, endpointPath string, body []byte, stream bool)
}

// EngineDetector is the outbound port for discovering local inference engines.
type EngineDetector interface {
	Detect(ctx context.Context) ([]domain.DetectedEngine, error)
}

// OllamaClient is the outbound port for communicating with a local Ollama instance.
type OllamaClient interface {
	ListModels(ctx context.Context) ([]OllamaModel, error)
	IsHealthy(ctx context.Context) bool
}

// OllamaModel represents a model from Ollama's API.
type OllamaModel struct {
	Name       string
	Size       int64
	Digest     string
	ModifiedAt string
}

// GridServer is the outbound port for agent→server communication.
type GridServer interface {
	Register(ctx context.Context, nodeID string, payload domain.NodeUpdateRequest) error
	Heartbeat(ctx context.Context, req domain.HeartbeatRequest) error
	Deregister(ctx context.Context, nodeID string) error
}
