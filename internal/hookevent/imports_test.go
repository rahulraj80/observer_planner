package hookevent

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins hookevent as pure data: of this module only the
// pure internal/integration registry.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"os/exec",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/") && p != "github.com/marmutapp/superbased-observer/internal/integration" {
				return "hookevent may import only internal/integration"
			}
			return ""
		},
		Reason: "hookevent is pure data",
	})
}

// TestFromBinding copies the binding's capability facts.
func TestFromBinding(t *testing.T) {
	b := integration.HookBinding{
		Cap: integration.HookCapToolGate, Phase: integration.HookPhasePre,
		NativeEvent: "PreToolUse", ProcRel: integration.ProcDescendant,
	}
	ev := FromBinding("qwen-code", b)
	if ev.Cap != b.Cap || ev.Phase != b.Phase || ev.Tool != "qwen-code" || ev.NativeEvent != "PreToolUse" || ev.ProcRel != b.ProcRel {
		t.Errorf("FromBinding = %+v", ev)
	}
	if Allow().Decision != DecisionAllow {
		t.Error("Allow() is not an allow")
	}
}
