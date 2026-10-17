package store

import "github.com/marmutapp/superbased-observer/internal/cloudcontract"

// CloudEvidenceSettings is the node-local, versioned selection policy. Counts
// are maxima; zero excludes a category. UserMessages includes the first prompt.
// It contains no content and is frozen on each per-upload receipt.
//
// It IS cloudcontract.EvidenceSettings (Cloud Intelligence parity, phase 4):
// the org plane stores and hands out the same shape, and one validator in the
// contract package means a node and an org can never accept different ones.
type CloudEvidenceSettings = cloudcontract.EvidenceSettings

// DefaultCloudEvidenceSettings preserves the previous excerpt selection.
func DefaultCloudEvidenceSettings() CloudEvidenceSettings {
	return cloudcontract.DefaultEvidenceSettings()
}

// CloudEvidenceSettingsForLevel narrows content to the selected consent level.
// Structural summary preferences remain available at either level; off never
// schedules work.
func CloudEvidenceSettingsForLevel(s CloudEvidenceSettings, level CloudEnrichLevel) CloudEvidenceSettings {
	if level == CloudEnrichTitles {
		return s.TitlesOnly()
	}
	return s
}

// ParseCloudEvidenceSettings returns nil only for legacy (empty) metadata.
// Malformed, partial, null, and future-version settings fail closed.
func ParseCloudEvidenceSettings(raw string) (*CloudEvidenceSettings, error) {
	return cloudcontract.ParseEvidenceSettings(raw)
}
