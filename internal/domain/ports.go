// Package domain defines the core types and business rules for the lattice inference grid.
// This package has NO external dependencies — it is the innermost hexagonal layer.
//
// Port interfaces (NodeRegistry, EngineDetector, etc.) live in the application layer
// (internal/application/ports.go) since they describe application-level contracts,
// not domain-level business rules.
package domain
