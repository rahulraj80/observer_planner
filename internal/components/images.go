package components

import "sort"

// ImageMarkerTag marks an observer-org build as the core image (lane
// MP-ORG): cmd/observer-org's components_build_image_core.go records it, so
// the server resolves its base selection from the core column.
const ImageMarkerTag = "sbo_image_core"

// ImageTags returns the build tags an observer-org image build passes,
// sorted (plan 3.4, lane MP-ORG). The full image passes none: a plain go
// build is the full server. The core image passes ImageMarkerTag plus one
// sbo_omit_<id> tag per org row its column lists as out and that targets
// L1; a tag no file honours yet changes nothing, and the server reports
// such a module as compiled in but inactive. Generated from the catalog,
// never typed by hand (plan 5.1).
func ImageTags(i Image) []string {
	if i != ImageCore {
		return nil
	}
	out := []string{ImageMarkerTag}
	for _, c := range ForBinary(BinaryOrg) {
		if c.Class == ClassCore || c.Images[i] != ImageOut || !c.Levels.Targets(L1) {
			continue
		}
		out = append(out, c.BuildTag())
	}
	sort.Strings(out)
	return out
}
