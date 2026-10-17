//go:build !sbo_omit_egress_pricing_feed

package main

import (
	"strings"
	"testing"
)

// TestEnrolledFeedLineAgreesWithTheGuardsPricingLine is the W2-6 pair: an
// enrolled node with from_org OFF applies NEITHER the public feed nor the
// org's rates, and both surfaces must say so. Moved from
// orgbudget_wire_test.go: enrolledFeedLine lives in pricing.go, which
// sbo_omit_egress_pricing_feed compiles out (REVIEW-4 P4-08).
func TestEnrolledFeedLineAgreesWithTheGuardsPricingLine(t *testing.T) {
	t.Parallel()

	on := enrolledFeedLine(true)
	if !strings.Contains(on, "prices come from the org rail") {
		t.Errorf("org-rail line = %q", on)
	}
	off := enrolledFeedLine(false)
	if !strings.Contains(off, "[guard.budget].from_org is off") {
		t.Errorf("opted-out line = %q, want it to name the switch the guard's pricing line names", off)
	}
	if strings.Contains(off, "prices come from the org rail") {
		t.Errorf("opted-out line = %q, must not claim the org rail applies", off)
	}
}
