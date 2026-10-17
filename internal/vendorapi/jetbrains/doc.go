// Package jetbrains holds the pure drivers of the JetBrains IDE Services
// public REST API (lane ENTAPI-L16C, plan Phase 8, T-710): the user list.
//
// IDE Services is CUSTOMER-HOSTED: the server runs on the customer's own
// domain, so its catalog rows are customer_host rows. A connection names the
// host tuple itself (an admin-declared, audited per-connection host binding,
// docs/vendor-connections.md "Customer-hosted host binding"); the catalog fixes
// only the path and the auth contract.
//
// Written from the public references
// (https://www.jetbrains.com/help/ide-services/rest-api.html and
// .../import-users-via-rest-api.html), fetched on 2026-10-08; nothing here was
// observed from a live response. The surfaces are labelled "synthetic,
// awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package jetbrains
