package routingcore

import "time"

// SimTurn is one recorded turn prepared for counterfactual replay
// (§R18.1). Boundary loaders assemble these from the observed substrate;
// In.ObservedUsage must carry the realized token bundle for dollar math.
type SimTurn struct {
	SessionID string
	Timestamp time.Time
	In        DecisionInput
}

// EvidenceKey addresses calibration evidence for a proposed move: the
// turn-kind being rerouted and the tier it would land on.
type EvidenceKey struct {
	Kind TurnKind
	Tier Tier
}

// Evidence verdict vocabulary consumed by routing.Simulate. Mirrors the
// Model Value Report's parity verdicts (kept as plain strings so routing
// does not import the intelligence tree).
const (
	EvidenceParity          = "parity"
	EvidenceCandidateBetter = "candidate_better"
)
