// Package grantverify is the ONE verifier of a node's STORED enrolment grant
// (Track C item 1, GRANT-INTEG-2): the daemon's identity refresh
// (cmd/observer/nodegov_wire.go) and `observer doctor` (internal/diag) both
// call Verify, so the two can never disagree about whether a grant is intact
// or what its signature covers (Sol A3-03 class: doctor used to keep a
// second, stricter verifier and reported every managed token-rail grant as
// altered).
//
// Pure: no database/sql, net/http or fsnotify (pinned by imports_test.go).
// The callers read the grant row, the enrolment tenancy and the org key
// material, and hand this package plain values.
package grantverify
