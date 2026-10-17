package components

import (
	"sort"
	"sync"
)

// compiled.go is the running binary's own report of what it contains, so
// the binary can state its compiled set without trusting build flags
// (plan 5.1).
//
// P0 shape: nothing is compiled out yet except through the pre-existing
// no_obs tag, so the registry records OMISSIONS. A build-tagged file in the
// binary's main package calls MarkOmitted from init() for each component
// its tags remove (cmd/observer/components_build_*.go). Phase P2 switches
// to positive marking from each component's wiring file (MarkCompiled in
// the on file), at which point this registry becomes the proof the
// self-check compares with the manifest's components digest.

var (
	omitMu  sync.Mutex
	omitted = map[string]bool{}
)

// MarkOmitted records that this binary was built without a component. It
// is called from init() in a build-tagged file and is safe for concurrent
// use. Unknown ids are recorded as given; Compiled ignores them.
func MarkOmitted(id string) {
	omitMu.Lock()
	defer omitMu.Unlock()
	omitted[id] = true
}

// Omitted returns the ids this binary was built without, sorted.
func Omitted() []string {
	omitMu.Lock()
	defer omitMu.Unlock()
	out := make([]string, 0, len(omitted))
	for id := range omitted {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// SettingsHint names where a person changes the component selection: the
// dashboard's Settings > Components page, or, in a binary built without
// the dashboard (ui.dashboard omitted, lane MP-L1M), the CLI that does the
// same thing. Refusal and skip messages use it so they never point at a
// page this build does not have (D-514).
func SettingsHint() string {
	omitMu.Lock()
	defer omitMu.Unlock()
	if omitted["ui.dashboard"] {
		return "`observer components status`"
	}
	return "Settings > Components"
}

// WhereToChange is the "how do I change it" clause for an action whose CLI
// verb is cli (already quoted, for example "`observer components add x`"):
// "in Settings > Components or with <cli>" when the dashboard is in the
// build, "with <cli>" when it is not (REVIEW-4 P4-19, D-514). Every
// user-facing pointer at the Components page goes through this or
// SettingsHint, so none names a page the binary lacks.
func WhereToChange(cli string) string {
	omitMu.Lock()
	defer omitMu.Unlock()
	if omitted["ui.dashboard"] {
		return "with " + cli
	}
	return "in Settings > Components or with " + cli
}

// buildDistro is the prebuilt distro this node binary was built as, "" for
// an unstamped build (today's release, the full product).
var buildDistro Distro

// SetBuildDistro stamps the prebuilt node distro this binary was built as.
// It is called once from init() in cmd/observer/components_distro_<name>.go,
// selected by the build tag sbo_distro_<name> that DistroTags adds (phase
// P3, lane MP-REL); nothing calls it at run time. full is never stamped: a
// full build is today's unstamped build, byte for byte, so an invalid or
// full value is ignored.
func SetBuildDistro(d Distro) {
	if !d.Valid() || d == DistroFull {
		return
	}
	omitMu.Lock()
	defer omitMu.Unlock()
	buildDistro = d
}

// BuildDistro returns the stamped distro, "" when the binary is unstamped.
func BuildDistro() Distro {
	omitMu.Lock()
	defer omitMu.Unlock()
	return buildDistro
}

// Compiled returns the ids of one binary's rows that this process was
// built with and that its distro offers, sorted. In a stamped node build a
// component the distro lists `out` is not offered even while its code is
// still in the bytes (its L1 wiring has not landed yet, so it is L2 only):
// the resolver treats it as not compiled, so no selection can activate it.
// DistroOut names those ids, so no surface claims they left the binary.
func Compiled(b Binary) []string {
	var offered map[string]bool
	if b == BinaryNode {
		// A custom build (custom.go) offers its manifest's set; read before
		// taking the lock, BuildCustom consults BuildDistro.
		offered = customOffered()
	}
	omitMu.Lock()
	defer omitMu.Unlock()
	if b == BinaryNode && buildDistro != "" {
		offered = toSet(InDistro(buildDistro))
	}
	var out []string
	for _, c := range ForBinary(b) {
		if omitted[c.ID] || (offered != nil && !offered[c.ID]) {
			continue
		}
		out = append(out, c.ID)
	}
	sort.Strings(out)
	return out
}

// DistroOut returns the node ids the stamped distro lists `out` whose code
// this binary still carries (no omit file marked them), sorted; nil for an
// unstamped build. They are inert and unselectable (L2), not compiled out
// (L1): the per-artifact proof gate (sbo-components proof) reports them and
// fails if one of them claims L1.
func DistroOut() []string {
	custom := customOffered()
	omitMu.Lock()
	defer omitMu.Unlock()
	offered := custom
	if buildDistro != "" {
		offered = toSet(InDistro(buildDistro))
	}
	if offered == nil {
		return nil
	}
	var out []string
	for _, c := range ForBinary(BinaryNode) {
		if !offered[c.ID] && !omitted[c.ID] {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// DistroCustom is reported when a node's compiled set matches no prebuilt
// distro.
const DistroCustom Distro = "custom"

// DistroOf names the prebuilt node distro whose compiled set equals ids,
// or DistroCustom.
func DistroOf(ids []string) Distro {
	got := Digest(ids)
	for _, d := range Distros() {
		if Digest(InDistro(d)) == got {
			return d
		}
	}
	return DistroCustom
}
