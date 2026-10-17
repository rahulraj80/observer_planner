// Package conformtest is the one shared implementation of the WP-T3
// adapter-to-tooltax conformance pins (plan of record:
// docs/plans/tool-taxonomy-standardization-plan-2026-07-31.md section 2).
//
// Each adapter that still classifies tool names privately keeps a small
// internal/adapter/<tool>/tooltax_conformance_test.go that hands its REAL
// private classifier (and its acknowledged freezes) to these helpers, so
// the pin still runs inside the adapter's own test binary and reaches the
// unexported classifier, while the algorithm lives once here instead of in
// 25 drifting copies:
//
//   - AgreesWithTable is the TABLE to CLASSIFIER direction: every
//     tool-specific tooltax row the adapter classifies at all must agree.
//   - DomainIsSubset is the CLASSIFIER to TABLE direction for a switch
//     classifier: its case labels are extracted from source by
//     internal/tooltax/conformast and each must have an agreeing
//     tool-specific row, or be frozen with a reason.
//   - MapIsSubset is the same direction for an enumerable map classifier.
//
// TEST SUPPORT ONLY: imported from _test.go files, never from production
// code.
package conformtest
