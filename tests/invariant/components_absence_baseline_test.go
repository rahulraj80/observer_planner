package invariant

// componentsAbsenceRed lists the L1-target components whose packages are
// still in their binary when it is built with the component's omit tag (see
// TestComponentsL1PackageAbsence). Recorded 2026-10-07 in phase PB (lane
// MP-BND). It is a ratchet, like componentsFenceBaseline: adding a line
// needs the orchestrator's sign-off, and a component leaves the list when
// its wiring pair (P2) and the fence work make it absent, at which point it
// may claim L1. Regenerate with SBO_COMPONENTS_PRINT_ABSENCE=1 when
// reviewing.
var componentsAbsenceRed = []string{
	"capture.browser",
}
