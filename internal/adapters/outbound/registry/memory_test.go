package registry

import (
	"context"
	"testing"
	"time"

	"github.com/rafaribe/lattice/internal/domain"
)

func TestMemory_CreateAndDiscover(t *testing.T) {
	reg := New("bg-test", "test", 60)
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
	engines, err := reg.Discover(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(engines) != 1 {
		t.Fatalf("expected 1 engine, got %d", len(engines))
	}
	if engines[0].Name != "gpu-1" {
		t.Fatalf("expected name 'gpu-1', got '%s'", engines[0].Name)
	}

	// Discover by model
	engines, err = reg.Discover(ctx, "llama3.2:latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(engines) != 1 {
		t.Fatal("expected 1 engine for llama3.2:latest")
	}

	// Discover non-existent model
	engines, err = reg.Discover(ctx, "nonexistent:latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(engines) != 0 {
		t.Fatal("expected 0 engines for nonexistent model")
	}
}

func TestMemory_Heartbeat(t *testing.T) {
	reg := New("bg-test", "test", 60)
	ctx := context.Background()

	resp, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	_, _ = reg.Update(ctx, resp.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest"},
		EndpointURL: "http://192.168.1.10:11434/v1",
		Load:        domain.Load{ActiveTasks: 0},
	})

	err := reg.Heartbeat(ctx, domain.HeartbeatRequest{NodeID: resp.NodeID, Load: domain.Load{ActiveTasks: 2}})
	if err != nil {
		t.Fatal(err)
	}

	node, _ := reg.Get(ctx, resp.NodeID)
	if node.Load.ActiveTasks != 2 {
		t.Fatalf("expected 2 active tasks, got %d", node.Load.ActiveTasks)
	}
}

func TestMemory_Delete(t *testing.T) {
	reg := New("bg-test", "test", 60)
	ctx := context.Background()

	resp, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	err := reg.Delete(ctx, resp.NodeID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = reg.Get(ctx, resp.NodeID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestMemory_TTLReaping(t *testing.T) {
	reg := New("bg-test", "test", 1) // 1 second TTL
	ctx := context.Background()

	resp, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	_, _ = reg.Update(ctx, resp.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest"},
		EndpointURL: "http://192.168.1.10:11434/v1",
		Load:        domain.Load{ActiveTasks: 0},
	})

	time.Sleep(2 * time.Second)

	engines, _ := reg.Discover(ctx, "")
	if len(engines) != 0 {
		t.Fatalf("expected 0 engines after TTL, got %d", len(engines))
	}
}

func TestMemory_LoadBasedRouting(t *testing.T) {
	reg := New("bg-test", "test", 60)
	ctx := context.Background()

	// Create two engines with different load
	resp1, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	reg.Update(ctx, resp1.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest"},
		EndpointURL: "http://192.168.1.10:11434/v1",
		Load:        domain.Load{ActiveTasks: 5},
		Name:        "gpu-1",
	})

	resp2, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-2"})
	reg.Update(ctx, resp2.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest"},
		EndpointURL: "http://192.168.1.20:11434/v1",
		Load:        domain.Load{ActiveTasks: 1},
		Name:        "gpu-2",
	})

	engines, _ := reg.Discover(ctx, "llama3.2:latest")
	if len(engines) != 2 {
		t.Fatalf("expected 2 engines, got %d", len(engines))
	}
	// First engine should be the least loaded
	if engines[0].Name != "gpu-2" {
		t.Fatalf("expected gpu-2 first (least loaded), got '%s'", engines[0].Name)
	}
}

func TestMemory_Info(t *testing.T) {
	reg := New("bg-test", "test-grid", 60)
	ctx := context.Background()

	resp, _ := reg.Create(ctx, domain.NodeCreateRequest{Role: domain.RoleEngine, Name: "gpu-1"})
	reg.Update(ctx, resp.NodeID, domain.NodeUpdateRequest{
		Role:        domain.RoleEngine,
		Models:      []string{"llama3.2:latest"},
		EndpointURL: "http://192.168.1.10:11434/v1",
	})

	info := reg.Info()
	if info.GridID != "bg-test" {
		t.Fatalf("expected grid_id 'bg-test', got '%s'", info.GridID)
	}
	if info.Name != "test-grid" {
		t.Fatalf("expected name 'test-grid', got '%s'", info.Name)
	}
	if info.EnginesOnline != 1 {
		t.Fatalf("expected 1 engine online, got %d", info.EnginesOnline)
	}
}

func TestMemory_TTL(t *testing.T) {
	reg := New("bg-test", "test", 42)
	if reg.TTL() != 42 {
		t.Fatalf("expected TTL 42, got %d", reg.TTL())
	}
}
