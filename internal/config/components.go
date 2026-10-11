package config

// ComponentsConfig is the [components] block: the node operator's component
// selection (modular packaging plan section 6.1, phase P1, lane MP-GATE). It
// says which catalogued components (internal/components) are active on this
// node. A component that is not active is L2 inert: Observer starts none of
// its goroutines or listeners, makes none of its outbound calls and writes
// none of its files or other tools' config, whatever its own config switch
// says.
//
// LOCAL-ONLY and never distributed to an org. The block is written only
// through the components service (internal/componentsvc: `observer
// components select|add|remove`, `observer init --components`, and the
// dashboard's Settings > Components page), which runs the teardown of a
// removed component and audits the change. Hand edits are honoured at the
// next start; nothing is torn down for them.
//
// THE ZERO VALUE IS TODAY'S BEHAVIOUR. An install with no [components] block
// (every install before this phase) keeps every component this binary was
// built with active, byte-identical to the product before the block existed;
// each feature's own config switch still decides whether it runs.
//
// The selection is stored as a DELTA from a base (plan 5.8) so a later
// release that adds a component does not silently change what runs:
//
//   - Distro names the base. "" (default) means every component this build
//     contains is active, as today. A prebuilt distro name ("minimal",
//     "developer", "managed", "full") starts from that distro's
//     default-active set instead (docs/components.md lists the cells).
//   - Add lists component ids to activate on top of the base.
//   - Remove lists component ids to make inactive. Remove wins over Add.
//
// An unknown id or an unknown distro name is reported (`observer components
// status`) and otherwise ignored: an unknown distro falls
// back to the empty base, so a typo never switches capture off.
type ComponentsConfig struct {
	// Distro is the base selection. "" (default) = everything this build
	// contains; or one of minimal, developer, managed, full.
	Distro string `toml:"distro,omitempty"`
	// Add lists component ids activated on top of the base.
	Add []string `toml:"add,omitempty"`
	// Remove lists component ids made inactive. Remove wins over Add.
	Remove []string `toml:"remove,omitempty"`
}

// Configured reports whether the block says anything at all. An
// unconfigured block is the legacy, everything-active posture.
func (c ComponentsConfig) Configured() bool {
	return c.Distro != "" || len(c.Add) > 0 || len(c.Remove) > 0
}
