package routing

import "github.com/marmutapp/superbased-observer/internal/routingcore"

// observer_planner fork: the benefit record types moved to routingcore with
// upstream's routing/routingcore split; these aliases keep routing's API.
type (
	QualitySignal    = routingcore.QualitySignal
	QualityEvidence  = routingcore.QualityEvidence
	BenefitBreakdown = routingcore.BenefitBreakdown
)

// Quality signals (see routingcore).
const (
	QualityHold    = routingcore.QualityHold
	QualityBetter  = routingcore.QualityBetter
	QualityWorse   = routingcore.QualityWorse
	QualityUnknown = routingcore.QualityUnknown
)
