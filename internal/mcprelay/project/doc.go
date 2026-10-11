// Package project projects the relay's desired state into every VERIFIED
// AI client's MCP config (doc3 §11.7 W4c, §12.1/§12.2): every existing
// stdio `{command,args}` entry is replaced by the relay's TRUE stdio wrapper
// (`observer mcp-relay wrap --client <tool> --server <key>`) with the
// original launch journaled first, and the org-approved remote entries are
// projected through the relay's loopback listener. It is the ONE owner of
// the launch-journal SEQUENCE (parking decision B5, Sol P3+P4 findings 2 +
// 7): stage -> journal rows carrying the original launch and the OWNED
// digest of each relay-owned entry -> atomic rewrite (CAS on the staged
// bytes, compared in process), idempotent re-apply, and the restore: the
// format-aware reversal of only the relay-owned entries.
//
// Client MCP configs can hold credentials (other servers' env blocks and
// headers), so the D-268 / D-270 credential-file rules apply (T-397): the
// writer edits only the relay-owned entries in place, no copy of a config
// is kept, and the journal never holds a whole-file digest or any other
// derivative of the file beyond the owned entries' own digests
// (OwnedStdioDigest / OwnedRemoteDigest). The per-format writers are
// internal/mcp's Registrar (pure write primitives reached through the
// Writer seam) and the durable journal is Lane N-M's mcp_relay_launch_spec
// store (reached through the Journal seam).
//
// Pure package (CLAUDE.md §1, pinned by imports_test.go): no database/sql,
// net/http, fsnotify, os, store, orgclient, config or mcp import. Every
// decision that is a rule (which client is projected, which entry is
// refused, what makes a re-apply a no-op, which restore mode a group
// takes) is a table walked top-down.
package project
