// Package domain defines the core types and ports for the beagrid inference grid.
package domain

import "context"

// NodeRegistry is the outbound port for managing grid nodes.
type NodeRegistry interface {
	Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error)
	Heartbeat(ctx context.Context, req HeartbeatRequest) error
	Deregister(ctx context.Context, nodeID string) error
	GetNode(ctx context.Context, nodeID string) (*Node, error)
	ListNodes(ctx context.Context) ([]Node, error)
	ListOnlineNodes(ctx context.Context) ([]Node, error)
}

// Router is the port for the routing algorithm.
type Router interface {
	// Route picks the best node for the given model based on priority, load, and availability.
	Route(ctx context.Context, model string) (*RoutingDecision, error)
}

// InferenceProxy is the port for forwarding inference requests to Ollama nodes.
type InferenceProxy interface {
	Forward(ctx context.Context, node *Node, req *InferenceRequest) ([]byte, error)
	ForwardStream(ctx context.Context, node *Node, req *InferenceRequest, flush func([]byte)) error
}

// OllamaClient is the port for interacting with a local Ollama instance (used by agents).
type OllamaClient interface {
	ListModels(ctx context.Context) ([]Model, error)
	IsHealthy(ctx context.Context) bool
}
