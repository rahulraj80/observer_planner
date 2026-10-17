package cloudcontract

// EvidenceLevel is the closed, ORDERED vocabulary naming how much of a session
// an enrichment envelope may carry. It is shared by the node and the org server
// (Cloud Intelligence parity design 2026-10-03, section 4.1) so the two sides
// can never accept different shapes:
//
//   - the org admin picks the org's level (org_intel_settings.evidence_level);
//   - an individual-tenancy developer consents to a level (their consent covers
//     every level at or below it, never above);
//   - the node builds an org-plane envelope at the LOWER of the two;
//   - the org server refuses a submission above either.
//
// The three levels are exactly the personal plane's own choices, so a managed
// developer and an individual one are described by the same words:
//
//   - structural: metadata only (tool, model family, timing, metrics, activity
//     mix, action summaries). No text from the session.
//   - titles: structural plus the developer's FIRST real prompt (the personal
//     plane's "Title only" level and its first_user_prompt_excerpt field class).
//   - excerpts: structural plus bounded, scrubbed excerpts under the
//     bounded_context_enrichment purpose (the personal "Also describe with
//     excerpts" level), shaped by the versioned evidence settings.
//
// The zero value (empty string) is not a level; Valid reports false for it and
// every comparison treats it as below structural, so an unset level can never
// authorize anything.
type EvidenceLevel string

const (
	// EvidenceLevelStructural carries metadata only.
	EvidenceLevelStructural EvidenceLevel = "structural"
	// EvidenceLevelTitles adds the first real user prompt.
	EvidenceLevelTitles EvidenceLevel = "titles"
	// EvidenceLevelExcerpts adds bounded, scrubbed session excerpts.
	EvidenceLevelExcerpts EvidenceLevel = "excerpts"
)

// DefaultOrgEvidenceLevel is the org plane's default level (ruling R2 / Q6):
// the smallest level that produces a useful title.
const DefaultOrgEvidenceLevel = EvidenceLevelTitles

// evidenceLevelRows is the ordered level table (CLAUDE.md #5): index is rank,
// and each row names the consent purpose an envelope at that level is built
// under and whether it carries the first-prompt field class.
var evidenceLevelRows = []struct {
	level       EvidenceLevel
	purpose     Purpose
	firstPrompt bool
}{
	{EvidenceLevelStructural, PurposeStructuralInsights, false},
	{EvidenceLevelTitles, PurposeStructuralInsights, true},
	{EvidenceLevelExcerpts, PurposeContextEnrichment, false},
}

// AllEvidenceLevels returns the levels in ascending order.
func AllEvidenceLevels() []EvidenceLevel {
	out := make([]EvidenceLevel, 0, len(evidenceLevelRows))
	for _, r := range evidenceLevelRows {
		out = append(out, r.level)
	}
	return out
}

// rank returns the level's position, or -1 for a value outside the vocabulary.
func (l EvidenceLevel) rank() int {
	for i, r := range evidenceLevelRows {
		if r.level == l {
			return i
		}
	}
	return -1
}

// Valid reports whether l is a member of the closed vocabulary.
func (l EvidenceLevel) Valid() bool { return l.rank() >= 0 }

// Covers reports whether consent (or an org setting) at level l authorizes an
// envelope built at level want. An invalid l covers nothing; an invalid want is
// never covered.
func (l EvidenceLevel) Covers(want EvidenceLevel) bool {
	lr, wr := l.rank(), want.rank()
	return lr >= 0 && wr >= 0 && wr <= lr
}

// Purpose is the consent purpose an envelope at this level is built under. An
// invalid level yields the structural purpose, the narrowest one, so a caller
// that skipped Valid can never widen disclosure through this accessor.
func (l EvidenceLevel) Purpose() Purpose {
	if i := l.rank(); i >= 0 {
		return evidenceLevelRows[i].purpose
	}
	return PurposeStructuralInsights
}

// FieldClasses is the field-class set an envelope at this level binds: the
// first-prompt class at titles, none otherwise (the excerpts level is governed
// by its purpose, not a class).
func (l EvidenceLevel) FieldClasses() []FieldClass {
	if i := l.rank(); i >= 0 && evidenceLevelRows[i].firstPrompt {
		return []FieldClass{FieldClassFirstUserPrompt}
	}
	return nil
}

// MinEvidenceLevel returns the lower of two levels. An invalid input yields
// structural only when the other is valid; two invalid inputs yield the zero
// value, which Valid rejects.
func MinEvidenceLevel(a, b EvidenceLevel) EvidenceLevel {
	ar, br := a.rank(), b.rank()
	switch {
	case ar < 0 && br < 0:
		return ""
	case ar < 0:
		return EvidenceLevelStructural
	case br < 0:
		return EvidenceLevelStructural
	case ar <= br:
		return a
	default:
		return b
	}
}

// NormalizeEvidenceLevel maps a stored or submitted value onto the vocabulary,
// falling back to def when it is unknown (the read-side fail-open every closed
// org setting uses).
func NormalizeEvidenceLevel(v string, def EvidenceLevel) EvidenceLevel {
	if l := EvidenceLevel(v); l.Valid() {
		return l
	}
	return def
}

// EnvelopeEvidenceLevel is the level an envelope's CONTENT carries: no context
// excerpts is structural, exactly the first prompt is titles, anything more is
// excerpts. The org server decides a submission's level with it (never from
// the request), and the node labels its ledger with the same rule.
func EnvelopeEvidenceLevel(e Envelope) EvidenceLevel {
	switch {
	case len(e.Context) == 0:
		return EvidenceLevelStructural
	case e.ContextIsFirstPromptOnly():
		return EvidenceLevelTitles
	default:
		return EvidenceLevelExcerpts
	}
}
