package diag

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/proxyroute"
	"github.com/marmutapp/superbased-observer/internal/remedy"
)

// checkRoutePort flags a persisted proxy route that points at a loopback
// port other than [proxy].port (U9, lane FIX-UPGRADE). `observer init`
// treats any loopback route as already set, so after a port change the
// route keeps the old port and every routed request fails to connect; only
// a forced route rewrite moves it. The fix names that exact command.
func checkRoutePort(homeDir string, cfg config.Config) Check {
	const name = "proxy.route_port"
	want := cfg.Proxy.Port
	if want <= 0 {
		return Check{Name: name, Status: StatusOK, Message: "[proxy].port is not set: route ports not compared"}
	}
	ports := proxyroute.ObserverRoutePorts(homeDir)
	var tools []string
	for tool, p := range ports {
		if p != want {
			tools = append(tools, tool)
		}
	}
	if len(tools) == 0 {
		if len(ports) == 0 {
			return Check{Name: name, Status: StatusOK, Message: "no tool routes through a local Observer proxy"}
		}
		return Check{Name: name, Status: StatusOK, Message: fmt.Sprintf("every local proxy route uses [proxy].port %d", want)}
	}
	sort.Strings(tools)
	var details, fixes []string
	var rems []remedy.Remediation
	for _, tool := range tools {
		details = append(details, fmt.Sprintf("%s routes to port %d", tool, ports[tool]))
		r := remedy.Of(remedy.InitToolRoute, tool)
		cmd := remedy.Command(r)
		if strings.Contains(cmd, "--all") {
			// No per-tool selector: a forced --all would rewrite every
			// tool's route, so point at the dashboard control instead.
			fixes = append(fixes, fmt.Sprintf("%s: set its route again from Settings > Connected tools", tool))
			continue
		}
		fixes = append(fixes, fmt.Sprintf("`%s --proxy-port %d --force`", cmd, want))
		rems = append(rems, r)
	}
	return Check{
		Name: name, Status: StatusWarn,
		Message: fmt.Sprintf("%d route(s) point at another port than [proxy].port %d, so their requests do not reach this proxy - fix with %s (turn the route off in any live session first)",
			len(tools), want, strings.Join(fixes, "; ")),
		Details:      details,
		Remediations: rems,
	}
}
