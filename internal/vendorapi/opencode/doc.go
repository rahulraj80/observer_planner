// Package opencode holds the pure drivers of the opencode vendor group (lane
// ENTAPI-L16C, plan Phase 8, T-710): the OpenCode Console member budgets and
// the Charm Hyper team credit balance.
//
// Written from the public references (https://opencode.ai/v2/docs/console/api/budgets/
// and https://hyper.charm.land/docs/api/credits.html), fetched on 2026-10-08;
// nothing here was observed from a live response. The surfaces are labelled
// "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package opencode
