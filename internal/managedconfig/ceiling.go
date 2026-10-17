package managedconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// MaxBytes caps a ceiling source.
const MaxBytes = 64 << 10

// Ceiling is one explicit component allow-set. It never names a distro: a
// distro is provisioning shorthand resolved to ids at provisioning time
// (FromDistro), so a later release that grows a distro cannot widen a node.
type Ceiling struct {
	// IDs are the node component ids the machine allows, sorted. Core is
	// always active whether or not it is listed; an empty set means core
	// only.
	IDs []string `json:"ids"`
	// CatalogVersion is the catalog version the set was resolved against.
	CatalogVersion int `json:"catalog_version"`
	// SourceHash is the hex SHA-256 of the source bytes it was read from.
	SourceHash string `json:"source_hash,omitempty"`
}

// Status is the loader's verdict.
type Status string

// The closed Status vocabulary.
const (
	// StatusNone: no ceiling source exists. No ceiling applies.
	StatusNone Status = "none"
	// StatusValid: the source exists and passed every check.
	StatusValid Status = "valid"
	// StatusInvalidCoreOnly: the source exists but failed, and no valid
	// ceiling was ever read: only core runs.
	StatusInvalidCoreOnly Status = "invalid_core_only"
	// StatusInvalidLastKnown: the source exists but failed; the last-known
	// explicit set the caller stored stays in force.
	StatusInvalidLastKnown Status = "invalid_last_known"
)

// Probe is what a per-OS reader found. Exactly one of Data (a toml or json
// document) and Registry is set when Found and FailedCheck is empty.
type Probe struct {
	// Found reports that a ceiling source exists at the fixed location.
	Found bool
	// Source names it (a path, or the registry key).
	Source string
	// Format is "toml", "json" (a converted plist) or "registry".
	Format string
	// Data is the document bytes for toml and json.
	Data []byte
	// Registry holds the values read from the Windows policy key.
	Registry *RegistryValues
	// FailedCheck names the ownership, symlink, size or read check that
	// failed; empty when every check passed.
	FailedCheck string
}

// RegistryValues are the values of the Windows policy key.
type RegistryValues struct {
	HaveComponents bool
	Components     []string
	HaveVersion    bool
	CatalogVersion uint64
	// Unknown lists value names the key carries that this loader does not
	// know; any is a malformed source.
	Unknown []string
}

// Result is the loader's answer. Every caller (daemon, hook, launchers,
// doctor, updater) reads the same fields.
type Result struct {
	Status Status
	// Ceiling is the set in force; nil only for StatusNone.
	Ceiling *Ceiling
	Source  string
	// FailedCheck names the failed check, or "parse" with Err set.
	FailedCheck string
	Err         string
	// Notes are non-fatal observations (unknown ids ignored, a newer
	// catalog version).
	Notes []string
	// UpdatesAllowed is false while the source is invalid: every update
	// and distro transition is refused.
	UpdatesAllowed bool
	// Persist asks the caller to store Ceiling as its new last-known set
	// (with its catalog version and source hash; never re-derived from a
	// distro name).
	Persist bool
}

// Pinned returns the nodecomponents.Inputs / components.Governance
// ceiling pair.
func (r Result) Pinned() (bool, []string) {
	if r.Ceiling == nil {
		return false, nil
	}
	return true, append([]string{}, r.Ceiling.IDs...)
}

// outcome is the probe classified for the decision table.
type outcome int

const (
	outcomeAbsent outcome = iota
	outcomeValid
	outcomeInvalid
)

// decideRule is one row of the decision table.
type decideRule struct {
	name    string
	outcome outcome
	haveLKG bool
	anyLKG  bool
	status  Status
}

// decideRules is the fail-closed table (fourth-pass M1), one test row each.
var decideRules = []decideRule{
	{name: "no source", outcome: outcomeAbsent, anyLKG: true, status: StatusNone},
	{name: "valid source", outcome: outcomeValid, anyLKG: true, status: StatusValid},
	{name: "invalid source, a valid ceiling was read before", outcome: outcomeInvalid, haveLKG: true, status: StatusInvalidLastKnown},
	{name: "invalid source, no valid ceiling ever read", outcome: outcomeInvalid, haveLKG: false, status: StatusInvalidCoreOnly},
}

// Decide turns a probe and the caller's last-known ceiling into a Result.
func Decide(p Probe, lastKnown *Ceiling) Result {
	res := Result{Source: p.Source}
	oc := outcomeAbsent
	var parsed Ceiling
	if p.Found {
		oc = outcomeValid
		if p.FailedCheck != "" {
			oc = outcomeInvalid
			res.FailedCheck = p.FailedCheck
		} else {
			c, notes, err := parseProbe(p)
			if err != nil {
				oc = outcomeInvalid
				res.FailedCheck, res.Err = "parse", err.Error()
			} else {
				parsed, res.Notes = c, notes
			}
		}
	}
	for _, r := range decideRules {
		if r.outcome != oc || (!r.anyLKG && r.haveLKG != (lastKnown != nil)) {
			continue
		}
		res.Status = r.status
		break
	}
	switch res.Status {
	case StatusNone:
		res.UpdatesAllowed = true
	case StatusValid:
		res.UpdatesAllowed = true
		c := parsed
		res.Ceiling = &c
		res.Persist = lastKnown == nil || !sameCeiling(*lastKnown, c)
	case StatusInvalidLastKnown:
		c := *lastKnown
		c.IDs = append([]string{}, lastKnown.IDs...)
		res.Ceiling = &c
	case StatusInvalidCoreOnly:
		res.Ceiling = &Ceiling{IDs: []string{}, CatalogVersion: components.CatalogVersion}
	}
	return res
}

func sameCeiling(a, b Ceiling) bool {
	return a.CatalogVersion == b.CatalogVersion && a.SourceHash == b.SourceHash &&
		strings.Join(a.IDs, "\n") == strings.Join(b.IDs, "\n")
}

// tomlFile is the managed.toml schema:
//
//	[ceiling]
//	catalog_version = 1
//	components = ["adapter.claude-code", "codeintel"]
type tomlFile struct {
	Ceiling struct {
		CatalogVersion int      `toml:"catalog_version"`
		Components     []string `toml:"components"`
	} `toml:"ceiling"`
}

// plistJSON is the managed-preferences plist after plutil -convert json.
type plistJSON struct {
	Components     *[]string `json:"Components"`
	CatalogVersion *int      `json:"CatalogVersion"`
}

func parseProbe(p Probe) (Ceiling, []string, error) {
	switch p.Format {
	case "toml":
		return ParseTOML(p.Data)
	case "json":
		return parsePlistJSON(p.Data)
	case "registry":
		return parseRegistry(p.Registry)
	}
	return Ceiling{}, nil, fmt.Errorf("unknown source format %q", p.Format)
}

// ParseTOML parses and validates a managed.toml document strictly: unknown
// keys, a missing components list or a missing catalog version are errors.
func ParseTOML(data []byte) (Ceiling, []string, error) {
	if len(data) > MaxBytes {
		return Ceiling{}, nil, fmt.Errorf("managedconfig: %d bytes exceeds the %d-byte cap", len(data), MaxBytes)
	}
	var f tomlFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Ceiling{}, nil, fmt.Errorf("managedconfig: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return Ceiling{}, nil, fmt.Errorf("managedconfig: unknown keys %s", strings.Join(keys, ", "))
	}
	if !md.IsDefined("ceiling", "components") {
		return Ceiling{}, nil, errors.New("managedconfig: [ceiling] components is required (an empty list means core only)")
	}
	if !md.IsDefined("ceiling", "catalog_version") {
		return Ceiling{}, nil, errors.New("managedconfig: [ceiling] catalog_version is required")
	}
	return validate(f.Ceiling.Components, f.Ceiling.CatalogVersion, data)
}

func parsePlistJSON(data []byte) (Ceiling, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p plistJSON
	if err := dec.Decode(&p); err != nil {
		return Ceiling{}, nil, fmt.Errorf("managedconfig: managed preferences: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Ceiling{}, nil, errors.New("managedconfig: managed preferences: trailing data")
	}
	if p.Components == nil {
		return Ceiling{}, nil, errors.New("managedconfig: managed preferences: Components is required (an empty array means core only)")
	}
	if p.CatalogVersion == nil {
		return Ceiling{}, nil, errors.New("managedconfig: managed preferences: CatalogVersion is required")
	}
	return validate(*p.Components, *p.CatalogVersion, data)
}

func parseRegistry(v *RegistryValues) (Ceiling, []string, error) {
	if v == nil {
		return Ceiling{}, nil, errors.New("managedconfig: no registry values")
	}
	if len(v.Unknown) > 0 {
		u := append([]string{}, v.Unknown...)
		sort.Strings(u)
		return Ceiling{}, nil, fmt.Errorf("managedconfig: unknown registry values %s", strings.Join(u, ", "))
	}
	if !v.HaveComponents {
		return Ceiling{}, nil, errors.New("managedconfig: registry value Components (REG_MULTI_SZ) is required")
	}
	if !v.HaveVersion || v.CatalogVersion > 1<<31 {
		return Ceiling{}, nil, errors.New("managedconfig: registry value CatalogVersion (REG_DWORD) is required")
	}
	// The hash covers a canonical rendering of the values, so the same
	// policy hashes the same whatever order the registry returns them in.
	canon := fmt.Sprintf("Components=%s\nCatalogVersion=%d\n", strings.Join(v.Components, "\x00"), v.CatalogVersion)
	return validate(v.Components, int(v.CatalogVersion), []byte(canon))
}

// validate turns a raw id list into a Ceiling: ids must use the catalog id
// alphabet; ids this catalog does not know as node components are ignored
// with a note (a ceiling written for a newer release names components this
// binary does not have, which cannot widen anything).
func validate(ids []string, catalogVersion int, source []byte) (Ceiling, []string, error) {
	if catalogVersion < 1 {
		return Ceiling{}, nil, fmt.Errorf("managedconfig: catalog_version must be a positive integer, got %d", catalogVersion)
	}
	var notes []string
	if catalogVersion > components.CatalogVersion {
		notes = append(notes, fmt.Sprintf("ceiling was resolved against catalog version %d, newer than this build's %d", catalogVersion, components.CatalogVersion))
	}
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || id != raw || strings.ContainsAny(id, " *:") {
			return Ceiling{}, nil, fmt.Errorf("managedconfig: %q is not a component id (a ceiling lists explicit ids, never selectors or distros)", raw)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		c, ok := components.Get(id)
		switch {
		case !ok:
			notes = append(notes, fmt.Sprintf("%s: not a component of this build's catalog; ignored", id))
			continue
		case c.Binary != components.BinaryNode:
			notes = append(notes, fmt.Sprintf("%s: an org server component; ignored", id))
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	sum := sha256.Sum256(source)
	return Ceiling{IDs: out, CatalogVersion: catalogVersion, SourceHash: hex.EncodeToString(sum[:])}, notes, nil
}

// FromDistro resolves a prebuilt distro to the explicit set a ceiling file
// carries (provisioning shorthand, plan 6.2.2: `observer components
// ceiling-file --distro managed` and the web2 managed-settings export).
func FromDistro(d components.Distro) (Ceiling, error) {
	if !d.Valid() {
		return Ceiling{}, fmt.Errorf("managedconfig.FromDistro: %q is not a prebuilt distro", d)
	}
	ids := components.InDistro(d)
	sort.Strings(ids)
	return Ceiling{IDs: ids, CatalogVersion: components.CatalogVersion}, nil
}

// RenderTOML renders a ceiling as the managed.toml document ParseTOML reads
// back to the same ids and catalog version.
func RenderTOML(c Ceiling) []byte {
	var b strings.Builder
	b.WriteString("# SuperBased Observer local component ceiling (managed by your organization's MDM).\n")
	b.WriteString("# An explicit allow-set: a later release cannot widen this machine until this file changes.\n")
	b.WriteString("[ceiling]\n")
	fmt.Fprintf(&b, "catalog_version = %d\n", c.CatalogVersion)
	b.WriteString("components = [\n")
	ids := append([]string{}, c.IDs...)
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "  %q,\n", id)
	}
	b.WriteString("]\n")
	return []byte(b.String())
}

// Exceeds lists the non-core ids of compiled that the ceiling does not
// allow: what a distro transition or a policy would have to widen. The
// updater refuses a transition whose compiled set exceeds the ceiling.
func (c Ceiling) Exceeds(compiled []string) []string {
	allowed := map[string]bool{}
	for _, id := range c.IDs {
		allowed[id] = true
	}
	var out []string
	for _, id := range compiled {
		comp, ok := components.Get(id)
		if ok && comp.Class == components.ClassCore {
			continue
		}
		if !allowed[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
