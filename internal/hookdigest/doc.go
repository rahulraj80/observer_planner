// Package hookdigest computes the hooks-subtree digest Observer records in
// hook_checksums.json for a credential-bearing vendor file (the D-268
// exception, internal/integration HookSpec.HooksShareCredentialFile).
//
// A whole-file hash of such a file is a derivative of the credentials it
// holds, so it is never written outside the process. The digest here covers
// only the file's top-level "hooks" value, canonicalized (decoded and
// re-encoded as JSON with sorted keys), so the hook writer that records it
// (internal/hook) and the doctors that verify it (internal/diag) agree on
// one definition. Pure: no I/O; callers pass the bytes they already read.
package hookdigest
