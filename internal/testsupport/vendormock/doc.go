// Package vendormock holds the vendor-shaped mock servers, the shared
// conformance suite and the schema-random generator of the enterprise vendor
// API plan (section 12A.2, 12A.6 and 12A.8; lane F3 second half, task T-602).
//
// It is TEST SUPPORT ONLY. No production command or package may import it:
// tests/invariant/vendormock_boundary_test.go fails when any non-test package
// imports internal/testsupport or when the dependency closure of a command
// under cmd/ contains it. Only _test.go files import it; no build tag is
// relied on.
//
// Everything here is seeded from the fixture corpus in testdata/vendorapi
// (every fixture derived from a public vendor source, never from a real
// account) and from the dereferenced schema extracts in testdata/vendorschema.
// A mock answers with the corpus bytes, so a fixture change exercises both the
// parser and the request path. Nothing in this package proves a vendor
// behaves this way live: every surface stays "synthetic, awaiting live
// validation" until a later lane records a receipt from a real connection.
//
// The pieces:
//
//   - corpus.go loads the corpus (sidecars decoded strictly through
//     internal/vendorapi/fixturecorpus) and the schema extracts.
//   - profile.go is the per-vendor mechanics table (auth header and scheme,
//     required headers, the cursor parameter name) with the public source of
//     each row, and the paging-family classifier.
//   - server.go is the httptest mock: routing by the fixture's path template,
//     auth by scheme and plane, documented paging, 429 with Retry-After in
//     seconds or HTTP-date form, page-size 400s, slow responses, schema drift
//     and the two-step report download on a second host.
//   - conformance.go is the scenario vocabulary, the applicability rules, the
//     per-surface declarations (declarations.json) and Run, which drives any
//     Driver through every declared scenario.
//   - reference.go is a reference driver used to prove the mocks and the suite
//     are not vacuous before a real driver exists (lane F8 plugs real drivers
//     into Run).
//   - schemarand.go generates random schema-valid instances (nulls, absent
//     optional fields, empty arrays, large numbers, unknown extra fields) and
//     checks that a contract type decodes them without losing a declared
//     field.
package vendormock
