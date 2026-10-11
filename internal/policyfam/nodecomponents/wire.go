package nodecomponents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// Family is the unified policy resource family name (plan 6.2). It is
// registered in internal/policyfam.SupportedFamilies by lane MP-GOV together
// with its delivery rail; until then nothing delivers a body of this family.
const Family = "node.components"

// Authority is the enrolment-grant authority token that lets an org govern
// a managed node's components (plan 6.2.1). Operator ruling Q9 (D-374): it
// is super-admin-to-grant, like the Agent Access admin keys. Lane MP-GOV adds
// it to internal/govern's known, managed-only and family tables.
const Authority = "components.govern"

// The closed body vocabularies.
const (
	// UnlistedInert: a component no selector names is blocked (plan Q5:
	// new components stay inert on managed nodes until the policy allows
	// them). The only v1 value; an empty field means it.
	UnlistedInert = "inert"
	// TeardownAuto runs a removed component's catalog teardown steps.
	TeardownAuto = "auto"
	// TeardownManual leaves teardown to the operator and reports it.
	TeardownManual = "manual"
	// DataKeep keeps a removed component's node-local data (plan Q7, the
	// default).
	DataKeep = "keep"
	// DataPurge schedules a purge PurgeAfterDays after removal (plan 6.3:
	// an explicit future authorisation given at publish time).
	DataPurge = "purge"
	// MaxPurgeAfterDays bounds the purge delay.
	MaxPurgeAfterDays = 3650
)

// BodyV1 is the node.components v1 wire body (plan 6.2):
//
//	{
//	  "distro": "managed",
//	  "allow":   ["adapter.claude-code", "adapter.codex", "class:analytics", "terminals"],
//	  "require": ["org.client", "org.update", "guard", "integration.hooks"],
//	  "deny":    [{"selector": "cloud.intelligence"}, {"selector": "egress.*", "grantable": true}],
//	  "unlisted": "inert",
//	  "on_remove": {"teardown": "auto", "data": "keep", "purge_after_days": 0}
//	}
//
// Every field is optional. An empty body is a valid, strict policy: nothing
// allowed, so every non-core component is inert.
type BodyV1 struct {
	// Distro names the prebuilt distro this cohort should run; the update
	// rail (lane MP-REL) reads it. It never activates anything by itself.
	Distro   string        `json:"distro,omitempty"`
	Allow    []string      `json:"allow,omitempty"`
	Require  []string      `json:"require,omitempty"`
	Deny     []DenyEntryV1 `json:"deny,omitempty"`
	Unlisted string        `json:"unlisted,omitempty"`
	OnRemove *OnRemoveV1   `json:"on_remove,omitempty"`
}

// DenyEntryV1 is one typed deny entry. Grantable lets a component access
// grant override this entry; it defaults to false and is omitted from the
// canonical encoding when false (third-pass N4). The signed body always
// carries objects; a bare string is web2 authoring shorthand only.
type DenyEntryV1 struct {
	Selector  string `json:"selector"`
	Grantable bool   `json:"grantable,omitempty"`
}

// OnRemoveV1 is what a node does when a component this policy used to allow
// becomes blocked (plan 6.3).
type OnRemoveV1 struct {
	Teardown string `json:"teardown"`
	Data     string `json:"data"`
	// PurgeAfterDays: 0 means never (data is kept). A non-zero value
	// requires Data == "purge".
	PurgeAfterDays int `json:"purge_after_days"`
}

// DecodeBody strictly decodes raw org-wire JSON into a BodyV1: unknown
// fields are rejected, the document must not exceed maxBytes, and nothing
// may follow it. A bare string where a deny object belongs is rejected here.
func DecodeBody(raw []byte, maxBytes int64) (BodyV1, error) {
	if maxBytes <= 0 {
		return BodyV1{}, fmt.Errorf("policyfam/nodecomponents.DecodeBody: cap must be positive, got %d", maxBytes)
	}
	if int64(len(raw)) > maxBytes {
		return BodyV1{}, fmt.Errorf("policyfam/nodecomponents.DecodeBody: body is %d bytes, exceeds the %d-byte cap", len(raw), maxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var body BodyV1
	if err := dec.Decode(&body); err != nil {
		return BodyV1{}, fmt.Errorf("policyfam/nodecomponents.DecodeBody: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return BodyV1{}, fmt.Errorf("policyfam/nodecomponents.DecodeBody: trailing bytes after the document")
	}
	return body, nil
}

// Canonicalize returns the body in its canonical form: allow and require
// sorted and deduplicated, deny entries sorted by selector, the unlisted
// rule spelled out, and an absent on_remove filled with the defaults (keep
// data, auto teardown). A selector repeated in deny with different grantable
// values is an error, because there is no honest single meaning for it.
func Canonicalize(b BodyV1) (BodyV1, error) {
	out := BodyV1{
		Distro:  b.Distro,
		Allow:   uniqSorted(b.Allow),
		Require: uniqSorted(b.Require),
	}
	seen := map[string]bool{}
	for _, d := range b.Deny {
		if g, ok := seen[d.Selector]; ok {
			if g != d.Grantable {
				return BodyV1{}, fmt.Errorf("policyfam/nodecomponents.Canonicalize: deny selector %q is listed both grantable and not", d.Selector)
			}
			continue
		}
		seen[d.Selector] = d.Grantable
		out.Deny = append(out.Deny, d)
	}
	sort.Slice(out.Deny, func(i, j int) bool { return out.Deny[i].Selector < out.Deny[j].Selector })
	out.Unlisted = b.Unlisted
	if out.Unlisted == "" {
		out.Unlisted = UnlistedInert
	}
	if b.OnRemove != nil {
		r := *b.OnRemove
		out.OnRemove = &r
	} else {
		out.OnRemove = &OnRemoveV1{Teardown: TeardownAuto, Data: DataKeep}
	}
	return out, nil
}

// CanonicalJSON is the canonical encoding a body is hashed and signed over:
// Canonicalize, then a deterministic marshal. Whitespace, key order, list
// order and duplicates in the submission never change it.
func CanonicalJSON(b BodyV1) ([]byte, error) {
	c, err := Canonicalize(b)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("policyfam/nodecomponents.CanonicalJSON: %w", err)
	}
	return out, nil
}

// CompileBody is the strict publish path: decode, compile against this
// build's catalog with every unknown id an error, and return the canonical
// bytes to sign.
func CompileBody(raw []byte, maxBytes int64) (PolicySpec, []byte, error) {
	return compileBody(raw, maxBytes, true)
}

// CompileBodyLenient is the node accept path: a selector naming an exact id
// this build does not know is dropped and reported in PolicySpec.IgnoredIDs
// instead of failing the body, so a newer org server naming a newer
// component cannot stop every other directive reaching an older node (the
// node.governance precedent). Syntax errors and contradictions still fail.
func CompileBodyLenient(raw []byte, maxBytes int64) (PolicySpec, []byte, error) {
	return compileBody(raw, maxBytes, false)
}

func compileBody(raw []byte, maxBytes int64, strict bool) (PolicySpec, []byte, error) {
	body, err := DecodeBody(raw, maxBytes)
	if err != nil {
		return PolicySpec{}, nil, err
	}
	spec, err := compile(body, strict)
	if err != nil {
		return PolicySpec{}, nil, err
	}
	canon, err := CanonicalJSON(body)
	if err != nil {
		return PolicySpec{}, nil, err
	}
	return spec, canon, nil
}

func uniqSorted(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// distroValid reports whether d is a prebuilt node distro name.
func distroValid(d string) bool { return components.Distro(d).Valid() }
