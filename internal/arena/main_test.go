package arena

import (
	"fmt"
	"os"
	"testing"
)

// TestMain runs the package's git, the tests' own and the arena's under test,
// without the machine's global and system git config. A developer's
// core.hooksPath otherwise runs in every throwaway repo: a commit-msg hook
// that drops bot trailers deleted the arena's "Generated-by:" provenance
// line, so the keep tests failed on that machine and nowhere else. Each test
// repo sets the identity it commits with in its own config.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1"} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "arena tests: set %s: %v\n", k, err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
