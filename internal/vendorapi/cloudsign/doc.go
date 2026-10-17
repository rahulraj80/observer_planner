// Package cloudsign holds the pure request signers and assertion builders of
// the cloud auth kinds (lane ENTAPI-L16C, T-710, enterprise vendor API plan
// Phase 8): AWS Signature Version 4, Alibaba Cloud ACS3-HMAC-SHA256
// (signature V3) and the Google service account JWT bearer assertion (RFC
// 7523).
//
// Each function signs exactly the request it is given (method, host, path,
// query, the headers it lists and the payload bytes) and returns the header
// values to set; it never sends anything. The egress seam
// (internal/orgserver/vendorconn/egress) is the only caller in production: it
// signs AFTER its URL policy, over the exact bytes it sends, and only for a
// surface of its closed signing tables.
//
// Inputs are restricted so that the canonical form and the bytes on the wire
// cannot disagree: a path is limited to RFC 3986 unreserved characters and
// "/", and the query is rendered by CanonicalQuery, which the seam also sends
// verbatim as the raw query.
//
// Golden vectors come only from the vendors' published examples (AWS
// signature test suite, Alibaba's "V3 request structure and signature"
// worked example); the JWT assertion is tested with an RSA key generated in
// the test. The package is pure (no SQL, HTTP, process or file system
// access; imports_test.go pins it).
package cloudsign
