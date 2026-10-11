package dashboard

import (
	"strings"
	"testing"
)

// TestDashboardReasonsAreSentencesWithoutCLIDeadEnds pins T-374: reason
// strings the dashboard renders as prose end with a full stop, use no dash
// as a separator (neither an em / en dash nor a spaced hyphen) and point at
// a dashboard control rather than telling the user to run an `observer`
// command.
func TestDashboardReasonsAreSentencesWithoutCLIDeadEnds(t *testing.T) {
	cases := []struct{ name, text string }{
		{"session cost tab: no model", predictReasonNoModel},
		{"session cost tab: unpriced model", predictReasonUnpriced("big-pickle")},
		{"cloud sync: not signed in", cloudNotSignedInMessage},
		{"enrolment invite: not enrolled", notEnrolledInviteMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasSuffix(tc.text, ".") {
				t.Errorf("%q does not end with a full stop", tc.text)
			}
			if strings.ContainsAny(tc.text, "\u2013\u2014") || strings.Contains(tc.text, " - ") {
				t.Errorf("%q uses a dash as a separator", tc.text)
			}
			if strings.Contains(tc.text, "`observer ") || strings.Contains(tc.text, "observer init") {
				t.Errorf("%q points at a CLI command instead of a dashboard control", tc.text)
			}
		})
	}
}
