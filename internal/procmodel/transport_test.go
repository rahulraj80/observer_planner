package procmodel

import "testing"

// TestNormalizeTransportAuthClass pins the closed vocabulary the metrics
// label is drawn from. Its whole job is to make the label's value set a
// property of THIS build rather than of what a remote sent.
func TestNormalizeTransportAuthClass(t *testing.T) {
	t.Parallel()
	for _, known := range transportAuthClasses {
		if got := NormalizeTransportAuthClass(known); got != known {
			t.Errorf("NormalizeTransportAuthClass(%q) = %q, want it unchanged", known, got)
		}
	}
	for _, hostile := range []string{"", "bad_token\" injected=\"1", "protocol_version ", "anything-else"} {
		if got := NormalizeTransportAuthClass(hostile); got != TransportAuthClassUnknown {
			t.Errorf("NormalizeTransportAuthClass(%q) = %q, want %q", hostile, got, TransportAuthClassUnknown)
		}
	}
}
