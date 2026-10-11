package codeintel

import "github.com/marmutapp/superbased-observer/internal/codeintelapi"

// The facade (Provider and the types that cross it) lives in the core
// package internal/codeintelapi (lane MP-L1H, T-715). These aliases keep
// every existing caller compiling unchanged.
type (
	// Provider is codeintelapi.Provider.
	Provider = codeintelapi.Provider
	// Symbol is codeintelapi.Symbol.
	Symbol = codeintelapi.Symbol
	// SymbolMatch is codeintelapi.SymbolMatch.
	SymbolMatch = codeintelapi.SymbolMatch
	// Ref is codeintelapi.Ref.
	Ref = codeintelapi.Ref
	// GraphNode is codeintelapi.GraphNode.
	GraphNode = codeintelapi.GraphNode
	// GraphEdge is codeintelapi.GraphEdge.
	GraphEdge = codeintelapi.GraphEdge
	// ResultSet is codeintelapi.ResultSet.
	ResultSet = codeintelapi.ResultSet
	// Graph is codeintelapi.Graph.
	Graph = codeintelapi.Graph
	// RelationDirection is codeintelapi.RelationDirection.
	RelationDirection = codeintelapi.RelationDirection
)

// Relation directions (see codeintelapi).
const (
	RelationCallers  = codeintelapi.RelationCallers
	RelationCallees  = codeintelapi.RelationCallees
	RelationContains = codeintelapi.RelationContains
)

// DisabledNote is codeintelapi.DisabledNote.
const DisabledNote = codeintelapi.DisabledNote

// IsDisabled is codeintelapi.IsDisabled.
func IsDisabled(p Provider) bool { return codeintelapi.IsDisabled(p) }
