package server

import (
	"context"
	"testing"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

func TestPriorityRouter_Route(t *testing.T) {
	registry := NewMemoryRegistry(30 * time.Second)
	ctx := context.Background()

	// Register two nodes with the same model but different priorities
	_, err := registry.Register(ctx, domain.RegisterRequest{
		Name:     "node-low-priority",
		Address:  "http://192.168.1.10:11434",
		Priority: 20,
		Models:   []domain.Model{{Name: "llama3.2:latest"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp2, err := registry.Register(ctx, domain.RegisterRequest{
		Name:     "node-high-priority",
		Address:  "http://192.168.1.20:11434",
		Priority: 1,
		Models:   []domain.Model{{Name: "llama3.2:latest"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	router := NewPriorityRouter(registry)
	decision, err := router.Route(ctx, "llama3.2:latest")
	if err != nil {
		t.Fatal(err)
	}

	// Should pick the high-priority node
	if decision.TargetNode.ID != resp2.NodeID {
		t.Errorf("expected node %s, got %s", resp2.NodeID, decision.TargetNode.ID)
	}
	if decision.TargetNode.Priority != 1 {
		t.Errorf("expected priority 1, got %d", decision.TargetNode.Priority)
	}
}

func TestPriorityRouter_Route_ModelNotFound(t *testing.T) {
	registry := NewMemoryRegistry(30 * time.Second)
	ctx := context.Background()

	_, _ = registry.Register(ctx, domain.RegisterRequest{
		Name:    "node-a",
		Address: "http://192.168.1.10:11434",
		Models:  []domain.Model{{Name: "llama3.2:latest"}},
	})

	router := NewPriorityRouter(registry)
	_, err := router.Route(ctx, "nonexistent-model")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestPriorityRouter_LoadAware(t *testing.T) {
	registry := NewMemoryRegistry(30 * time.Second)
	ctx := context.Background()

	// Two nodes with same priority, but one is heavily loaded
	resp1, err := registry.Register(ctx, domain.RegisterRequest{
		Name:     "node-idle",
		Address:  "http://192.168.1.10:11434",
		Priority: 5,
		Models:   []domain.Model{{Name: "mistral:latest"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp2, err := registry.Register(ctx, domain.RegisterRequest{
		Name:     "node-busy",
		Address:  "http://192.168.1.20:11434",
		Priority: 5,
		Models:   []domain.Model{{Name: "mistral:latest"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Make node-busy loaded
	_ = registry.Heartbeat(ctx, domain.HeartbeatRequest{
		NodeID: resp2.NodeID,
		Models: []domain.Model{{Name: "mistral:latest"}},
		Stats:  domain.NodeStats{ActiveRequests: 10},
	})

	router := NewPriorityRouter(registry)
	decision, err := router.Route(ctx, "mistral:latest")
	if err != nil {
		t.Fatal(err)
	}

	if decision.TargetNode.ID != resp1.NodeID {
		t.Errorf("expected idle node %s, got %s", resp1.NodeID, decision.TargetNode.ID)
	}
}
