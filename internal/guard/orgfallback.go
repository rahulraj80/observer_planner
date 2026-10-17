package guard

import (
	"strconv"

	"github.com/marmutapp/superbased-observer/internal/guardfallback"
)

// Org-configurable fallbacks (operator rulings 2026-10-04, D-221).
//
// The organization chooses, in the `[fallbacks]` table of its ORG-WIDE signed
// guard bundle, what two degraded states do on its managed nodes: an invalid
// user guard-policy.toml (T-327: loud, or fail closed) and a proxied tool with
// no sandbox available (D-178: recorded_acceptance, or org_disallow). The
// table rides the bundle this guard already verifies under the enrolment key
// pin, so the daemon and a hook process that build a guard from the same
// inputs read the same choice.

// OrgFallbacks is the ONE answer to "what does the organization's fallback
// choice mean on THIS node": the verified org bundle's `[fallbacks]` table on
// a managed node, the defaults on an individual node or when no org bundle is
// loaded (an unverifiable bundle is not loaded, so its table is unknown and
// the defaults apply).
//
// Tenancy follows orgLocksEveryRule: a process that cannot resolve its
// tenancy reads as managed, the same conservative reading the override lock
// and GateEnforcement apply (D-194). Callers: GateEnforcement (the hook
// failure posture), the dashboard launcher's sandbox decision.
func (g *Guard) OrgFallbacks() guardfallback.Fallbacks {
	es := g.set.Load()
	if es == nil || es.orgLayer == nil {
		return guardfallback.Fallbacks{}.Normalize()
	}
	return guardfallback.Effective(es.orgLayer.fallbacks, g.orgLocksEveryRule())
}

// UserPolicyBlockReason is the developer-facing reason a gated hook gives
// when it fails closed because the user policy does not load under the
// organization's fail_closed choice. It names the file and, when it can be
// placed, the line. One wording, shared by the hook reply, the doctor row and
// the dashboard banner.
func UserPolicyBlockReason(st UserPolicyStatus) string {
	where := st.Path
	if st.Line > 0 {
		where = st.Path + " line " + strconv.Itoa(st.Line)
	}
	msg := "Observer guard: blocked. Your organization requires gated hooks to fail closed while your guard policy does not load: " +
		where
	if st.Error != "" {
		msg += ": " + st.Error
	}
	return msg + ". Fix the file (dashboard Security > Rules > Advanced: policy layers, or `observer guard lint " + st.Path +
		"`); calls are allowed again as soon as it loads."
}
