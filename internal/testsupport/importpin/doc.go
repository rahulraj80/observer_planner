// Package importpin is the one shared implementation of the per-package
// import-boundary pins (CLAUDE.md "Module Boundaries" rule 1: a pure
// package imports no database/sql, net/http, fsnotify or sibling
// subsystem). Each pure package keeps its own tiny imports_test.go that
// calls Check with that package's Spec, so the pin still lives in the
// package's own test binary, runs whenever that package is tested, and
// fails loudly naming the file and the offending import.
//
// Before this package existed the same go/parser loop was copied into
// about 215 imports_test.go files. The copies drifted: some trimmed the
// import literal of double quotes (a raw-string import literal then
// bypassed the check), some never noticed when they scanned zero files.
// Check decodes every literal with strconv.Unquote and treats an empty
// scan as a failure, so both defects are fixed once for every package.
//
// TEST SUPPORT ONLY: no production command or package may import it
// (tests/invariant/vendormock_boundary_test.go).
package importpin
