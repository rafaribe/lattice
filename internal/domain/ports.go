package domain

import "context"

// NodeRegistry is the port for managing grid nodes.
type NodeRegistry interface {
	Create(ctx context.Context, req NodeCreateRequest) (*NodeCreateResponse, error)
	Update(ctx context.Context, nodeID string, req NodeUpdateRequest) (*Node, error)
	Heartbeat(ctx context.Context, req HeartbeatRequest) error
	Delete(ctx context.Context, nodeID string) error
	Get(ctx context.Context, nodeID string) (*Node, error)
	// Discover returns active engines, optionally filtered by model.
	Discover(ctx context.Context, model string) ([]*Node, error)
	Info() *GridInfo
}

// EngineDetector discovers inference engines running on the local machine.
type EngineDetector interface {
	Detect(ctx context.Context) ([]DetectedEngine, error)
}
