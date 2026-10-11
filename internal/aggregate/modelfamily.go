package aggregate

import "github.com/marmutapp/superbased-observer/internal/modelfamily"

// modelfamily.go re-exports the model_family normalizer, whose owner is the
// core package internal/modelfamily (lane MP-L1E, T-715): the store's
// structural-insights facts use it too, and they must not import this
// package (egress.aggregate-share compiles out).

// FamilyMapVersion is the version of the model_family normalizer's mapping
// (see modelfamily.FamilyMapVersion).
const FamilyMapVersion = modelfamily.FamilyMapVersion

// The closed model_family vocabulary (see internal/modelfamily).
const (
	FamilyClaudeOpus   = modelfamily.FamilyClaudeOpus
	FamilyClaudeSonnet = modelfamily.FamilyClaudeSonnet
	FamilyClaudeHaiku  = modelfamily.FamilyClaudeHaiku
	FamilyGPT5         = modelfamily.FamilyGPT5
	FamilyGPT5Mini     = modelfamily.FamilyGPT5Mini
	FamilyGeminiPro    = modelfamily.FamilyGeminiPro
	FamilyGeminiFlash  = modelfamily.FamilyGeminiFlash
	FamilyGrok         = modelfamily.FamilyGrok
	FamilyOpenWeight   = modelfamily.FamilyOpenWeight
	FamilyOther        = modelfamily.FamilyOther
)

// Families is the complete closed set Family can return.
var Families = modelfamily.Families

// Family maps a raw model id to its closed model_family (see
// modelfamily.Family).
func Family(modelID string) string { return modelfamily.Family(modelID) }
