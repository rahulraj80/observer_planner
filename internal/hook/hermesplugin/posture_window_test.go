package hermesplugin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guardposture"
)

// TestPostureWindowMatchesGo pins the plugin's posture freshness constants
// to the Go hook's (guardposture.MaxAge / MaxFutureSkew, T-050), so the
// Python bridge and `observer hook` agree on when a posture is stale.
func TestPostureWindowMatchesGo(t *testing.T) {
	src, err := pluginFS.ReadFile("__init__.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("POSTURE_MAX_AGE_SECONDS = %d\n", int(guardposture.MaxAge.Seconds())),
		fmt.Sprintf("POSTURE_SKEW_SECONDS = %d\n", int(guardposture.MaxFutureSkew.Seconds())),
		fmt.Sprintf("POSTURE_SCHEMA = %d\n", guardposture.Schema),
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("__init__.py lacks %q", strings.TrimSpace(want))
		}
	}
}
