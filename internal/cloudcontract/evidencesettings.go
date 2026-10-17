package cloudcontract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EvidenceSettings is the versioned excerpt-selection policy: how much of a
// session an envelope may describe within its evidence level. It is the ONE
// shape both planes accept (Cloud Intelligence parity design 2026-10-03,
// section 3.6): the personal plane freezes it on each per-upload receipt
// (agent migration 121), and the org plane stores it as the org's (or a team's,
// or a member's) evidence settings and hands it to the node, which builds the
// org-plane envelope with it. Because the validator lives here, a node and an
// org can never accept different shapes.
//
// Counts are maxima; zero excludes a category. UserMessages includes the first
// prompt. It carries no content.
type EvidenceSettings struct {
	Version           int  `json:"version"`
	UserMessages      int  `json:"user_messages"`
	AssistantMessages int  `json:"assistant_messages"`
	FailureClasses    int  `json:"failure_classes"`
	ActionSummaries   int  `json:"action_summaries"`
	ExcerptBytes      int  `json:"excerpt_bytes"`
	Milestones        bool `json:"milestones"`
	Outcomes          bool `json:"outcomes"`
}

// EvidenceSettingsVersion is the only version this build accepts.
const EvidenceSettingsVersion = 1

// evidenceSettingsFieldCount is the number of fields a stored document must
// carry: a partial document fails closed rather than inheriting defaults.
const evidenceSettingsFieldCount = 8

// Bounds of the evidence settings (the personal dashboard's form and the org
// form render these, so the numbers have one owner).
const (
	MaxEvidenceUserMessages      = 20
	MaxEvidenceAssistantMessages = 20
	MaxEvidenceFailureClasses    = 3
	MaxEvidenceExcerptTotal      = 20
	MaxEvidenceActionSummaries   = 256
	MinEvidenceExcerptBytes      = 128
	MaxEvidenceExcerptBytes      = 1024
)

// DefaultEvidenceSettings preserves the original excerpt selection.
func DefaultEvidenceSettings() EvidenceSettings {
	return EvidenceSettings{
		Version: EvidenceSettingsVersion, UserMessages: 15, AssistantMessages: 1,
		FailureClasses: 3, ActionSummaries: 256, ExcerptBytes: 1024, Milestones: true, Outcomes: true,
	}
}

// Validate refuses invalid or unknown policy versions without applying
// defaults.
func (s EvidenceSettings) Validate() error {
	if s.Version != EvidenceSettingsVersion {
		return fmt.Errorf("cloud evidence settings: unsupported version %d", s.Version)
	}
	if s.UserMessages < 0 || s.AssistantMessages < 0 || s.FailureClasses < 0 || s.FailureClasses > MaxEvidenceFailureClasses ||
		s.UserMessages > MaxEvidenceUserMessages || s.AssistantMessages > MaxEvidenceAssistantMessages ||
		s.UserMessages+s.AssistantMessages+s.FailureClasses > MaxEvidenceExcerptTotal {
		return fmt.Errorf("cloud evidence settings: user messages + assistant messages + failure classes must total at most 20 (failure classes 0–3)")
	}
	if s.ActionSummaries < 0 || s.ActionSummaries > MaxEvidenceActionSummaries ||
		s.ExcerptBytes < MinEvidenceExcerptBytes || s.ExcerptBytes > MaxEvidenceExcerptBytes {
		return fmt.Errorf("cloud evidence settings: action summaries must be 0–256 and excerpt bytes 128–1024")
	}
	return nil
}

// TitlesOnly narrows the selection to what the titles level may carry: at most
// the first prompt, no assistant text, no failure classes. Structural summary
// preferences (actions, milestones, outcomes) are untouched.
func (s EvidenceSettings) TitlesOnly() EvidenceSettings {
	if s.UserMessages > 1 {
		s.UserMessages = 1
	}
	s.AssistantMessages, s.FailureClasses = 0, 0
	return s
}

// ForEvidenceLevel narrows the selection to an org-plane evidence level:
// excerpts keeps it, titles keeps the first prompt only, and structural (or an
// invalid level) carries no text at all.
func (s EvidenceSettings) ForEvidenceLevel(level EvidenceLevel) EvidenceSettings {
	switch level {
	case EvidenceLevelExcerpts:
		return s
	case EvidenceLevelTitles:
		return s.TitlesOnly()
	default:
		s.UserMessages, s.AssistantMessages, s.FailureClasses = 0, 0, 0
		return s
	}
}

// ParseEvidenceSettings returns nil only for legacy (empty) metadata.
// Malformed, partial, null, and future-version settings fail closed.
func ParseEvidenceSettings(raw string) (*EvidenceSettings, error) {
	if raw == "" {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields) != evidenceSettingsFieldCount {
		return nil, fmt.Errorf("cloud evidence settings: all eight fields are required")
	}
	for _, field := range fields {
		if string(field) == "null" {
			return nil, fmt.Errorf("cloud evidence settings: null fields are not allowed")
		}
	}
	var s EvidenceSettings
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return nil, fmt.Errorf("cloud evidence settings: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// JSON returns canonical settings metadata after validation.
func (s EvidenceSettings) JSON() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(s)
	return string(b), err
}
