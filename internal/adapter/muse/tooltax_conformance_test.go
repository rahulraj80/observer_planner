package muse

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/tooltax/conformtest"
)

// WP-T3 conformance pins for an adapter whose private classifier is an
// enumerable map, so both directions walk it directly (shared algorithm in
// internal/tooltax/conformtest). The private classifier is actionMap behind normalizeToolKey.

// TestPrivateMappingAgreesWithTooltax is the TABLE to CLASSIFIER direction.
func TestPrivateMappingAgreesWithTooltax(t *testing.T) {
	conformtest.AgreesWithTable(t, []string{models.ToolMuse}, func(native string) string {
		return actionMap[normalizeToolKey(native)]
	})
}

// TestPrivateActionMapIsSubsetOfTooltax is the CLASSIFIER to TABLE
// direction: every name the map classifies must exist in tooltax with the
// SAME action type.
func TestPrivateActionMapIsSubsetOfTooltax(t *testing.T) {
	conformtest.MapIsSubset(t, models.ToolMuse, actionMap)
}
