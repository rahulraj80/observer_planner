// Package navtabsrc reads the node dashboard's page tab tables
// (web/src/lib/pageTabRoutes.ts and web/src/lib/nodePageTabs.ts) and renders
// them as the Go source of internal/policyfam/nodegov's tab vocabulary
// (navtabs_gen.go).
//
// The SPA's tab tables are the ONE source of the tab vocabulary (backlog item
// 4, per-tab visibility): nodegov's table is generated from them by
// `go generate ./internal/policyfam/nodegov`, and nodegov's
// navtabs_source_test.go fails when the committed file no longer matches what
// the generator would write, so a renamed or added tab can never drift into a
// publish lint that disagrees with the page.
//
// Pure: it parses bytes and returns bytes. No file system access outside the
// generator's main package.
package navtabsrc
