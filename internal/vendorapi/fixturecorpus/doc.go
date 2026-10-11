// Package fixturecorpus is the contract of the vendor API fixture corpus
// (enterprise vendor API plan section 12A.1 and 12A.6, lane F3 / L0, task
// T-602). The corpus lives in testdata/vendorapi/<vendor>/<surface>/; every
// fixture file <name>.json (or .ndjson) has a sidecar <name>.source.json
// naming the public source it was derived from (URL, kind, date checked,
// spec or SDK version, the SHA-256 of the source document as fetched, the
// SHA-256 of the fixture bytes) and a per-field evidence ledger graded
// VerifiedOfficial, VerifiedSDK, ThirdParty or Inferred.
//
// Nothing in the corpus is captured from a real account: every fixture is
// derived from public documentation, official OpenAPI descriptions or SDK
// types, or synthesized from their schemas with reserved example values, and
// every sidecar says so (CapturedFromRealAccount is always false; Status is
// vendorsurface.StatusLabel).
//
// This package holds the sidecar type, the scenario vocabulary, the JSON
// field-path resolver used to check a ledger against its fixture, and the
// validator. It is pure (imports_test.go); the corpus walk happens in its
// tests.
package fixturecorpus
