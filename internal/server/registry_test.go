package server

import (
	"context"
	"testing"
	"time"

	"github.com/rafaribe/beagrid/internal/domain"
)

func TestRegistry_CreateAndDiscover(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	// Create a node
	resp, err := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NodeID == "" {
		t.Fatal("expected non-empty node ID")
	}

	// Update it with models
	_, err = reg.Update(ctx, resp.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest", "mistral:latest"},
		EndpointURL: "http://192.168.1.10:11434/v1",
		Load:        domain.Load{ActiveTasks: 0},
		Name:        "gpu-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Discover all engines
	engines, _ := reg.Discover(ctx, "")
	if len(engines) != 1 {
		t.Fatalf("expected 1 engine, got %d", len(engines))
	}
	if engines[0].Name != "gpu-1" {
		t.Errorf("expected name 'gpu-1', got %q", engines[0].Name)
	}

	// Discover by model
	engines, _ = reg.Discover(ctx, "llama3.2:latest")
	if len(engines) != 1 {
		t.Fatalf("expected 1 engine for llama3.2, got %d", len(engines))
	}

	engines, _ = reg.Discover(ctx, "nonexistent")
	if len(engines) != 0 {
		t.Fatalf("expected 0 engines for nonexistent model, got %d", len(engines))
	}
}

func TestRegistry_PutWithoutCreate(t *testing.T) {
	// autonomous-grid allows PUT without prior POST
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	node, err := reg.Update(ctx, "node-direct-put", domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"qwen:latest"},
		EndpointURL: "http://localhost:8080/v1",
		Load:        domain.Load{ActiveTasks: 2},
		Name:        "direct-engine",
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.NodeID != "node-direct-put" {
		t.Errorf("expected node-direct-put, got %s", node.NodeID)
	}

	engines, _ := reg.Discover(ctx, "qwen:latest")
	if len(engines) != 1 {
		t.Fatalf("expected 1, got %d", len(engines))
	}
}

func TestRegistry_LoadBasedRouting(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	// Two engines with same model, different loads
	reg.Update(ctx, "node-idle", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"model-a"},
		EndpointURL: "http://a:8000/v1", Load: domain.Load{ActiveTasks: 0}, Name: "idle",
	})
	reg.Update(ctx, "node-busy", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"model-a"},
		EndpointURL: "http://b:8000/v1", Load: domain.Load{ActiveTasks: 5}, Name: "busy",
	})

	engines, _ := reg.Discover(ctx, "model-a")
	if len(engines) != 2 {
		t.Fatalf("expected 2, got %d", len(engines))
	}
	// Idle should come first
	if engines[0].Name != "idle" {
		t.Errorf("expected idle first, got %s", engines[0].Name)
	}
}

func TestRegistry_TTLReap(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 1) // 1 second TTL
	ctx := context.Background()

	reg.Update(ctx, "node-stale", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"x"},
		EndpointURL: "http://x:8000/v1", Name: "stale",
	})

	// Should be discoverable immediately
	engines, _ := reg.Discover(ctx, "x")
	if len(engines) != 1 {
		t.Fatalf("expected 1, got %d", len(engines))
	}

	// Wait for TTL to expire
	time.Sleep(1100 * time.Millisecond)

	// Should be reaped
	engines, _ = reg.Discover(ctx, "x")
	if len(engines) != 0 {
		t.Fatalf("expected 0 after TTL, got %d", len(engines))
	}
}

func TestRegistry_Heartbeat(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	reg.Update(ctx, "node-hb", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"m1"},
		EndpointURL: "http://x:8000/v1", Load: domain.Load{ActiveTasks: 0},
	})

	// Heartbeat with updated load
	err := reg.Heartbeat(ctx, domain.HeartbeatRequest{
		NodeID: "node-hb",
		Load:   domain.Load{ActiveTasks: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	node, _ := reg.Get(ctx, "node-hb")
	if node.Load.ActiveTasks != 3 {
		t.Errorf("expected 3 active tasks, got %d", node.Load.ActiveTasks)
	}
}

func TestRegistry_HeartbeatNotFound(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	err := reg.Heartbeat(ctx, domain.HeartbeatRequest{NodeID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestRegistry_Delete(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	reg.Update(ctx, "node-del", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"m1"},
		EndpointURL: "http://x:8000/v1",
	})

	err := reg.Delete(ctx, "node-del")
	if err != nil {
		t.Fatal(err)
	}

	_, err = reg.Get(ctx, "node-del")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestRegistry_UpstreamAlias(t *testing.T) {
	reg := NewRegistry("bg-test", "test", 60)
	ctx := context.Background()

	reg.Update(ctx, "node-alias", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"my-coder"},
		EndpointURL: "http://x:8000/v1",
		Upstream:    map[string]string{"my-coder": "qwen3-coder:32b"},
		Name:        "aliased",
	})

	engines, _ := reg.Discover(ctx, "my-coder")
	if len(engines) != 1 {
		t.Fatalf("expected 1, got %d", len(engines))
	}
	if engines[0].Upstream["my-coder"] != "qwen3-coder:32b" {
		t.Errorf("expected upstream mapping to qwen3-coder:32b")
	}
}

func TestRegistry_GridInfo(t *testing.T) {
	reg := NewRegistry("bg-test-123", "mytest", 60)
	ctx := context.Background()

	reg.Update(ctx, "n1", domain.NodeUpdateRequest{
		Role: domain.RoleEngine, Models: []string{"a"},
		EndpointURL: "http://x:8000/v1",
	})
	reg.Update(ctx, "n2", domain.NodeUpdateRequest{
		Role: domain.RoleApp, Models: []string{},
	})

	info := reg.Info()
	if info.GridID != "bg-test-123" {
		t.Errorf("expected bg-test-123, got %s", info.GridID)
	}
	if info.Name != "mytest" {
		t.Errorf("expected mytest, got %s", info.Name)
	}
	if info.EnginesOnline != 1 {
		t.Errorf("expected 1 engine online, got %d", info.EnginesOnline)
	}
}
