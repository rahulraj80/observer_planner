package orgbudget

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins the composition layer PURE (CLAUDE.md rule 1): no I/O imports and, among
// observer packages, ONLY orgcontract and govern, so it composes from config/store/guard/policy without a cycle.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"os", "os/exec", "io", "io/ioutil", "path/filepath", "net/http", "database/sql", "github.com/fsnotify/fsnotify"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Except:            []string{"github.com/marmutapp/superbased-observer/internal/orgcontract", "github.com/marmutapp/superbased-observer/internal/govern"},
		Reason:            "orgbudget may import only orgcontract and govern",
	})
}
