package artifactstate

import "testing"

// TestHelperNotApplicableDetail is U13: a helper that ships beside the
// binary but serves another host shape (the Antigravity bridge outside WSL)
// says so, instead of "does not ship for this platform"; the generic detail
// stays for a helper that really does not ship.
func TestHelperNotApplicableDetail(t *testing.T) {
	st := Helper("artifact.antigravity-bridge", HelperInput{NotApplicableDetail: "ships beside the Linux binary for a daemon running under WSL; not used here"})
	if st.State != StateNotApplicable || st.Detail != "ships beside the Linux binary for a daemon running under WSL; not used here" {
		t.Fatalf("Helper = %+v", st)
	}
	if st := Helper("artifact.node-inspection", HelperInput{}); st.Detail != "this helper does not ship for this platform" {
		t.Fatalf("generic detail changed: %+v", st)
	}
}
