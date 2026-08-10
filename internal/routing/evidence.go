package routing

import "math"

// EvidenceSample is one tier-level pooled calibration aggregate for a
// turn-kind: the graded denominators actually observed (§R7.2). The
// snapshot refresher pools the per-model model_calibration global cells
// up to (turn-kind, tier); this package derives the quality signal from
// the pooled counts so the store stays aggregate-only and routing stays
// pure.
type EvidenceSample struct {
	// N is the raw turn count — informational only; the graded counts
	// below drive the verdict (proxy status rows grade the error rate,
	// attributable actions grade the tool-failure rate).
	N int64
	// ErrorGraded / ErrorCount grade the proxy-observed error rate.
	ErrorGraded int64
	ErrorCount  int64
	// ToolGraded / ToolFailures grade the attributed tool-failure rate.
	ToolGraded   int64
	ToolFailures int64
}

// ErrorRate returns the graded error rate (0 when nothing graded).
func (e EvidenceSample) ErrorRate() float64 {
	if e.ErrorGraded <= 0 {
		return 0
	}
	return float64(e.ErrorCount) / float64(e.ErrorGraded)
}

// ToolFailureRate returns the graded tool-failure rate (0 when nothing
// graded).
func (e EvidenceSample) ToolFailureRate() float64 {
	if e.ToolGraded <= 0 {
		return 0
	}
	return float64(e.ToolFailures) / float64(e.ToolGraded)
}

// QualityEvidence is the calibration-derived expectation for one
// (turn-kind, target tier) move: the quality signal plus the confidence
// in it (0..1). Zero confidence means uncalibrated — callers then render
// the benefit as "quality unknown" (§5.4).
type QualityEvidence struct {
	Signal     QualitySignal
	Confidence float64
}

// MinEvidenceSample is the §R7.2 evidence floor: below this many graded
// observations on BOTH sides the verdict degrades to unknown — no claim.
const MinEvidenceSample int64 = 50

// materialRegressionDelta is the error-rate increase (candidate minus
// baseline, in proportions) beyond which a downshift counts as a
// material quality regression. Research-backed from api-intelligence
// §8: a cheap model whose output needs correction more than ~20% of the
// time loses its price advantage.
const materialRegressionDelta = 0.20

// QualityEvidenceFor derives the quality signal for a candidate tier
// against the baseline tier pool, mirroring the Model Value Report's
// §R7.2 verdict logic at tier granularity: error rate first when both
// sides pass the sample floor on proxy-graded observations, tool-failure
// rate as the fallback (action attribution works on watcher-only
// substrates too), and insufficient evidence → QualityUnknown / 0
// confidence otherwise.
//
// Confidence compounds as evidence accumulates: it starts at 0.5 at the
// sample floor and tightens toward 0.95 as the graded counts grow (the
// CI half-width shrinks ∝ 1/√n). For hold verdicts the confidence is the
// degree to which the interval excludes a material regression
// (materialRegressionDelta) — the actionable "safe to downshift" claim.
func QualityEvidenceFor(candidate, baseline EvidenceSample, minSample int64) QualityEvidence {
	if minSample <= 0 {
		minSample = MinEvidenceSample
	}
	if candidate.ErrorGraded >= minSample && baseline.ErrorGraded >= minSample {
		return gradeEvidence(candidate.ErrorRate(), candidate.ErrorGraded,
			baseline.ErrorRate(), baseline.ErrorGraded)
	}
	if candidate.ToolGraded >= minSample && baseline.ToolGraded >= minSample {
		return gradeEvidence(candidate.ToolFailureRate(), candidate.ToolGraded,
			baseline.ToolFailureRate(), baseline.ToolGraded)
	}
	return QualityEvidence{Signal: QualityUnknown}
}

// gradeEvidence turns a rate difference + denominators into a signal and
// confidence. Directional verdicts (better/worse) require the 95%
// interval to exclude zero; the confidence is then how tightly the
// interval bounds the direction. A hold verdict gets the confidence that
// the candidate does not regress by more than materialRegressionDelta.
func gradeEvidence(candRate float64, candN int64, baseRate float64, baseN int64) QualityEvidence {
	delta := candRate - baseRate
	ci := diffCI95(candRate, candN, baseRate, baseN)
	switch {
	case delta-ci > 0:
		return QualityEvidence{Signal: QualityWorse, Confidence: clampEvidenceConfidence(1 - ci)}
	case delta+ci < 0:
		return QualityEvidence{Signal: QualityBetter, Confidence: clampEvidenceConfidence(1 - ci)}
	default:
		upper := delta + ci
		if upper < 0 {
			upper = 0
		}
		return QualityEvidence{Signal: QualityHold, Confidence: clampEvidenceConfidence(1 - upper/materialRegressionDelta)}
	}
}

// clampEvidenceConfidence bounds the derived confidence: never below 0.5
// (a graded verdict is never worthless) and never above 0.95 (evidence
// is observational, never certain).
func clampEvidenceConfidence(c float64) float64 {
	if c < 0.5 {
		return 0.5
	}
	if c > 0.95 {
		return 0.95
	}
	return c
}

// diffCI95 is the 95% normal-approximation half-width for a difference
// of two proportions. Mirrors the Model Value Report's diffCI95, kept
// local so this package stays import-free of the intelligence tree.
// Returns 0 when either denominator is zero (no graded observations —
// the verdict then degrades).
func diffCI95(p1 float64, n1 int64, p2 float64, n2 int64) float64 {
	if n1 <= 0 || n2 <= 0 {
		return 0
	}
	return 1.96 * math.Sqrt(p1*(1-p1)/float64(n1)+p2*(1-p2)/float64(n2))
}
