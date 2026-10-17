package guidance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// osAdapterFile is the single file allowed to touch the real filesystem.
const osAdapterFile = "fs_os.go"

// TestNoForbiddenImports pins internal/guidance pure: os and path/filepath are
// allowed only in the one adapter file; everything else goes through the FS seam.
func TestNoForbiddenImports(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/db",
		},
		Custom: func(file, p string) string {
			if filepath.Base(file) != osAdapterFile && (p == "os" || p == "path/filepath") {
				return "filesystem access belongs in " + osAdapterFile + ", reached through the FS seam"
			}
			return ""
		},
		Reason: "internal/guidance must stay pure",
	})
	if _, err := os.Stat(osAdapterFile); err != nil {
		t.Fatalf("%s is missing: the adapter-only allowance points at nothing", osAdapterFile)
	}
}
