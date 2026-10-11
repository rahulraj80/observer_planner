package routing

import "github.com/marmutapp/superbased-observer/internal/routingcore"

// core.go re-exports the pure, always-linked half of routing that moved to
// internal/routingcore (modular packaging lane MP-L1I, T-715, ruling D-512):
// the tier vocabulary and tier table, the turn-kind classifier, the
// command-class resolver, the shared request / session / snapshot /
// decision types, the sub-agent recommendation emitter and the replay input
// types. Every existing caller of these names compiles unchanged; code that
// must keep working without proxy.routing imports internal/routingcore.

// Types re-exported from internal/routingcore.
type (
	// ActionSignal is routingcore.ActionSignal.
	ActionSignal = routingcore.ActionSignal
	// BudgetBurnState is routingcore.BudgetBurnState.
	BudgetBurnState = routingcore.BudgetBurnState
	// Channel is routingcore.Channel.
	Channel = routingcore.Channel
	// Classification is routingcore.Classification.
	Classification = routingcore.Classification
	// CommandClass is routingcore.CommandClass.
	CommandClass = routingcore.CommandClass
	// ContextWindowFn is routingcore.ContextWindowFn.
	ContextWindowFn = routingcore.ContextWindowFn
	// Decision is routingcore.Decision.
	Decision = routingcore.Decision
	// DecisionInput is routingcore.DecisionInput.
	DecisionInput = routingcore.DecisionInput
	// EvidenceKey is routingcore.EvidenceKey.
	EvidenceKey = routingcore.EvidenceKey
	// HealthState is routingcore.HealthState.
	HealthState = routingcore.HealthState
	// Mode is routingcore.Mode.
	Mode = routingcore.Mode
	// ModelCandidate is routingcore.ModelCandidate.
	ModelCandidate = routingcore.ModelCandidate
	// PriceFn is routingcore.PriceFn.
	PriceFn = routingcore.PriceFn
	// PromptUsage is routingcore.PromptUsage.
	PromptUsage = routingcore.PromptUsage
	// ProviderShape is routingcore.ProviderShape.
	ProviderShape = routingcore.ProviderShape
	// ReasonCode is routingcore.ReasonCode.
	ReasonCode = routingcore.ReasonCode
	// SessionActivity is routingcore.SessionActivity.
	SessionActivity = routingcore.SessionActivity
	// SessionState is routingcore.SessionState.
	SessionState = routingcore.SessionState
	// SimTurn is routingcore.SimTurn.
	SimTurn = routingcore.SimTurn
	// Snapshot is routingcore.Snapshot.
	Snapshot = routingcore.Snapshot
	// SubagentEvidence is routingcore.SubagentEvidence.
	SubagentEvidence = routingcore.SubagentEvidence
	// SubagentRecommendation is routingcore.SubagentRecommendation.
	SubagentRecommendation = routingcore.SubagentRecommendation
	// Tier is routingcore.Tier.
	Tier = routingcore.Tier
	// TierResolver is routingcore.TierResolver.
	TierResolver = routingcore.TierResolver
	// TierSource is routingcore.TierSource.
	TierSource = routingcore.TierSource
	// TierTable is routingcore.TierTable.
	TierTable = routingcore.TierTable
	// TurnKind is routingcore.TurnKind.
	TurnKind = routingcore.TurnKind
	// TurnShape is routingcore.TurnShape.
	TurnShape = routingcore.TurnShape
	// WindowState is routingcore.WindowState.
	WindowState = routingcore.WindowState
)

// Constants re-exported from internal/routingcore.
const (
	// ChannelAgentConfig is routingcore.ChannelAgentConfig.
	ChannelAgentConfig = routingcore.ChannelAgentConfig
	// ChannelProxy is routingcore.ChannelProxy.
	ChannelProxy = routingcore.ChannelProxy
	// CommandBuild is routingcore.CommandBuild.
	CommandBuild = routingcore.CommandBuild
	// CommandLint is routingcore.CommandLint.
	CommandLint = routingcore.CommandLint
	// CommandNone is routingcore.CommandNone.
	CommandNone = routingcore.CommandNone
	// CommandOther is routingcore.CommandOther.
	CommandOther = routingcore.CommandOther
	// CommandTest is routingcore.CommandTest.
	CommandTest = routingcore.CommandTest
	// CommandVCS is routingcore.CommandVCS.
	CommandVCS = routingcore.CommandVCS
	// DefaultLongContextPromptTokens is routingcore.DefaultLongContextPromptTokens.
	DefaultLongContextPromptTokens = routingcore.DefaultLongContextPromptTokens
	// EffortHigh is routingcore.EffortHigh.
	EffortHigh = routingcore.EffortHigh
	// EffortLow is routingcore.EffortLow.
	EffortLow = routingcore.EffortLow
	// EffortMedium is routingcore.EffortMedium.
	EffortMedium = routingcore.EffortMedium
	// EffortMinimal is routingcore.EffortMinimal.
	EffortMinimal = routingcore.EffortMinimal
	// EntitlementAPIKey is routingcore.EntitlementAPIKey.
	EntitlementAPIKey = routingcore.EntitlementAPIKey
	// EntitlementSubscription is routingcore.EntitlementSubscription.
	EntitlementSubscription = routingcore.EntitlementSubscription
	// EvidenceCandidateBetter is routingcore.EvidenceCandidateBetter.
	EvidenceCandidateBetter = routingcore.EvidenceCandidateBetter
	// EvidenceParity is routingcore.EvidenceParity.
	EvidenceParity = routingcore.EvidenceParity
	// HealthDegraded is routingcore.HealthDegraded.
	HealthDegraded = routingcore.HealthDegraded
	// HealthHalfOpen is routingcore.HealthHalfOpen.
	HealthHalfOpen = routingcore.HealthHalfOpen
	// HealthHealthy is routingcore.HealthHealthy.
	HealthHealthy = routingcore.HealthHealthy
	// HealthOpen is routingcore.HealthOpen.
	HealthOpen = routingcore.HealthOpen
	// ModeAdvise is routingcore.ModeAdvise.
	ModeAdvise = routingcore.ModeAdvise
	// ModeEnforce is routingcore.ModeEnforce.
	ModeEnforce = routingcore.ModeEnforce
	// ModeOff is routingcore.ModeOff.
	ModeOff = routingcore.ModeOff
	// ReasonAvailabilityFallback is routingcore.ReasonAvailabilityFallback.
	ReasonAvailabilityFallback = routingcore.ReasonAvailabilityFallback
	// ReasonBudgetBand50 is routingcore.ReasonBudgetBand50.
	ReasonBudgetBand50 = routingcore.ReasonBudgetBand50
	// ReasonBudgetBand80 is routingcore.ReasonBudgetBand80.
	ReasonBudgetBand80 = routingcore.ReasonBudgetBand80
	// ReasonBudgetBand95 is routingcore.ReasonBudgetBand95.
	ReasonBudgetBand95 = routingcore.ReasonBudgetBand95
	// ReasonBudgetExhausted is routingcore.ReasonBudgetExhausted.
	ReasonBudgetExhausted = routingcore.ReasonBudgetExhausted
	// ReasonBudgetHardStop is routingcore.ReasonBudgetHardStop.
	ReasonBudgetHardStop = routingcore.ReasonBudgetHardStop
	// ReasonCacheHold is routingcore.ReasonCacheHold.
	ReasonCacheHold = routingcore.ReasonCacheHold
	// ReasonCalibrationDemoted is routingcore.ReasonCalibrationDemoted.
	ReasonCalibrationDemoted = routingcore.ReasonCalibrationDemoted
	// ReasonCapabilityHold is routingcore.ReasonCapabilityHold.
	ReasonCapabilityHold = routingcore.ReasonCapabilityHold
	// ReasonContextWindowUnknown is routingcore.ReasonContextWindowUnknown.
	ReasonContextWindowUnknown = routingcore.ReasonContextWindowUnknown
	// ReasonCustomRule is routingcore.ReasonCustomRule.
	ReasonCustomRule = routingcore.ReasonCustomRule
	// ReasonEffortDownshift is routingcore.ReasonEffortDownshift.
	ReasonEffortDownshift = routingcore.ReasonEffortDownshift
	// ReasonEntitlementHold is routingcore.ReasonEntitlementHold.
	ReasonEntitlementHold = routingcore.ReasonEntitlementHold
	// ReasonEscalation is routingcore.ReasonEscalation.
	ReasonEscalation = routingcore.ReasonEscalation
	// ReasonFailOpen is routingcore.ReasonFailOpen.
	ReasonFailOpen = routingcore.ReasonFailOpen
	// ReasonInsufficientEvidence is routingcore.ReasonInsufficientEvidence.
	ReasonInsufficientEvidence = routingcore.ReasonInsufficientEvidence
	// ReasonNoCandidate is routingcore.ReasonNoCandidate.
	ReasonNoCandidate = routingcore.ReasonNoCandidate
	// ReasonNoRoute is routingcore.ReasonNoRoute.
	ReasonNoRoute = routingcore.ReasonNoRoute
	// ReasonOverpoweredHousekeeping is routingcore.ReasonOverpoweredHousekeeping.
	ReasonOverpoweredHousekeeping = routingcore.ReasonOverpoweredHousekeeping
	// ReasonOverpoweredRead is routingcore.ReasonOverpoweredRead.
	ReasonOverpoweredRead = routingcore.ReasonOverpoweredRead
	// ReasonOverpoweredSubagent is routingcore.ReasonOverpoweredSubagent.
	ReasonOverpoweredSubagent = routingcore.ReasonOverpoweredSubagent
	// ReasonOverpoweredTestRun is routingcore.ReasonOverpoweredTestRun.
	ReasonOverpoweredTestRun = routingcore.ReasonOverpoweredTestRun
	// ReasonPhasePin is routingcore.ReasonPhasePin.
	ReasonPhasePin = routingcore.ReasonPhasePin
	// ReasonPrivacyHold is routingcore.ReasonPrivacyHold.
	ReasonPrivacyHold = routingcore.ReasonPrivacyHold
	// ReasonQualityFloorHold is routingcore.ReasonQualityFloorHold.
	ReasonQualityFloorHold = routingcore.ReasonQualityFloorHold
	// ReasonRateLimitWindow is routingcore.ReasonRateLimitWindow.
	ReasonRateLimitWindow = routingcore.ReasonRateLimitWindow
	// ReasonStickinessHold is routingcore.ReasonStickinessHold.
	ReasonStickinessHold = routingcore.ReasonStickinessHold
	// ReasonUnclassifiedModel is routingcore.ReasonUnclassifiedModel.
	ReasonUnclassifiedModel = routingcore.ReasonUnclassifiedModel
	// ReasonUnknownTurnKind is routingcore.ReasonUnknownTurnKind.
	ReasonUnknownTurnKind = routingcore.ReasonUnknownTurnKind
	// ShapeAnthropic is routingcore.ShapeAnthropic.
	ShapeAnthropic = routingcore.ShapeAnthropic
	// ShapeGoogle is routingcore.ShapeGoogle.
	ShapeGoogle = routingcore.ShapeGoogle
	// ShapeOpenAI is routingcore.ShapeOpenAI.
	ShapeOpenAI = routingcore.ShapeOpenAI
	// ShapeUnknown is routingcore.ShapeUnknown.
	ShapeUnknown = routingcore.ShapeUnknown
	// TierFree is routingcore.TierFree.
	TierFree = routingcore.TierFree
	// TierHaikuClass is routingcore.TierHaikuClass.
	TierHaikuClass = routingcore.TierHaikuClass
	// TierLocal is routingcore.TierLocal.
	TierLocal = routingcore.TierLocal
	// TierOpusClass is routingcore.TierOpusClass.
	TierOpusClass = routingcore.TierOpusClass
	// TierSonnetClass is routingcore.TierSonnetClass.
	TierSonnetClass = routingcore.TierSonnetClass
	// TierSourceDateStripped is routingcore.TierSourceDateStripped.
	TierSourceDateStripped = routingcore.TierSourceDateStripped
	// TierSourceExact is routingcore.TierSourceExact.
	TierSourceExact = routingcore.TierSourceExact
	// TierSourceFamily is routingcore.TierSourceFamily.
	TierSourceFamily = routingcore.TierSourceFamily
	// TierSourceMiss is routingcore.TierSourceMiss.
	TierSourceMiss = routingcore.TierSourceMiss
	// TierUnclassified is routingcore.TierUnclassified.
	TierUnclassified = routingcore.TierUnclassified
	// TurnEdit is routingcore.TurnEdit.
	TurnEdit = routingcore.TurnEdit
	// TurnHousekeeping is routingcore.TurnHousekeeping.
	TurnHousekeeping = routingcore.TurnHousekeeping
	// TurnLongContext is routingcore.TurnLongContext.
	TurnLongContext = routingcore.TurnLongContext
	// TurnPlan is routingcore.TurnPlan.
	TurnPlan = routingcore.TurnPlan
	// TurnReadOnly is routingcore.TurnReadOnly.
	TurnReadOnly = routingcore.TurnReadOnly
	// TurnSubagent is routingcore.TurnSubagent.
	TurnSubagent = routingcore.TurnSubagent
	// TurnTestRun is routingcore.TurnTestRun.
	TurnTestRun = routingcore.TurnTestRun
	// TurnUnknown is routingcore.TurnUnknown.
	TurnUnknown = routingcore.TurnUnknown
)

// AllTiers is routingcore.AllTiers.
func AllTiers() []Tier { return routingcore.AllTiers() }

// AllTurnKinds is routingcore.AllTurnKinds.
func AllTurnKinds() []TurnKind { return routingcore.AllTurnKinds() }

// ClassifyTurnKind is routingcore.ClassifyTurnKind.
func ClassifyTurnKind(in DecisionInput) Classification { return routingcore.ClassifyTurnKind(in) }

// KnownReasonCodes is routingcore.KnownReasonCodes.
func KnownReasonCodes() []ReasonCode { return routingcore.KnownReasonCodes() }

// NewTierResolver is routingcore.NewTierResolver.
func NewTierResolver() *TierResolver { return routingcore.NewTierResolver() }

// RecommendSubagentModels is routingcore.RecommendSubagentModels.
func RecommendSubagentModels(tiers *TierTable, evidence []SubagentEvidence, minActions int64) []SubagentRecommendation {
	return routingcore.RecommendSubagentModels(tiers, evidence, minActions)
}

// ResolveCommandClass is routingcore.ResolveCommandClass.
func ResolveCommandClass(command string) CommandClass {
	return routingcore.ResolveCommandClass(command)
}

// ShapeForModel is routingcore.ShapeForModel.
func ShapeForModel(model string) ProviderShape { return routingcore.ShapeForModel(model) }

// SubagentRuleNames is routingcore.SubagentRuleNames.
func SubagentRuleNames() []string { return routingcore.SubagentRuleNames() }

// TurnKindRuleNames is routingcore.TurnKindRuleNames.
func TurnKindRuleNames() []string { return routingcore.TurnKindRuleNames() }
