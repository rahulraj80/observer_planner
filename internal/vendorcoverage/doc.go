// Package vendorcoverage is the pure composition of the integration registry
// and the vendor surface catalog (enterprise vendor API plan section 4.2,
// lane F2, task T-603). It is the one place that imports both
// internal/integration (the only author of the tool to surface relation) and
// internal/vendorsurface/catalog, and derives from them:
//   - the per-tool surface set with retag inheritance resolved in one place
//     (kiro-crew follows kiro-cli, grokbot follows cursor);
//   - the reverse index surface to tools;
//   - the coverage targets that are not registry rows (vendor products,
//     gateway routes, conditional providers; plan 4.5 Targets and routes);
//   - the consistency checks the catalog tests pin.
//
// Pure: no database/sql, net/http, os/exec or fsnotify (imports_test.go).
package vendorcoverage
