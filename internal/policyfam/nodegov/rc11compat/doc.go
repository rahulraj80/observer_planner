// Package rc11compat holds a VERBATIM copy of the node.governance compiler as
// released in v1.34.0-rc.11 (the rc11_*_test.go files: `git show
// v1.34.0-rc.11:internal/policyfam/nodegov/<file>`, only the package clause
// changed), and tests that run what the org server would serve such a node
// through it.
//
// Every v1.34.0 release candidate and v1.34.0 itself compile node.governance
// identically (their vocab.go, pinnable.go key sets, wire.go and policy.go
// are byte-identical), so this one fixture stands for the whole released
// v1.34 line, the build the demo estate runs. A released node compiles the
// body STRICTLY: one unknown id, key or schema fails the whole body and the
// node keeps its previous governance, which is what the serve-time projection
// (nodegov.ProjectToTarget) exists to prevent.
//
// The fixture is test-only code: it is never linked into a binary.
package rc11compat
