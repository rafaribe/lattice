// Package domain defines the core types for the lattice inference grid.
// Mirrors autonomous-grid's data model: nodes with roles, model aliasing (upstream),
// load tracking, capabilities, TTL-based reaping.
package domain

import "time"

// Node represents a compute node in the grid.
type Node struct {
	NodeID        string            `json:"node_id"`
	Role          Role              `json:"role"`                    // engine, app, both
	Name          string            `json:"name,omitempty"`
	Models        []string          `json:"models"`                  // model names this node advertises
	EndpointURL   string            `json:"endpoint_url,omitempty"`  // OpenAI-compatible URL for text inference
	MediaURL      string            `json:"media_url,omitempty"`     // URL for media generation
	Pricing       map[string]any    `json:"pricing,omitempty"`
	Capabilities  map[string]any    `json:"capabilities,omitempty"`
	Load          Load              `json:"load"`
	Upstream      map[string]string `json:"upstream,omitempty"`      // advertised_name → engine's real model name
	FirstSeenAt   string            `json:"first_seen_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	TTLSeconds    int               `json:"ttl_seconds"`
	Status        NodeStatus        `json:"status"`                  // online, degraded, offline
	LastCheckedAt time.Time         `json:"last_checked_at,omitempty"`
	FailCount     int               `json:"fail_count,omitempty"`    // consecutive health check failures
}

// NodeStatus represents the health state of a node.
type NodeStatus string

const (
	StatusOnline   NodeStatus = "online"
	StatusDegraded NodeStatus = "degraded" // 1-2 failed checks
	StatusOffline  NodeStatus = "offline"  // 3+ failed checks
)

// Role defines what a node does in the grid.
type Role string

const (
	RoleEngine Role = "engine"
	RoleApp    Role = "app"
	RoleBoth   Role = "both"
)

// Load contains runtime load metrics for routing decisions.
type Load struct {
	ActiveTasks int     `json:"active_tasks"`
	GPUPercent  float64 `json:"gpu_percent,omitempty"`
	MemPercent  float64 `json:"mem_percent,omitempty"`
}

// NodeCreateRequest is POST /nodes — creates a node slot.
type NodeCreateRequest struct {
	Role Role   `json:"role"`
	Name string `json:"name,omitempty"`
}

// NodeCreateResponse is returned after creating a node.
type NodeCreateResponse struct {
	NodeID string `json:"node_id"`
	Role   Role   `json:"role"`
}

// NodeUpdateRequest is PUT /nodes/{node_id} — registers/updates an engine.
type NodeUpdateRequest struct {
	Role         Role              `json:"role"`
	Models       []string          `json:"models"`
	EndpointURL  string            `json:"endpoint_url,omitempty"`
	MediaURL     string            `json:"media_url,omitempty"`
	Pricing      map[string]any    `json:"pricing,omitempty"`
	Capabilities map[string]any    `json:"capabilities,omitempty"`
	Load         Load              `json:"load"`
	Name         string            `json:"name,omitempty"`
	Upstream     map[string]string `json:"upstream,omitempty"`
}

// HeartbeatRequest is POST /nodes/heartbeat.
type HeartbeatRequest struct {
	NodeID string `json:"node_id"`
	Load   Load   `json:"load"`
}

// GridInfo is returned by GET / and GET /grid/info.
type GridInfo struct {
	GridID        string `json:"grid_id"`
	Name          string `json:"name"`
	GridType      string `json:"grid_type"`
	AuthRequired  bool   `json:"auth_required"`
	LANOnly       bool   `json:"lan_only"`
	NodeTTL       int    `json:"node_ttl_seconds"`
	EnginesOnline int    `json:"engines_online"`
}

// OpenAIModel is one entry in /v1/models response.
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIModelList is the full /v1/models response.
type OpenAIModelList struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// OpenAIError is the standard OpenAI error envelope.
type OpenAIError struct {
	Error OpenAIErrorBody `json:"error"`
}

// OpenAIErrorBody is the inner error structure.
type OpenAIErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

// ChatCompletionRequest is the OpenAI-compatible chat completions request.
type ChatCompletionRequest struct {
	Model            string    `json:"model"`
	Messages         []Message `json:"messages"`
	Stream           bool      `json:"stream,omitempty"`
	Temperature      *float64  `json:"temperature,omitempty"`
	MaxTokens        *int      `json:"max_tokens,omitempty"`
	TopP             *float64  `json:"top_p,omitempty"`
	FrequencyPenalty *float64  `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64  `json:"presence_penalty,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GridConfig represents a persisted grid configuration (stored in ~/.lattice/grids/<id>/config.json).
type GridConfig struct {
	GridID         string `json:"grid_id"`
	Name           string `json:"name"`
	GridType       string `json:"grid_type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SignalingURL   string `json:"lan_signaling_url"`
	ServerPID      int    `json:"server_pid"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// GridState is the global state file (~/.lattice/state.json).
type GridState struct {
	ActiveGrid string `json:"active_grid,omitempty"`
}

// DetectedEngine represents an inference engine found running on the local machine.
type DetectedEngine struct {
	Label       string   `json:"label"`        // ollama, vllm, lm-studio, mlx, llama.cpp, comfyui
	EndpointURL string   `json:"endpoint_url"`
	Models      []string `json:"models"`
	Media       bool     `json:"media"`
}
