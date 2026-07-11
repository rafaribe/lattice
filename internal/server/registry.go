package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rafaribe/beagrid/internal/domain"
)

// MemoryRegistry is an in-memory implementation of the NodeRegistry port.
type MemoryRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*domain.Node

	heartbeatTimeout time.Duration
}

func NewMemoryRegistry(heartbeatTimeout time.Duration) *MemoryRegistry {
	r := &MemoryRegistry{
		nodes:            make(map[string]*domain.Node),
		heartbeatTimeout: heartbeatTimeout,
	}
	go r.reaper()
	return r
}

func (r *MemoryRegistry) Register(_ context.Context, req domain.RegisterRequest) (*domain.RegisterResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	nodeID := uuid.New().String()[:8]
	priority := req.Priority
	if priority == 0 {
		priority = 10
	}

	node := &domain.Node{
		ID:           nodeID,
		Name:         req.Name,
		Address:      req.Address,
		Models:       req.Models,
		Priority:     priority,
		Status:       domain.StatusOnline,
		LastSeen:     time.Now(),
		RegisteredAt: time.Now(),
	}

	r.nodes[nodeID] = node
	return &domain.RegisterResponse{
		NodeID:            nodeID,
		HeartbeatInterval: 10 * time.Second,
	}, nil
}

func (r *MemoryRegistry) Heartbeat(_ context.Context, req domain.HeartbeatRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[req.NodeID]
	if !ok {
		return fmt.Errorf("node %s not found", req.NodeID)
	}

	node.LastSeen = time.Now()
	node.Status = domain.StatusOnline
	node.Models = req.Models
	node.Stats = req.Stats
	return nil
}

func (r *MemoryRegistry) Deregister(_ context.Context, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, nodeID)
	return nil
}

func (r *MemoryRegistry) GetNode(_ context.Context, nodeID string) (*domain.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}
	return node, nil
}

func (r *MemoryRegistry) ListNodes(_ context.Context) ([]domain.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make([]domain.Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		nodes = append(nodes, *n)
	}
	return nodes, nil
}

func (r *MemoryRegistry) ListOnlineNodes(_ context.Context) ([]domain.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make([]domain.Node, 0)
	for _, n := range r.nodes {
		if n.Status == domain.StatusOnline {
			nodes = append(nodes, *n)
		}
	}
	return nodes, nil
}

// reaper marks nodes offline if they miss heartbeats.
func (r *MemoryRegistry) reaper() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for _, n := range r.nodes {
			if now.Sub(n.LastSeen) > r.heartbeatTimeout && n.Status == domain.StatusOnline {
				n.Status = domain.StatusOffline
			}
		}
		r.mu.Unlock()
	}
}
