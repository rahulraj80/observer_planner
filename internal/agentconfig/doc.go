// Package agentconfig is the adapter settings catalogue: every config and
// instruction file of every AI tool Observer tracks (the Files table) and
// every setting those files can hold (the settings catalogue), as data
// (docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// section 2, phase P1; operator ruling D-105: "a complete map of all
// settings by adapter ... tracked almost like a separate table").
//
// # Tables
//
//   - Files(): FileRow per physical file location (tool, kind, scope,
//     per-OS path, format, the tool's own precedence, write privilege).
//     Instruction / rule / skill / agent / command rows are DERIVED from
//     internal/guidance.Rules (the one owner of instruction globs), never
//     re-declared here; settings rows are declared in files.go.
//   - Catalog(): SettingRow per setting. Three population tiers (plan
//     section 2.2): schema-generated rows (catalog_<tool>.gen.go, written
//     by ./catgen from the pinned vendor schemas under
//     testdata/agentconfig/schemas, byte-checked by `make
//     verify-agentconfig-catalog`), the hand-written overlay tables
//     (overlay_<tool>.go: owner, danger, secret, scope narrowing and the
//     env-backed keys a schema omits, each with its Source), and the
//     doc-authored tools of phase P2 (pending, reported honestly by
//     Coverage()).
//
// # Common vocabulary, owned spans, precedence (phase P3)
//
//   - CommonSpecs(): the closed 21-key common vocabulary (section 2.4 /
//     4), each key with a canonical type and values. MapRows() maps a key
//     onto each tool's catalogue rows through one closed transform table
//     (transform.go: identity, enum_map with fan-out, bool_invert, scale,
//     effort_to_budget, list_join, rule_translate, model_alias,
//     mcp_shape). A lossy mapping says so (Lossy); an inexpressible value
//     is ErrRefused, never approximated.
//   - OwnedSpans(): the parts of vendor files Observer's own writers own
//     (hook registration, MCP, proxy route, statusline, routing apply,
//     relay), COMPUTED from the internal/integration registry plus the
//     per-writer tables in spans.go; whole-key spans stamp the covered
//     rows observer:<writer> (read-only), and SpansFor() names the spans
//     inside a user-owned value.
//   - Resolve(): the section 3.2 precedence table (vendor default, the
//     developer's L1-L3, the org's O1-O3 default and enforced, the vendor
//     managed layer) as DATA, with operator rulings D-122 (an org default
//     ranks above L1-L3 unless the developer holds an edit grant) and
//     D-123 (differing team values need a team priority; ordered-danger
//     keys resolve strictest-wins, deny lists union, allow lists
//     intersect). LintTeamConflicts is the publish-time half of D-123;
//     ResolveFiles applies the tool's own file precedence.
//
// Rows address settings by catalogue id ("codex/model") and a
// format-agnostic KeyPath; the comment-preserving engines that read and
// write the files live in internal/cfgfmt.
//
// # Secrets
//
// The catalogue never carries a secret value: a row marked Secret (an API
// key, a bearer token) has no Default, and the pinned schemas and fixtures
// are scanned by the tests. Kimi Code's ~/.kimi-code/config.toml (it holds
// a plaintext key) is never read by anything in this package or its tests.
//
// # Read view and save pipeline (phases P4 and P5)
//
//   - InspectTool (view.go): one tool's files by scope (ResolvePath,
//     paths.go), each catalogued setting's effective value and the layer
//     and file that decide it, Observer's live spans (spanlive.go) and the
//     secrets a file holds, by path only (secrets.go masks values and
//     restores a placeholder sent back unchanged).
//   - PlanSave (hygiene.go): the section 5.2 sanity check every save runs,
//     dashboard and CLI alike: parse, vendor schema (vendorschema, offline
//     and hash-pinned), the catalogue's type / enum / bounds, the org lock,
//     the danger classes (confirm, refuse while integrated), Observer-owned
//     spans, the secret posture, shadowing by a higher-precedence file, and
//     a masked unified diff (internal/textdiff). A problem already in the
//     file is a warning; only what the save introduces can block it.
//   - PlanFanOut (fanout.go): the L1 "all AI tools" write, one native edit
//     per installed tool that exposes the key, refused rows named.
//
// The I/O half (reading the files, the lock, backup, atomic write and
// read-back, the node-local layer store) is internal/agentconfigsvc.
//
// # Purity
//
// No os, database/sql, net/http or fsnotify import (pinned by
// imports_test.go). The package never names a tool in control flow: tools
// are data in the tables (CLAUDE.md rules #3 and #5).
package agentconfig
