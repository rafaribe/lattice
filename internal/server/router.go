package server

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/rafaribe/beagrid/internal/domain"
)

// PriorityRouter implements the Router port using a weighted scoring algorithm.
// Factors: priority (weight 40%), active load (30%), error rate (20%), latency (10%).
type PriorityRouter struct {
	registry domain.NodeRegistry
}

func NewPriorityRouter(registry domain.NodeRegistry) *PriorityRouter {
	return &PriorityRouter{registry: registry}
}

func (r *PriorityRouter) Route(ctx context.Context, model string) (*domain.RoutingDecision, error) {
	nodes, err := r.registry.ListOnlineNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing online nodes: %w", err)
	}

	// Filter to nodes that have the requested model
	candidates := make([]domain.Node, 0)
	for _, n := range nodes {
		for _, m := range n.Models {
			if m.Name == model {
				candidates = append(candidates, n)
				break
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no online node has model %q", model)
	}

	// Score each candidate (lower score = better)
	type scored struct {
		node  domain.Node
		score float64
	}
	scoredNodes := make([]scored, 0, len(candidates))
	for _, n := range candidates {
		s := computeScore(n)
		scoredNodes = append(scoredNodes, scored{node: n, score: s})
	}

	sort.Slice(scoredNodes, func(i, j int) bool {
		return scoredNodes[i].score < scoredNodes[j].score
	})

	best := scoredNodes[0]
	return &domain.RoutingDecision{
		TargetNode: &best.node,
		Reason:     fmt.Sprintf("score=%.2f (priority=%d, active=%d)", best.score, best.node.Priority, best.node.Stats.ActiveRequests),
		Score:      best.score,
	}, nil
}

// computeScore produces a weighted score for a node. Lower is better.
func computeScore(n domain.Node) float64 {
	// Normalize priority: 0-100 scale (priority 0 → 0, priority 100 → 100)
	priorityScore := float64(n.Priority)

	// Load score: active requests (capped at 20)
	loadScore := math.Min(float64(n.Stats.ActiveRequests)*5, 100)

	// Error rate score
	var errorScore float64
	if n.Stats.TotalRequests > 0 {
		errorScore = (float64(n.Stats.ErrorCount) / float64(n.Stats.TotalRequests)) * 100
	}

	// Latency score: normalize avg latency (cap at 10s)
	latencyScore := math.Min(n.Stats.AvgLatencyMs/100, 100)

	// Weighted combination
	return priorityScore*0.4 + loadScore*0.3 + errorScore*0.2 + latencyScore*0.1
}
