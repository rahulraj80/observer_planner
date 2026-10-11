// Package catalog holds the Vendor Connections surface catalog rows (plan
// section 4.3.2, lane F2, task T-603): one JSON file per vendor group under
// data/, embedded at build time, decoded strictly and validated by
// vendorsurface.ValidateCatalog.
//
// The first load was imported from the v5 research tables
// (docs/plans/enterprise-vendor-api-research-2026-10-07/v5/*-surfaces.md) by
// docs/plans/enterprise-vendor-api-research-2026-10-07/tools/catalog_export.py.
// From that import on the JSON files are the authored catalog: a change edits
// the row and cites its source. Every row is DocOnly and carries
// vendorsurface.StatusLabel.
//
// The package is pure (embedded data, no I/O at run time); imports_test.go
// pins it free of database/sql, net/http, os/exec and fsnotify.
package catalog
