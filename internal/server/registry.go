package server

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rafaribe/beagrid/internal/domain"
)

const DefaultNodeTTL = 60 // seconds

// Registry is an in-memory implementation of the NodeRegistry port.
type Registry struct {
	mu     sync.RWMutex
	nodes  map[string]*domain.Node
	gridID string
	name   string
	ttl    int // seconds
}

func NewRegistry(gridID, name string, ttl int) *Registry {
	if ttl <= 0 {
		ttl = DefaultNodeTTL
	}
	r := &Registry{
		nodes:  make(map[string]*domain.Node),
		gridID: gridID,
		name:   name,
		ttl:    ttl,
	}
	go r.reaper()
	return r
}

func (r *Registry) Create(_ context.Context, req domain.NodeCreateRequest) (*domain.NodeCreateResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	nodeID := uuid.New().String()
	role := req.Role
	if role == "" {
		role = domain.RoleApp
	}

	node := &domain.Node{
		NodeID:        nodeID,
		Role:          role,
		Name:          req.Name,
		Models:        []string{},
		Load:          domain.Load{},
		Upstream:      map[string]string{},
		FirstSeenAt:   time.Now().UTC().Format(time.RFC3339),
		LastHeartbeat: time.Now(),
		TTLSeconds:    r.ttl,
	}
	r.nodes[nodeID] = node
	return &domain.NodeCreateResponse{NodeID: nodeID, Role: role}, nil
}

func (r *Registry) Update(_ context.Context, nodeID string, req domain.NodeUpdateRequest) (*domain.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		// Auto-create (autonomous-grid allows PUT without prior POST)
		node = &domain.Node{
			NodeID:      nodeID,
			FirstSeenAt: time.Now().UTC().Format(time.RFC3339),
			TTLSeconds:  r.ttl,
		}
		r.nodes[nodeID] = node
	}

	node.Role = req.Role
	node.Models = dedup(req.Models)
	node.Name = req.Name
	if req.EndpointURL != "" {
		node.EndpointURL = trimSlash(req.EndpointURL)
	}
	if req.MediaURL != "" {
		node.MediaURL = trimSlash(req.MediaURL)
	}
	node.Pricing = req.Pricing
	node.Capabilities = req.Capabilities
	node.Load = req.Load
	node.Upstream = req.Upstream
	node.LastHeartbeat = time.Now()
	return node, nil
}

func (r *Registry) Heartbeat(_ context.Context, req domain.HeartbeatRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[req.NodeID]
	if !ok {
		return fmt.Errorf("node not found")
	}
	node.Load = req.Load
	node.LastHeartbeat = time.Now()
	return nil
}

func (r *Registry) Delete(_ context.Context, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.nodes[nodeID]; !ok {
		return fmt.Errorf("node not found")
	}
	delete(r.nodes, nodeID)
	return nil
}

func (r *Registry) Get(_ context.Context, nodeID string) (*domain.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node not found")
	}
	return node, nil
}

// Discover returns active engine nodes, optionally filtered by model.
// Stale nodes are reaped. Results sorted by load score ascending.
func (r *Registry) Discover(_ context.Context, model string) ([]*domain.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	stale := []string{}
	engines := []*domain.Node{}

	for id, n := range r.nodes {
		if now.Sub(n.LastHeartbeat).Seconds() > float64(r.ttl) {
			stale = append(stale, id)
			continue
		}
		if n.Role != domain.RoleEngine && n.Role != domain.RoleBoth {
			continue
		}
		if model != "" {
			found := false
			for _, m := range n.Models {
				if m == model {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		engines = append(engines, n)
	}

	for _, id := range stale {
		delete(r.nodes, id)
	}

	// Sort by load score (active_tasks), then by last heartbeat (freshest first)
	sort.Slice(engines, func(i, j int) bool {
		if engines[i].Load.ActiveTasks != engines[j].Load.ActiveTasks {
			return engines[i].Load.ActiveTasks < engines[j].Load.ActiveTasks
		}
		return engines[i].LastHeartbeat.After(engines[j].LastHeartbeat)
	})

	return engines, nil
}

func (r *Registry) Info() *domain.GridInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	online := 0
	now := time.Now()
	for _, n := range r.nodes {
		if now.Sub(n.LastHeartbeat).Seconds() <= float64(r.ttl) &&
			(n.Role == domain.RoleEngine || n.Role == domain.RoleBoth) {
			online++
		}
	}

	return &domain.GridInfo{
		GridID:        r.gridID,
		Name:          r.name,
		GridType:      "lan-permissionless",
		AuthRequired:  false,
		LANOnly:       true,
		NodeTTL:       r.ttl,
		EnginesOnline: online,
	}
}

// reaper removes stale nodes in the background.
func (r *Registry) reaper() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for id, n := range r.nodes {
			if now.Sub(n.LastHeartbeat).Seconds() > float64(r.ttl) {
				delete(r.nodes, id)
			}
		}
		r.mu.Unlock()
	}
}

func dedup(items []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
