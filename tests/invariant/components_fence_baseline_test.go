package invariant

// componentsFenceBaseline is today's boundary debt: every import of an
// L1-target component's package from outside the component (see
// TestComponentsPackageFence), recorded 2026-10-07 at catalog v1. It is a
// ratchet, never a whitelist to grow: adding a line needs the
// orchestrator's sign-off, and removing one is how lanes MP-BND, MP-L1A,
// MP-L1B and MP-ORG show the boundary refactor progressing. Regenerate the
// list with SBO_COMPONENTS_PRINT_FENCE=1 when reviewing.
//
// Empty since lane MP-L1K (T-715): every node and org row is paid.
var componentsFenceBaseline = []string{}
