package main

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// components_require_test.go: the constraints a test of this package carries
// when it exercises a component (lane MP-GOV3, the MP-L1D remainder). The
// omit-tag CI job (scripts/omit-tag-check.sh) runs this package under every
// distro tag set; a stamped distro build (minimal, developer, managed) starts
// an unconfigured node from its distro's default set rather than from every
// compiled component, so a test that needs, say, the MCP server or the
// claude-code adapter active says so here and is skipped (with the reason)
// where the build leaves it inactive. A plain (full) build resolves every
// compiled component active with no [components] block, so none of these ever
// skips there: the full product's coverage is unchanged.

// defaultSnapshot is the component resolution of an unconfigured node in
// this binary (no [components] block, no governance).
func defaultSnapshot() componentstate.Snapshot {
	return componentstate.Resolve(config.ComponentsConfig{}, nil)
}

// defaultActive reports whether id is active on an unconfigured node.
func defaultActive(id string) bool { return defaultSnapshot().Active(id) }

// requireComponents skips t unless every id is active on an unconfigured
// node of this build (compiled in, and in the default set of a stamped
// distro).
func requireComponents(t *testing.T, ids ...string) {
	t.Helper()
	snap := defaultSnapshot()
	for _, id := range ids {
		if !snap.Active(id) {
			t.Skipf("needs component %s, which is not active by default in this build (distro %q)", id, components.BuildDistro())
		}
	}
}

// requireActiveAdapters is requireComponents for adapter rows by tool name.
func requireActiveAdapters(t *testing.T, tools ...string) {
	t.Helper()
	ids := make([]string, 0, len(tools))
	for _, tool := range tools {
		ids = append(ids, components.AdapterID(tool))
	}
	requireComponents(t, ids...)
}

// requireCompiled skips t unless every id is offered by this build (compiled
// in and, in a stamped distro, part of it), whether or not it is active by
// default: for tests that select or govern the component themselves.
func requireCompiled(t *testing.T, ids ...string) {
	t.Helper()
	offered := map[string]bool{}
	for _, id := range components.Compiled(components.BinaryNode) {
		offered[id] = true
	}
	for _, id := range ids {
		if !offered[id] {
			t.Skipf("needs component %s, which this build does not contain (distro %q)", id, components.BuildDistro())
		}
	}
}

// compiledOut reports whether an omit tag removed component id from this
// test binary (its omit file called components.MarkOmitted). Tests use it to
// assert the build's "not in this build" answer instead of the full
// behaviour (D-535).
func compiledOut(id string) bool {
	for _, o := range components.Omitted() {
		if o == id {
			return true
		}
	}
	return false
}

// unstampedBuild reports a plain build (the full product), whose unconfigured
// node keeps the legacy "everything compiled is active" base.
func unstampedBuild() bool { return components.BuildDistro() == "" }

// requireUnstampedBuild skips a test that pins that legacy base, which a
// stamped distro build replaces by design with its distro's default set.
func requireUnstampedBuild(t *testing.T) {
	t.Helper()
	if !unstampedBuild() {
		t.Skipf("pins the plain build's legacy everything-active base; this build is stamped %q", components.BuildDistro())
	}
}

// TestComponentRequirementsNeverSkipAFullBuild pins the promise above: in a
// plain build every compiled component is active by default, so no
// requireComponents call can skip there.
func TestComponentRequirementsNeverSkipAFullBuild(t *testing.T) {
	if !unstampedBuild() || len(components.Omitted()) > 0 {
		t.Skip("a stamped distro build, or a build with an omit tag, may leave a component inactive by design")
	}
	snap := defaultSnapshot()
	for _, id := range components.Compiled(components.BinaryNode) {
		if !snap.Active(id) {
			t.Errorf("%s is compiled but not active by default in a plain build", id)
		}
	}
}
