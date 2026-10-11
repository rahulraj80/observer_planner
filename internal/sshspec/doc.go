// Package sshspec is the neutral model behind SSH remote-system profiles: the
// Profile and Options types, their closed-vocabulary validation and the port
// and timeout defaults.
//
// It exists so the core config loader (internal/config) can validate
// [[terminal.ssh.profiles]] at load time without importing the terminals.ssh
// feature package internal/sshprofile, which composes the OpenSSH argv
// (modular packaging plan phase PB, rev F1: a build tag can only remove a
// package nothing outside its component imports). internal/sshprofile
// re-exports these types as aliases, so its callers are unchanged.
//
// Validation error text still names "sshprofile", the package operators see in
// messages today; moving the code is not a behaviour change.
//
// Module shape (CLAUDE.md "Module Boundaries" #1): pure. No database/sql, no
// net/http, no os/exec, no fsnotify and no observer-internal imports, pinned by
// imports_test.go. The one filesystem touch is os.Stat behind ValidateWithFS;
// key material is never opened or read.
package sshspec
