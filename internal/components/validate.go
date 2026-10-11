package components

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// idPattern is the id grammar: lowercase words joined by dots or dashes.
var idPattern = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)

// mandatoryNodeComponents must be compiled into every node distro and may
// never target L1 (rev S1: org.client stays compiled into every distro).
var mandatoryNodeComponents = []string{"core", "org.client"}

// Validate checks the whole catalog and returns every problem found, sorted.
// An empty result means the catalog is well formed. It is the single
// definition of "well formed": the invariant suite and the generator both
// call it, and a release must never ship a catalog with problems.
func Validate() []string {
	return validateRows(All(), Artifacts())
}

func validateRows(rows []Component, arts []ArtifactComponent) []string {
	var p []string
	add := func(format string, a ...any) { p = append(p, fmt.Sprintf(format, a...)) }

	ids := validateIDs(add, rows, arts)
	// Package ownership is unique per binary (a package can serve the node
	// obs component and the org selfobs component, never two node rows).
	owner := map[ownKey]string{}
	for _, c := range rows {
		validateRow(add, c, ids, owner)
	}
	// HostedIn names a package owned by ANOTHER row of the same binary, or
	// an unowned (core) package; never the row's own package. Whether the
	// package exists is a reality test (tests/invariant).
	for _, c := range rows {
		for _, h := range c.HostedIn {
			if owner[ownKey{c.Binary, h}] == c.ID {
				add("%s: hosted-in package %s is its own package", c.ID, h)
			}
		}
	}
	p = append(p, validateCycles(rows)...)
	p = append(p, validateDistroConsistency(rows)...)
	p = append(p, validateBundles(rows)...)
	validateArtifacts(add, arts, ids)
	sort.Strings(p)
	return p
}

// ownKey is one (binary, package) ownership slot.
type ownKey struct {
	b   Binary
	pkg string
}

// validateIDs checks id grammar and uniqueness across components and
// artifacts, and returns the component index.
func validateIDs(add func(string, ...any), rows []Component, arts []ArtifactComponent) map[string]Component {
	ids := map[string]Component{}
	for _, c := range rows {
		if !idPattern.MatchString(c.ID) {
			add("%s: id does not match %s", c.ID, idPattern)
		}
		if _, dup := ids[c.ID]; dup {
			add("%s: duplicate id", c.ID)
		}
		ids[c.ID] = c
	}
	for _, a := range arts {
		if _, dup := ids[a.ID]; dup {
			add("%s: artifact id collides with a component id", a.ID)
		}
		if !strings.HasPrefix(a.ID, "artifact.") {
			add("%s: artifact ids start with artifact.", a.ID)
		}
	}
	return ids
}

// validateRow runs every single-row rule and records package ownership.
func validateRow(add func(string, ...any), c Component, ids map[string]Component, owner map[ownKey]string) {
	checkCopy(add, c.ID, "title", c.Title)
	checkCopy(add, c.ID, "about", c.About)
	checkCopy(add, c.ID, "note", c.Note)
	validateVocabulary(add, c)
	validateLevels(add, c)
	for _, pkg := range c.Packages {
		k := ownKey{c.Binary, pkg}
		if prev, ok := owner[k]; ok {
			add("%s: package %s already owned by %s", c.ID, pkg, prev)
		}
		owner[k] = c.ID
		if strings.HasPrefix(pkg, modulePath) || strings.HasPrefix(pkg, "/") {
			add("%s: package %s must be relative to the module root", c.ID, pkg)
		}
	}
	// A verb or a route prefix has one owner per binary (T-661, T-671): the
	// verb gate and the dashboard route guard read ownership from these
	// lists, so a second claimant would make the owner depend on map order.
	for _, v := range c.Verbs {
		k := ownKey{c.Binary, "verb " + v}
		if prev, ok := owner[k]; ok {
			add("%s: verb %s already owned by %s", c.ID, v, prev)
		}
		owner[k] = c.ID
	}
	for _, r := range c.Routes {
		k := ownKey{c.Binary, "route " + strings.TrimSuffix(r, "/")}
		if prev, ok := owner[k]; ok {
			add("%s: route %s already owned by %s", c.ID, r, prev)
		}
		owner[k] = c.ID
	}
	validateRefs(add, c, ids)
	validateSurface(add, c)
	validateData(add, c)
	switch c.Binary {
	case BinaryNode:
		validateDistros(add, c)
		if c.Images != nil {
			add("%s: node components carry distro cells, not image cells", c.ID)
		}
	default:
		if c.Distros != nil {
			add("%s: server components carry image cells, not distro cells", c.ID)
		}
		validateImages(add, c)
	}
}

// validateVocabulary checks the closed-vocabulary fields and the
// class/binary pairing.
func validateVocabulary(add func(string, ...any), c Component) {
	if !c.Class.Valid() {
		add("%s: unknown class %q", c.ID, c.Class)
	}
	if !c.Binary.Valid() {
		add("%s: unknown binary %q", c.ID, c.Binary)
	}
	if !c.Stability.Valid() {
		add("%s: unknown stability %q", c.ID, c.Stability)
	}
	if c.Class == ClassAdapter && c.Binary != BinaryNode {
		add("%s: adapters are node components", c.ID)
	}
	if c.Class == ClassServer && c.Binary == BinaryNode {
		add("%s: server components are not node components", c.ID)
	}
	if c.Class == ClassAdapter && !strings.HasPrefix(c.ID, "adapter.") {
		add("%s: adapter ids start with adapter.", c.ID)
	}
	if c.RestartClass != "" && c.RestartClass != "hot" && c.RestartClass != "soft" && c.RestartClass != "restart" {
		add("%s: restart class %q is not hot, soft or restart", c.ID, c.RestartClass)
	}
}

// validateData checks migration classes and data ownership declarations.
func validateData(add func(string, ...any), c Component) {
	for _, m := range c.Migrations {
		if !m.Class.Valid() {
			add("%s: migration %s has unknown class %q", c.ID, m.File, m.Class)
		}
	}
	for _, d := range c.Data {
		if d.Ownership != "exclusive" && d.Ownership != "shared" {
			add("%s: data ownership for %s must be exclusive or shared", c.ID, d.Table)
		}
		if d.Ownership == "shared" && d.Selector == "" {
			add("%s: shared table %s needs a row selector", c.ID, d.Table)
		}
	}
}

// validateArtifacts checks the external artifact rows.
func validateArtifacts(add func(string, ...any), arts []ArtifactComponent, ids map[string]Component) {
	for _, a := range arts {
		checkCopy(add, a.ID, "title", a.Title)
		checkCopy(add, a.ID, "about", a.About)
		switch a.Kind {
		case ArtifactBrowserExtension, ArtifactHelperBinary, ArtifactEditorExtension, ArtifactPluginShim:
		default:
			add("%s: unknown artifact kind %q", a.ID, a.Kind)
		}
		for field, v := range map[string]string{"not_installed": a.NotInstalled, "removal": a.Removal, "lockstep": a.Lockstep} {
			checkCopy(add, a.ID, field, v)
			if strings.TrimSpace(v) == "" {
				add("%s: an artifact says what %s means (lane MP-ART)", a.ID, field)
			}
		}
		if len(a.States) == 0 {
			add("%s: an artifact needs its state vocabulary", a.ID)
		}
		for _, r := range a.Requires {
			if _, ok := ids[r]; !ok {
				add("%s: requires unknown component %s", a.ID, r)
			}
		}
	}
}

// checkCopy enforces the operator's no em or en dash copy rule.
func checkCopy(add func(string, ...any), id, field, s string) {
	if strings.ContainsAny(s, "–—") {
		add("%s: %s contains an em or en dash", id, field)
	}
}

func validateLevels(add func(string, ...any), c Component) {
	for _, l := range append(append([]Level(nil), c.Levels.Implemented...), c.Levels.Target...) {
		switch l {
		case L0, L1, L2, L3:
		default:
			add("%s: unknown level %q", c.ID, l)
		}
	}
	if c.Class == ClassCore {
		if !c.Levels.Has(L0) || !c.Levels.Targets(L0) {
			add("%s: core rows are L0", c.ID)
		}
		return
	}
	if c.Levels.Has(L0) || c.Levels.Targets(L0) {
		add("%s: only core rows are L0", c.ID)
	}
	// Implemented must be a subset of Target: the plan never removes a
	// level that ships.
	for _, l := range c.Levels.Implemented {
		if !c.Levels.Targets(l) {
			add("%s: implemented level %s is missing from the target set", c.ID, l)
		}
	}
	missing := false
	for _, l := range c.Levels.Target {
		if !c.Levels.Has(l) {
			missing = true
		}
	}
	if missing && c.Levels.Phase == "" {
		add("%s: a target level not yet implemented needs the phase that delivers it", c.ID)
	}
	if !missing && c.Levels.Phase != "" {
		add("%s: phase %s named but every target level is implemented", c.ID, c.Levels.Phase)
	}
	if c.Binary == BinaryNode {
		for _, m := range mandatoryNodeComponents {
			if c.ID == m && c.Levels.Targets(L1) {
				add("%s: must never target L1 (rev S1)", c.ID)
			}
		}
	}
}

func validateRefs(add func(string, ...any), c Component, ids map[string]Component) {
	check := func(kind string, refs []string) {
		seen := map[string]bool{}
		for _, r := range refs {
			if seen[r] {
				add("%s: %s lists %s twice", c.ID, kind, r)
			}
			seen[r] = true
			if r == c.ID {
				add("%s: %s itself", c.ID, kind)
				continue
			}
			t, ok := ids[r]
			if !ok {
				add("%s: %s unknown component %s", c.ID, kind, r)
				continue
			}
			if t.Binary != c.Binary {
				add("%s: %s %s from another binary", c.ID, kind, r)
			}
		}
	}
	check("requires", c.Requires)
	check("requires_compiled", c.RequiresCompiled)
	check("bundles", c.Bundles)
	check("recommends", c.Recommends)
	check("conflicts", c.Conflicts)
	for _, r := range c.Requires {
		for _, k := range c.Conflicts {
			if r == k {
				add("%s: both requires and conflicts with %s", c.ID, r)
			}
		}
	}
}

func validateSurface(add func(string, ...any), c Component) {
	for _, l := range c.Surface.Listeners {
		if l.Proto != "tcp" && l.Proto != "unix" && l.Proto != "pipe" {
			add("%s: listener %s has unknown proto %q", c.ID, l.Addr, l.Proto)
		}
		if l.Addr == "" || l.Auth == "" {
			add("%s: listener needs an address and its auth", c.ID)
		}
		if !l.Trigger.Valid() {
			add("%s: listener %s has unknown trigger %q", c.ID, l.Addr, l.Trigger)
		}
		if l.Proto == "tcp" && l.Loopback && !strings.HasPrefix(l.Addr, "127.0.0.1:") {
			add("%s: loopback listener %s is not on 127.0.0.1", c.ID, l.Addr)
		}
		checkCopy(add, c.ID, "listener", l.Auth)
	}
	for _, e := range c.Surface.Egress {
		if e.Host == "" || e.Purpose == "" || e.Gate == "" {
			add("%s: egress needs host, purpose and gate", c.ID)
		}
		if !e.Trigger.Valid() {
			add("%s: egress %s has unknown trigger %q", c.ID, e.Host, e.Trigger)
		}
		checkCopy(add, c.ID, "egress", e.Purpose)
	}
}

func validateDistros(add func(string, ...any), c Component) {
	for _, d := range Distros() {
		v, ok := c.Distros[d]
		if !ok {
			add("%s: missing the %s distro cell", c.ID, d)
			continue
		}
		if !v.Valid() {
			add("%s: unknown %s distro cell %q", c.ID, d, v)
		}
	}
	if len(c.Distros) != len(Distros()) {
		add("%s: distro cells must be exactly %v", c.ID, Distros())
	}
	if c.Class == ClassCore && len(c.Distros) > 0 {
		for _, d := range Distros() {
			if c.Distros[d] != DefaultOn {
				add("%s: core is on in every distro", c.ID)
			}
		}
	}
	for _, m := range mandatoryNodeComponents {
		if c.ID != m {
			continue
		}
		for _, d := range Distros() {
			if !c.Distros[d].Compiled() {
				add("%s: must be compiled into the %s distro (rev S1)", c.ID, d)
			}
		}
	}
	// "full" is everything, as shipped today.
	if v := c.Distros[DistroFull]; v == DefaultOut {
		add("%s: the full distro compiles everything in", c.ID)
	}
}

func validateImages(add func(string, ...any), c Component) {
	for _, i := range Images() {
		v, ok := c.Images[i]
		if !ok {
			add("%s: missing the %s image cell", c.ID, i)
			continue
		}
		if !v.Valid() {
			add("%s: unknown %s image cell %q", c.ID, i, v)
		}
	}
	if c.Class == ClassCore && (c.Images[ImageFull] != ImageIn || c.Images[ImageCore] != ImageIn) {
		add("%s: server core is in every image", c.ID)
	}
	if c.Binary != BinaryOrg && c.Images[ImageFull] != ImageSeparate {
		add("%s: a component outside observer-org ships as a separate image", c.ID)
	}
}

// validateCycles rejects a requires (or requires_compiled) cycle: a catalog
// bug that fails the build (plan 3.1).
func validateCycles(rows []Component) []string {
	edges := map[string][]string{}
	for _, c := range rows {
		edges[c.ID] = append(append([]string(nil), c.Requires...), c.RequiresCompiled...)
	}
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var p []string
	var visit func(id string, path []string)
	visit = func(id string, path []string) {
		color[id] = grey
		for _, n := range edges[id] {
			switch color[n] {
			case grey:
				p = append(p, fmt.Sprintf("%s: requires cycle %s", id, strings.Join(append(path, id, n), " -> ")))
			case white:
				visit(n, append(path, id))
			}
		}
		color[id] = black
	}
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			visit(id, nil)
		}
	}
	return p
}

// validateDistroConsistency checks every prebuilt distro: a compiled
// component's requires and requires_compiled are compiled too, and a
// default-on component's requires are on or conditional.
func validateDistroConsistency(rows []Component) []string {
	by := map[string]Component{}
	for _, c := range rows {
		by[c.ID] = c
	}
	var p []string
	for _, c := range rows {
		if c.Binary != BinaryNode {
			continue
		}
		for _, d := range Distros() {
			cell := c.Distros[d]
			if !cell.Compiled() {
				// Only an L1-target component can leave a binary, unless it
				// leaves with something it needs (proxy.compression goes
				// where capture.proxy goes).
				if !c.Levels.Targets(L1) && !needsSomethingOut(by, c, d) {
					p = append(p, fmt.Sprintf("%s: compiled out of %s, but it does not target L1 and nothing it needs is out", c.ID, d))
				}
				continue
			}
			for _, r := range append(append([]string(nil), c.Requires...), c.RequiresCompiled...) {
				if !by[r].Distros[d].Compiled() {
					p = append(p, fmt.Sprintf("%s: compiled into %s but needs %s, which %s compiles out", c.ID, d, r, d))
				}
			}
			if cell == DefaultOn {
				for _, r := range c.Requires {
					if v := by[r].Distros[d]; v != DefaultOn && v != DefaultCond {
						p = append(p, fmt.Sprintf("%s: on by default in %s but requires %s, which is %s there", c.ID, d, r, v))
					}
				}
			}
		}
	}
	return p
}

// needsSomethingOut reports whether c (transitively) requires, needs the
// code of, or is bundled with a component distro d compiles out.
func needsSomethingOut(by map[string]Component, c Component, d Distro) bool {
	seen := map[string]bool{c.ID: true}
	var walk func(x Component) bool
	walk = func(x Component) bool {
		for _, r := range append(append(append([]string(nil), x.Requires...), x.RequiresCompiled...), x.Bundles...) {
			if seen[r] {
				continue
			}
			seen[r] = true
			t, ok := by[r]
			if !ok {
				continue
			}
			if !t.Distros[d].Compiled() && t.Levels.Targets(L1) {
				return true
			}
			if walk(t) {
				return true
			}
		}
		return false
	}
	return walk(c)
}

// validateBundles checks that bundles are symmetric and share their distro
// cells' compiled-ness: bundled ids are compiled together.
func validateBundles(rows []Component) []string {
	by := map[string]Component{}
	for _, c := range rows {
		by[c.ID] = c
	}
	var p []string
	for _, c := range rows {
		for _, b := range c.Bundles {
			o, ok := by[b]
			if !ok {
				continue
			}
			back := false
			for _, x := range o.Bundles {
				if x == c.ID {
					back = true
				}
			}
			// capture.browser bundles the web adapters; the adapters name
			// it through requires (they cannot run without the host), so
			// the relation is symmetric through either field.
			for _, x := range o.Requires {
				if x == c.ID {
					back = true
				}
			}
			if !back {
				p = append(p, fmt.Sprintf("%s: bundles %s but %s does not bundle or require it back", c.ID, b, b))
			}
			if c.Binary == BinaryNode {
				for _, d := range Distros() {
					if c.Distros[d].Compiled() != o.Distros[d].Compiled() {
						p = append(p, fmt.Sprintf("%s: bundled with %s but the %s distro compiles only one of them", c.ID, b, d))
					}
				}
			}
		}
	}
	return p
}

// ValidateSelection checks a component id set offered as a distro or a
// custom manifest for one binary (plan 5.3): every id exists and belongs to
// the binary, the mandatory components are present, and every requires and
// requires_compiled is included. It returns the problems, sorted.
func ValidateSelection(b Binary, compiled []string) []string {
	var p []string
	set := map[string]bool{}
	for _, id := range compiled {
		c, ok := Get(id)
		switch {
		case !ok:
			p = append(p, fmt.Sprintf("%s: unknown component", id))
			continue
		case c.Binary != b:
			p = append(p, fmt.Sprintf("%s: belongs to the %s binary, not %s", id, c.Binary, b))
			continue
		}
		set[id] = true
	}
	if b == BinaryNode {
		for _, m := range mandatoryNodeComponents {
			if !set[m] {
				p = append(p, fmt.Sprintf("%s: every node build includes it (rev S1)", m))
			}
		}
	}
	for _, c := range ForBinary(b) {
		if c.Class == ClassCore && !set[c.ID] && b != BinaryNode {
			p = append(p, fmt.Sprintf("%s: every %s build includes core", c.ID, b))
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c, _ := Get(id)
		for _, r := range append(append([]string(nil), c.Requires...), c.RequiresCompiled...) {
			if !set[r] {
				p = append(p, fmt.Sprintf("%s: needs %s, which the selection leaves out", id, r))
			}
		}
		for _, bd := range c.Bundles {
			if !set[bd] {
				p = append(p, fmt.Sprintf("%s: is compiled together with %s, which the selection leaves out", id, bd))
			}
		}
	}
	sort.Strings(p)
	return p
}
