// Package drivers is the driver table of the enterprise vendor API plan
// (section 3: "driver table: surface id -> pure driver in
// internal/vendorapi/<vendor>"). It is the one place a surface id is
// mapped to a driver; the runner looks drivers up here and never names a
// vendor. Every entry's surface is Built in the catalog and every Built
// catalog row with a pull reach has an entry (drivers_test.go).
//
// Pure: it imports only the vendorapi contract and the per-vendor driver
// packages (imports_test.go pins it).
package drivers
