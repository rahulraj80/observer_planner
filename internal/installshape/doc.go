// Package installshape decides which command an Observer registration (a
// hook entry, an MCP server entry) should invoke, from the shape of the
// install the running binary lives in (U1, lane FIX-UPGRADE, D-590).
//
// A package manager owns the tree it installed: npm deletes
// node_modules/@superbased/observer-<platform>/bin/observer on an upgrade
// (and 1.36 hoists it where 1.34 nested it), pip replaces
// site-packages/observer/_bin/observer. A hook or MCP entry that names that
// path breaks the moment the user upgrades, until something rewrites it.
// The package manager's own launcher (the npm bin symlink, the pip console
// script) survives the upgrade, so a registration made from a binary inside
// such a tree names the launcher instead, when one exists that resolves to
// the same install. Every other install (an archive, install.sh, a build)
// keeps the real path, as before.
//
// The package is pure: Decide walks an ordered rule table over a binary
// path, the PATH candidates and an injected Resolve function. No file
// system, no exec, no environment reads (pinned by imports_test.go); the
// one I/O seam is cmd/observer's registrationCommand.
package installshape
