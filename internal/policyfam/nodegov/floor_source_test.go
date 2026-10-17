package nodegov

import (
	"regexp"
	"testing"
)

// visibilitySourceRel is the SPA module whose FLOOR_NAV_IDS /
// FLOOR_SETTINGS_IDS decide what a managed node's dashboard still offers while
// its posture is unknown (Sol A4-05). They must be exactly this package's
// never-hidden lists: a page missing from the web copy would vanish during a
// pending read, an extra one would be offered before the org's answer.
const visibilitySourceRel = "../../../web/src/lib/visibility.ts"

// floorListRe matches `export const <NAME>: readonly string[] = [ ... ];`.
var (
	floorListRe = regexp.MustCompile(`export const (FLOOR_NAV_IDS|FLOOR_SETTINGS_IDS)\s*:\s*readonly string\[\]\s*=\s*\[([^\]]*)\]`)
	quotedIDRe  = regexp.MustCompile(`"([a-z0-9-]+)"`)
)

// TestWebFloorListsMatchUnhideable pins the SPA's floor constants to
// UnhideableNavSectionIDs / UnhideableSettingsSectionIDs (this package is the
// one owner); it fails when either side drifts.
func TestWebFloorListsMatchUnhideable(t *testing.T) {
	raw := readSPASource(t, visibilitySourceRel)
	lists := map[string][]string{}
	for _, m := range floorListRe.FindAllStringSubmatch(raw, -1) {
		ids := []string{}
		for _, q := range quotedIDRe.FindAllStringSubmatch(m[2], -1) {
			ids = append(ids, q[1])
		}
		lists[m[1]] = ids
	}
	for name, want := range map[string][]string{
		"FLOOR_NAV_IDS":      UnhideableNavSectionIDs,
		"FLOOR_SETTINGS_IDS": UnhideableSettingsSectionIDs,
	} {
		got, ok := lists[name]
		if !ok {
			t.Fatalf("parsed no %s out of %s: the SPA file's shape changed and this test's regex needs updating", name, visibilitySourceRel)
		}
		if len(got) != len(want) {
			t.Errorf("%s in %s has %d ids %v, nodegov has %d %v", name, visibilitySourceRel, len(got), got, len(want), want)
		}
		assertSameSet(t, name, want, got, visibilitySourceRel)
	}
}
