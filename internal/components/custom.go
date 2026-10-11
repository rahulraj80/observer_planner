package components

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// custom.go is the custom distro of modular packaging phase P7 (plan 5.3,
// lane MP-CUST): a component manifest (`sbo-distro.v1`) names a prebuilt
// base distro and adds or omits node components; the catalog resolves it to
// a compiled set, the build tags, the proof expectations and the stamp the
// binary carries so it can say what it is.
//
// It is pure like the rest of the package. The TOML file is decoded by the
// caller (cmd/sbo-components); this file sees a Manifest value, and the one
// byte form it defines (CanonicalManifest) is what the digest, the stamp and
// the custom distro id are computed from.
//
// How a custom build identifies itself:
//   - its components report says distro "custom" (DistroOf: the compiled
//     set matches no prebuilt distro, which ResolveManifest guarantees) and
//     the components digest of that set, like every build;
//   - it also reports the custom distro id `custom-<manifest digest>` (the
//     full SHA-256 of the canonical manifest, never a prefix, so two
//     manifests can never share an id). The update rail selects artifacts by
//     that id and nothing else (internal/update, distro.go): a custom build
//     updates only to the same manifest's build for the new version, named by
//     a vendor-signed release index, or it refuses.

// ManifestSchemaV1 is the only component manifest schema.
const ManifestSchemaV1 = "sbo-distro.v1"

// CustomDistroPrefix starts every custom distro id.
const CustomDistroPrefix = "custom-"

// CustomStampVar is the linker symbol a custom build stamps with
// `-X <CustomStampVar>=<stamp>` (CustomBuild.LDFlag). The value is the
// canonical manifest, base64url without padding; the binary decodes and
// resolves it itself, so it never trusts a list handed to it.
const CustomStampVar = "github.com/marmutapp/superbased-observer/internal/components.customManifest"

// customManifest is set only by the linker in a custom build (see
// CustomStampVar). Empty in every other build.
var customManifest string

// Manifest is one component manifest. Name is a label for people; it is
// carried in the stamp but is NOT part of the digest, so two manifests that
// differ only in name are the same build.
type Manifest struct {
	Schema string   `json:"schema" toml:"schema"`
	Name   string   `json:"name,omitempty" toml:"name"`
	Base   Distro   `json:"base" toml:"base"`
	Add    []string `json:"add,omitempty" toml:"add"`
	Omit   []string `json:"omit,omitempty" toml:"omit"`
}

// CustomBuild is a resolved manifest: everything a build, a proof, an index
// entry and the running binary need.
type CustomBuild struct {
	// Manifest is the normalized manifest (ids trimmed and sorted).
	Manifest Manifest `json:"manifest"`
	// ManifestDigest is the hex SHA-256 of the canonical manifest without
	// its name (ManifestDigest).
	ManifestDigest string `json:"manifest_digest"`
	// Distro is the custom distro id, CustomDistroPrefix + ManifestDigest.
	Distro string `json:"distro"`
	// Compiled is the compiled node id set, sorted.
	Compiled []string `json:"compiled"`
	// ComponentsDigest is Digest(Compiled).
	ComponentsDigest string `json:"components_digest"`
	// CatalogVersion is the catalog the set was resolved under.
	CatalogVersion int `json:"catalog_version"`
	// Tags are the omit tags the build passes, sorted.
	Tags []string `json:"tags"`
	// AbsentL1 are the excluded components that claim L1 (their packages
	// must be absent); L2Only are the excluded ones whose code this release
	// still carries (inert and unselectable through the stamp).
	AbsentL1 []string `json:"absent_l1"`
	L2Only   []string `json:"l2_only"`
	// DefaultSelection is what an unconfigured node built from this manifest
	// starts with: the base distro's on and cond cells it compiles, plus
	// every added component, plus core.
	DefaultSelection []string `json:"default_selection"`
	// Stamp is the -X value (CanonicalManifest with the name, base64url).
	Stamp string `json:"stamp"`
}

// LDFlag is the one linker flag that stamps a custom build.
func (cb CustomBuild) LDFlag() string { return "-X " + CustomStampVar + "=" + cb.Stamp }

// CustomManifestFileName is the canonical manifest file a custom build's
// outputs and its air-gap bundle carry beside the archives
// (`<distro>.sbo-distro.json`); `vendorsign index` reads it to compute the
// custom entries' components digest.
func CustomManifestFileName(distro string) string { return distro + ".sbo-distro.json" }

var (
	// ErrManifestInvalid wraps every manifest problem.
	ErrManifestInvalid = errors.New("components: the component manifest is invalid")
	customIDRe         = regexp.MustCompile(`^custom-[0-9a-f]{64}$`)
	manifestNameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
)

// IsCustomDistro reports whether d is a custom distro id.
func IsCustomDistro(d string) bool { return customIDRe.MatchString(d) }

// normalize trims and sorts the manifest's lists (duplicates are kept so the
// rules can report them).
func (m Manifest) normalize() Manifest {
	n := Manifest{Schema: strings.TrimSpace(m.Schema), Name: strings.TrimSpace(m.Name), Base: Distro(strings.TrimSpace(string(m.Base)))}
	for _, id := range m.Add {
		n.Add = append(n.Add, strings.TrimSpace(id))
	}
	for _, id := range m.Omit {
		n.Omit = append(n.Omit, strings.TrimSpace(id))
	}
	sort.Strings(n.Add)
	sort.Strings(n.Omit)
	return n
}

// CanonicalManifest is the one byte form of a manifest: compact JSON, fixed
// field order, sorted lists, empty lists spelled []. withName false drops
// the label (the digest input).
func CanonicalManifest(m Manifest, withName bool) []byte {
	n := m.normalize()
	type body struct {
		Schema string   `json:"schema"`
		Name   string   `json:"name,omitempty"`
		Base   Distro   `json:"base"`
		Add    []string `json:"add"`
		Omit   []string `json:"omit"`
	}
	b := body{Schema: n.Schema, Base: n.Base, Add: nonNil(n.Add), Omit: nonNil(n.Omit)}
	if withName {
		b.Name = n.Name
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(b) // only strings and string slices: cannot fail
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// ManifestDigest is the hex SHA-256 of the domain tag and the canonical
// manifest without its name.
func ManifestDigest(m Manifest) string {
	h := sha256.New()
	h.Write([]byte(ManifestSchemaV1 + "\n"))
	h.Write(CanonicalManifest(m, false))
	return hex.EncodeToString(h.Sum(nil))
}

// DecodeManifestJSON parses a canonical (or any JSON) manifest, refusing
// unknown fields and trailing data.
func DecodeManifestJSON(b []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrManifestInvalid, err)
	}
	if dec.More() {
		return Manifest{}, fmt.Errorf("%w: trailing JSON value", ErrManifestInvalid)
	}
	return m, nil
}

// manifestCtx is the state the manifest rules read and fill.
type manifestCtx struct {
	m        Manifest
	compiled map[string]bool
}

// manifestRule is one row of the ordered manifest rule table. A row that
// returns problems stops the walk when stop is true: later rows assume the
// earlier ones held (CLAUDE.md rule 5, table-driven).
type manifestRule struct {
	name  string
	stop  bool
	check func(*manifestCtx) []string
}

// manifestRules is walked top-down by ResolveManifest.
var manifestRules = []manifestRule{
	{"schema", true, func(c *manifestCtx) []string {
		if c.m.Schema != ManifestSchemaV1 {
			return []string{fmt.Sprintf("schema %q: want %q", c.m.Schema, ManifestSchemaV1)}
		}
		return nil
	}},
	{"name", false, func(c *manifestCtx) []string {
		if c.m.Name != "" && !manifestNameRe.MatchString(c.m.Name) {
			return []string{fmt.Sprintf("name %q: letters, digits, space, dot, dash and underscore, at most 64", c.m.Name)}
		}
		return nil
	}},
	{"base", false, func(c *manifestCtx) []string {
		if !c.m.Base.Valid() {
			return []string{fmt.Sprintf("base %q: want one of minimal, developer, managed, full", c.m.Base)}
		}
		return nil
	}},
	{"ids", true, checkManifestIDs},
	{"selection", true, func(c *manifestCtx) []string {
		c.compiled = toSet(InDistro(c.m.Base))
		for _, id := range c.m.Add {
			c.compiled[id] = true
		}
		for _, id := range c.m.Omit {
			delete(c.compiled, id)
		}
		var p []string
		for _, s := range ValidateSelection(BinaryNode, sortedKeys(c.compiled)) {
			p = append(p, s+closureHint(s))
		}
		return p
	}},
	{"not-prebuilt", true, func(c *manifestCtx) []string {
		got := Digest(sortedKeys(c.compiled))
		for _, d := range Distros() {
			if Digest(InDistro(d)) == got {
				return []string{fmt.Sprintf("the manifest resolves to exactly the prebuilt %s distro: install that distro instead of a custom build", d)}
			}
		}
		return nil
	}},
}

// closureHint names the fix for a selection problem.
func closureHint(problem string) string {
	switch {
	case strings.Contains(problem, "which the selection leaves out"):
		return " (add it, or omit the component that needs it)"
	case strings.Contains(problem, "every node build includes it"):
		return " (it can never be omitted)"
	}
	return ""
}

// checkManifestIDs is the ids row: every add and omit id is a known,
// non-core node component, listed once, never in both lists, an add is not
// already in the base and an omit is in it.
func checkManifestIDs(c *manifestCtx) []string {
	var p []string
	base := toSet(InDistro(c.m.Base))
	seen := map[string]string{}
	check := func(list, id string) {
		comp, ok := Get(id)
		switch {
		case id == "":
			p = append(p, list+": an empty id")
			return
		case !ok:
			p = append(p, fmt.Sprintf("%s %q: not a component (sbo-components ids --distro full lists them)", list, id))
			return
		case comp.Binary != BinaryNode:
			p = append(p, fmt.Sprintf("%s %q: an org server component; custom builds are node builds", list, id))
			return
		case comp.Class == ClassCore:
			p = append(p, fmt.Sprintf("%s %q: core is in every build", list, id))
			return
		}
		if prev, dup := seen[id]; dup {
			if prev == list {
				p = append(p, fmt.Sprintf("%s %q: listed twice", list, id))
			} else {
				p = append(p, fmt.Sprintf("%q: in both add and omit", id))
			}
			return
		}
		seen[id] = list
		switch {
		case list == "add" && base[id]:
			p = append(p, fmt.Sprintf("add %q: the %s base already compiles it", id, c.m.Base))
		case list == "omit" && !base[id]:
			p = append(p, fmt.Sprintf("omit %q: the %s base does not compile it", id, c.m.Base))
		}
	}
	for _, id := range c.m.Add {
		check("add", id)
	}
	for _, id := range c.m.Omit {
		check("omit", id)
	}
	return p
}

// ResolveManifest checks a manifest through the catalog and resolves it. It
// refuses an inconsistent set and names why (plan 5.3); problems are sorted
// within each rule, in rule order.
func ResolveManifest(m Manifest) (CustomBuild, []string) {
	c := &manifestCtx{m: m.normalize()}
	var problems []string
	for _, r := range manifestRules {
		p := r.check(c)
		sort.Strings(p)
		problems = append(problems, p...)
		if len(problems) > 0 && r.stop {
			return CustomBuild{}, problems
		}
	}
	if len(problems) > 0 {
		return CustomBuild{}, problems
	}
	cb := CustomBuild{
		Manifest:       c.m,
		ManifestDigest: ManifestDigest(c.m),
		Compiled:       sortedKeys(c.compiled),
		CatalogVersion: CatalogVersion,
		Tags:           []string{},
		AbsentL1:       []string{},
		L2Only:         []string{},
		Stamp:          base64.RawURLEncoding.EncodeToString(CanonicalManifest(c.m, true)),
	}
	cb.Distro = CustomDistroPrefix + cb.ManifestDigest
	cb.ComponentsDigest = Digest(cb.Compiled)
	add := toSet(c.m.Add)
	sel := map[string]bool{"core": true}
	for _, comp := range ForBinary(BinaryNode) {
		if comp.Class == ClassCore {
			continue
		}
		if !c.compiled[comp.ID] {
			cb.Tags = append(cb.Tags, comp.BuildTag())
			if comp.Levels.Has(L1) {
				cb.AbsentL1 = append(cb.AbsentL1, comp.ID)
			} else {
				cb.L2Only = append(cb.L2Only, comp.ID)
			}
			continue
		}
		switch {
		case add[comp.ID]:
			sel[comp.ID] = true
		case comp.Distros[c.m.Base] == DefaultOn || comp.Distros[c.m.Base] == DefaultCond:
			sel[comp.ID] = true
		}
	}
	sort.Strings(cb.Tags)
	sort.Strings(cb.AbsentL1)
	sort.Strings(cb.L2Only)
	cb.DefaultSelection = sortedKeys(sel)
	return cb, nil
}

// DecodeCustomStamp decodes and resolves a stamp value (the binary's own
// path, and the proof's).
func DecodeCustomStamp(stamp string) (CustomBuild, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(stamp))
	if err != nil {
		return CustomBuild{}, fmt.Errorf("%w: stamp encoding: %w", ErrManifestInvalid, err)
	}
	m, err := DecodeManifestJSON(raw)
	if err != nil {
		return CustomBuild{}, err
	}
	cb, problems := ResolveManifest(m)
	if len(problems) > 0 {
		return CustomBuild{}, fmt.Errorf("%w: %s", ErrManifestInvalid, strings.Join(problems, "; "))
	}
	return cb, nil
}

var (
	customOnce  sync.Once
	customBuild CustomBuild
	customErr   error
	customSet   bool
)

// customStampValue is customManifest, a variable so tests can stamp one.
var customStampValue = func() string { return customManifest }

// BuildCustom returns this binary's custom build. stamped is false in every
// non-custom build. A stamped binary whose stamp does not decode or resolve
// returns the error: Compiled then offers only the mandatory components
// (fail closed, the ceiling-invalid precedent), and the report says why.
// A build also stamped as a prebuilt distro ignores the custom stamp.
func BuildCustom() (cb CustomBuild, stamped bool, err error) {
	customOnce.Do(func() {
		v := customStampValue()
		if strings.TrimSpace(v) == "" {
			return
		}
		customSet = true
		customBuild, customErr = DecodeCustomStamp(v)
	})
	if BuildDistro() != "" {
		return CustomBuild{}, false, nil
	}
	return customBuild, customSet, customErr
}

// resetCustomForTest clears the memoized stamp (tests only).
func resetCustomForTest(v string) {
	customOnce = sync.Once{}
	customBuild, customErr, customSet = CustomBuild{}, nil, false
	customStampValue = func() string { return v }
}

// customOffered is the offered set of a custom build, nil when the binary
// is not one.
func customOffered() map[string]bool {
	cb, stamped, err := BuildCustom()
	switch {
	case !stamped:
		return nil
	case err != nil:
		return toSet(mandatoryNodeComponents)
	}
	return toSet(cb.Compiled)
}

// CustomView is the custom-build block of the components report.
type CustomView struct {
	// Distro is the custom distro id (custom-<manifest digest>).
	Distro         string   `json:"distro,omitempty"`
	ManifestDigest string   `json:"manifest_digest,omitempty"`
	Name           string   `json:"name,omitempty"`
	Base           Distro   `json:"base,omitempty"`
	Add            []string `json:"add,omitempty"`
	Omit           []string `json:"omit,omitempty"`
	// Error is set when the binary's stamp does not resolve; the binary
	// then offers only the mandatory components.
	Error string `json:"error,omitempty"`
}

// customView renders BuildCustom for the report, nil for a non-custom build.
func customView() *CustomView {
	cb, stamped, err := BuildCustom()
	if !stamped {
		return nil
	}
	if err != nil {
		return &CustomView{Error: err.Error()}
	}
	return &CustomView{
		Distro: cb.Distro, ManifestDigest: cb.ManifestDigest, Name: cb.Manifest.Name,
		Base: cb.Manifest.Base, Add: cb.Manifest.Add, Omit: cb.Manifest.Omit,
	}
}
