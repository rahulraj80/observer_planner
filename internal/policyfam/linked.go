package policyfam

import (
	"errors"
	"fmt"
	"sync"
)

// linked.go is the seam for a policy family whose compiler belongs to an
// optional component (modular packaging, lane MP-L1G). Such a family keeps
// its name in SupportedFamilies (the closed enum and its stable order never
// depend on the build), but its compiler is linked by a registration pair
// file that the component's omit tag leaves out. When the pair is absent,
// CompileFamilyBody answers ErrFamilyNotLinked for that family, so a publish
// is refused and an accepting node refuses the body with a named reason,
// never a silent no-op. Today one family uses it: tools.mcp_access
// (family_mcpaccess.go, omitted by sbo_omit_org_agentaccess). A node lane can
// add its own component's tag to the same pair file's build line.

// ErrFamilyNotLinked is a supported family whose compiler is not in this
// build. Callers can match it with errors.Is.
var ErrFamilyNotLinked = errors.New("policyfam: family compiler is not in this build")

// LinkedFamily is the compiler of a family registered through
// RegisterLinkedFamily.
type LinkedFamily struct {
	// Compile is the family's CompileFamilyBody leg: the compiled spec and
	// the canonical body bytes.
	Compile func(raw []byte, maxBytes int64) (spec any, canonicalBody []byte, err error)
	// Enforces is the family's SpecRequestsEnforceMode leg, called with a
	// spec Compile returned.
	Enforces func(spec any) bool
}

var (
	linkedMu       sync.RWMutex
	linkedFamilies = map[string]LinkedFamily{}
)

// RegisterLinkedFamily links family's compiler into this build. It is called
// from a registration pair file's init. A second registration of the same
// family replaces the first.
func RegisterLinkedFamily(family string, f LinkedFamily) {
	linkedMu.Lock()
	defer linkedMu.Unlock()
	linkedFamilies[family] = f
}

// linkedOnlyFamilies are the supported families compiled only through the
// seam; every other supported family is compiled by the switch in
// CompileFamilyBody and is always linked.
var linkedOnlyFamilies = map[string]bool{FamilyMCPAccess: true}

// IsFamilyLinked reports whether family is supported and its compiler is in
// this build.
func IsFamilyLinked(family string) bool {
	if !IsSupportedFamily(family) {
		return false
	}
	if !linkedOnlyFamilies[family] {
		return true
	}
	_, ok := lookupLinkedFamily(family)
	return ok
}

func lookupLinkedFamily(family string) (LinkedFamily, bool) {
	linkedMu.RLock()
	defer linkedMu.RUnlock()
	f, ok := linkedFamilies[family]
	return f, ok && f.Compile != nil && f.Enforces != nil
}

// compileLinked compiles a linked family's body, or answers
// ErrFamilyNotLinked naming the family.
func compileLinked(family string, raw []byte, maxBytes int64) (any, []byte, error) {
	f, ok := lookupLinkedFamily(family)
	if !ok {
		return nil, nil, fmt.Errorf("policyfam.CompileFamilyBody: %q: %w", family, ErrFamilyNotLinked)
	}
	return f.Compile(raw, maxBytes)
}

// linkedEnforces is SpecRequestsEnforceMode for a linked family. spec came
// from compileLinked, so the family is registered; were it not, the answer is
// the conservative one (true: the body needs the node's enforce consent).
func linkedEnforces(family string, spec any) bool {
	f, ok := lookupLinkedFamily(family)
	if !ok {
		return true
	}
	return f.Enforces(spec)
}
