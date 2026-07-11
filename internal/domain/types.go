// Package domain defines the core types for the beagrid inference grid.
package domain

import "time"

// Node represents a compute node in the grid that runs Ollama.
type Node struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Address     string    `json:"address"`      // e.g. "http://192.168.1.10:11434"
	Models      []Model   `json:"models"`       // models available on this node
	Priority    int       `json:"priority"`     // lower = higher priority (0 is highest)
	Status      Status    `json:"status"`       // online, offline, draining
	LastSeen    time.Time `json:"last_seen"`    // last heartbeat
	RegisteredAt time.Time `json:"registered_at"`
	Stats       NodeStats `json:"stats"`
}

// Model represents an LLM model available on a node.
type Model struct {
	Name       string `json:"name"`       // e.g. "llama3.2:latest"
	Size       int64  `json:"size"`       // size in bytes
	Digest     string `json:"digest"`     // model digest/hash
	ModifiedAt string `json:"modified_at"`
}

// NodeStats contains runtime metrics for a node.
type NodeStats struct {
	ActiveRequests  int     `json:"active_requests"`
	TotalRequests   int64   `json:"total_requests"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	ErrorCount      int64   `json:"error_count"`
	GPUUtilization  float64 `json:"gpu_utilization,omitempty"`  // 0-100
	MemoryUsedBytes int64   `json:"memory_used_bytes,omitempty"`
}

// Status represents the operational status of a node.
type Status string

const (
	StatusOnline   Status = "online"
	StatusOffline  Status = "offline"
	StatusDraining Status = "draining"
)

// RegisterRequest is sent by an agent to join the grid.
type RegisterRequest struct {
	Name     string  `json:"name"`
	Address  string  `json:"address"`
	Models   []Model `json:"models"`
	Priority int     `json:"priority,omitempty"` // optional, default 10
}

// RegisterResponse is returned after successful registration.
type RegisterResponse struct {
	NodeID            string        `json:"node_id"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

// HeartbeatRequest is sent periodically by agents to indicate liveness.
type HeartbeatRequest struct {
	NodeID string    `json:"node_id"`
	Models []Model   `json:"models"` // refreshed model list
	Stats  NodeStats `json:"stats"`
}

// InferenceRequest represents an OpenAI-compatible chat completion request.
type InferenceRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
	// Pass-through fields for OpenAI compatibility
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RoutingDecision captures the result of the routing algorithm.
type RoutingDecision struct {
	TargetNode *Node   `json:"target_node"`
	Reason     string  `json:"reason"`
	Score      float64 `json:"score"`
}

// GridInfo provides an overview of the grid state.
type GridInfo struct {
	TotalNodes    int      `json:"total_nodes"`
	OnlineNodes   int      `json:"online_nodes"`
	TotalModels   int      `json:"total_models"`
	UniqueModels  []string `json:"unique_models"`
	TotalRequests int64    `json:"total_requests"`
}
