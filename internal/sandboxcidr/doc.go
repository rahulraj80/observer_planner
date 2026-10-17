// Package sandboxcidr owns what a sandbox egress allow-list entry may be: the
// four private ranges the egress gateway refuses by default and
// ParseAllowCIDRs, which accepts only canonical CIDRs inside them.
//
// It is the neutral half of internal/sandboxnet, split out in modular
// packaging phase PB (rev F1) so the core config loader can validate
// [terminal.sandbox].egress_allow_cidrs without importing the
// terminals.sandbox feature package. sandboxnet re-exports the rule names and
// ParseAllowCIDRs, so its callers are unchanged; error text still names
// sandboxnet.ParseAllowCIDRs.
//
// Pure: standard library only, no I/O.
package sandboxcidr
