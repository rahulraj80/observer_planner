package sandhold

import (
	"sort"
	"strings"

	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// vocab.go maps SandHold's words (note 41 D4, owned by SandHold) onto
// Observer's sandbox vocabulary, one to one, and never claims more than the
// node says.

// The node's isolation tiers, weakest first.
const (
	TierShared   = "shared"
	TierShielded = "shielded"
	TierSealed   = "sealed"
	TierVault    = "vault"
)

// The node's isolation levels (a target's capabilities.isolation).
const (
	LevelProcess    = "process"
	LevelContainer  = "container"
	LevelUserKernel = "user-kernel"
	LevelMicroVM    = "microvm"
)

// The node's network words.
const (
	NetworkNone    = "none"
	NetworkLimited = "limited"
	NetworkOpen    = "open"
)

// levelClass maps a level to Observer's class: exact, one to one.
var levelClass = map[string]vocab.IsoClass{
	LevelProcess:    vocab.ClassProcess,
	LevelContainer:  vocab.ClassContainer,
	LevelUserKernel: vocab.ClassUserspaceKernel,
	LevelMicroVM:    vocab.ClassHWVM,
}

// tierClass maps a tier to Observer's class when a target names no level:
// D4's one-to-one mapping (Shared a jailed process, Shielded a hardened
// container, Sealed its own user-space kernel, Vault its own virtual
// machine).
var tierClass = map[string]vocab.IsoClass{
	TierShared:   vocab.ClassProcess,
	TierShielded: vocab.ClassContainer,
	TierSealed:   vocab.ClassUserspaceKernel,
	TierVault:    vocab.ClassHWVM,
}

// networkTier maps the node's network words to Observer's tiers: none to
// none, limited (an allow list) to proxy_only, open to internet. host never
// maps: a hosted box never shares this machine's network.
var networkTier = map[string]vocab.Tier{
	NetworkNone:    vocab.TierNone,
	NetworkLimited: vocab.TierProxyOnly,
	NetworkOpen:    vocab.TierInternet,
}

// tierTitle is the tier's display name.
var tierTitle = map[string]string{TierShared: "Shared", TierShielded: "Shielded", TierSealed: "Sealed", TierVault: "Vault"}

// ClassForLevel is the class of a node isolation level.
func ClassForLevel(level string) (vocab.IsoClass, bool) {
	c, ok := levelClass[strings.ToLower(strings.TrimSpace(level))]
	return c, ok
}

// ClassForTier is the class of a node isolation tier.
func ClassForTier(tier string) (vocab.IsoClass, bool) {
	c, ok := tierClass[strings.ToLower(strings.TrimSpace(tier))]
	return c, ok
}

// TierForNetwork is Observer's tier for a node network word.
func TierForNetwork(network string) (vocab.Tier, bool) {
	t, ok := networkTier[strings.ToLower(strings.TrimSpace(network))]
	return t, ok
}

// The sources of a target's class.
const (
	ClassFromLevel = "level"
	ClassFromTier  = "tier"
)

// TargetClass is a target's Observer class: from its level when it names
// one (more exact), else from its tier; ok is false when neither is known
// (the target is then described in the node's words only, never given a
// class).
func TargetClass(t Target) (class vocab.IsoClass, from string, ok bool) {
	if c, ok := ClassForLevel(t.Capabilities.Isolation); ok {
		return c, ClassFromLevel, true
	}
	if c, ok := ClassForTier(t.Tier); ok {
		return c, ClassFromTier, true
	}
	return "", "", false
}

// TargetNetworks are the node network words a target can give a sandbox:
// none, limited and open where it enforces an allow list, else open only
// (a create asking for an allow list there is refused by the node).
func TargetNetworks(t Target) []string {
	if t.Capabilities.EgressAllowlist {
		return []string{NetworkNone, NetworkLimited, NetworkOpen}
	}
	return []string{NetworkOpen}
}

// TargetTiers are TargetNetworks in Observer's tiers.
func TargetTiers(t Target) []vocab.Tier {
	var out []vocab.Tier
	for _, n := range TargetNetworks(t) {
		if tr, ok := TierForNetwork(n); ok {
			out = append(out, tr)
		}
	}
	return out
}

// TierTitle is a tier's display name ("Vault"), or the node's word as sent.
func TierTitle(tier string) string {
	if s, ok := tierTitle[strings.ToLower(tier)]; ok {
		return s
	}
	return tier
}

// Offer is one isolation the node offers, in its words and Observer's.
type Offer struct {
	// Tier and Means are the node's words ("vault", "its own kernel in a
	// virtual machine").
	Tier  string `json:"tier"`
	Means string `json:"means,omitempty"`
	// Class is Observer's class ("" when the node's words do not map).
	Class vocab.IsoClass `json:"class,omitempty"`
	// ClassFrom is ClassFromLevel or ClassFromTier.
	ClassFrom string `json:"class_from,omitempty"`
	// Networks are the node's network words this isolation can give.
	Networks []string `json:"networks"`
	// Targets are the ids that offer it; Default when one is the org's
	// default.
	Targets []string `json:"targets"`
	Default bool     `json:"default,omitempty"`
}

// Offers folds the targets the key may use (state active, or no state
// sent) into one entry per isolation, weakest first.
func Offers(ts []Target) []Offer {
	byTier := map[string]*Offer{}
	var order []string
	rank := map[string]int{}
	for _, t := range ts {
		if t.State != "" && t.State != "active" {
			continue
		}
		key := strings.ToLower(t.Tier)
		if key == "" {
			key = "(" + t.ID + ")"
		}
		o, seen := byTier[key]
		if !seen {
			o = &Offer{Tier: t.Tier, Means: t.TierMeans}
			if o.Tier == "" {
				o.Tier = t.ID
			}
			if c, from, ok := TargetClass(t); ok {
				o.Class, o.ClassFrom = c, from
			}
			byTier[key] = o
			order = append(order, key)
			rank[key] = t.TierRank
		} else if c, from, ok := TargetClass(t); ok && (o.Class == "" || c.Rank() < o.Class.Rank()) {
			// Two targets of one tier with different levels: the weaker is
			// what the tier can be counted on for.
			o.Class, o.ClassFrom = c, from
		}
		o.Targets = append(o.Targets, t.ID)
		o.Default = o.Default || t.Default
		o.Networks = unionNetworks(o.Networks, TargetNetworks(t))
	}
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := rank[order[i]], rank[order[j]]
		if ri == 0 {
			ri = 99
		}
		if rj == 0 {
			rj = 99
		}
		return ri < rj
	})
	out := make([]Offer, 0, len(order))
	for _, k := range order {
		out = append(out, *byTier[k])
	}
	return out
}

func unionNetworks(a, b []string) []string {
	all := []string{NetworkNone, NetworkLimited, NetworkOpen}
	have := map[string]bool{}
	for _, n := range a {
		have[n] = true
	}
	for _, n := range b {
		have[n] = true
	}
	var out []string
	for _, n := range all {
		if have[n] {
			out = append(out, n)
		}
	}
	return out
}

// Line is the offer in one line: the node's words first, then Observer's
// class and tiers ("Vault (its own kernel in a virtual machine): hw_vm;
// network none, limited, open; runs on kata (default)").
func (o Offer) Line() string {
	var b strings.Builder
	b.WriteString(TierTitle(o.Tier))
	if o.Means != "" {
		b.WriteString(" (" + o.Means + ")")
	}
	if o.Class != "" {
		b.WriteString(": Observer class " + string(o.Class))
	} else {
		b.WriteString(": no Observer class (the node's words do not name a known isolation)")
	}
	b.WriteString("; network " + strings.Join(o.Networks, ", "))
	if len(o.Targets) > 0 {
		b.WriteString("; runs on " + strings.Join(o.Targets, ", "))
	}
	if o.Default {
		b.WriteString(" (default)")
	}
	return PlainLine(b.String())
}
