package aggregatesvc

// imports_test.go carries no build tag on purpose (D-535): it reads the
// package's source files from disk, so it runs (and still guards svc.go)
// in a -tags sbo_omit_egress_aggregate_share build too, where it is the
// package's only compiled file and keeps `go test` from failing setup with
// "build constraints exclude all Go files".

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins the collector to reach raw I/O only through
// its seams (internal/store, internal/aggregateclient): no DIRECT database/sql,
// net/http or fsnotify import.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "the collector reaches raw I/O only through its seams",
	})
}
