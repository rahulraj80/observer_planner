// Package textdiff renders the line diff a settings save previews (the
// adapter-config hygiene pipeline step 7 and the Observer config.toml
// dry-run, docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// sections 5.2 and 5.5).
//
// It is pure: no file, database, network or watcher access. Callers mask
// secret values BEFORE diffing, so a diff can never echo a credential.
package textdiff
