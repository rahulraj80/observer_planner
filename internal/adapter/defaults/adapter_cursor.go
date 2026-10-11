//go:build !sbo_omit_adapter_cursor

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
)

// adapter.cursor (package internal/adapter/cursor). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_cursor_omit.go reports the component compiled out (L1).
func init() {
	register(8, func() adapter.Adapter { return cursor.New() })
}
