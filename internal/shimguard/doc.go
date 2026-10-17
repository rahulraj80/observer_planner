// Package shimguard is the core half of command wrapping: the shim marker,
// the default shim directory and a PATH lookup that skips shims. It is what
// keeps a launcher or a "is claude installed" probe from resolving a vendor
// command to an observer shim (which would recurse), so it must stay in
// every build, including one without the integration.shell-wrap component
// (a previous install's shims can outlive it). internal/shellwrap and
// internal/shellwrapsvc, which own writing shims, re-export these names
// (lane MP-L1E, T-715).
package shimguard
