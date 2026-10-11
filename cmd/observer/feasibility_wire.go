package main

import (
	"fmt"
	"io"
	"net"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/feasibility"
	"github.com/marmutapp/superbased-observer/internal/feasibility/nodefeas"
)

// feasibility_wire.go is the daemon's call into the node feasibility seam
// for a single setting (operator rule D-514, D-516): a wiring site that
// cannot honour a setting the config turns on asks for the one line to log,
// then starts with the setting off.

// settingOffLine evaluates nodefeas.SettingRules row id for a setting the
// config turns on (on). It returns the verdict's one line and true when the
// setting must start off. Component state is the binary's compiled set
// (componentstate.Legacy): a component compiled out of this build is
// unavailable with the "not in this build" reason; facts carries host facts
// (nodefeas.Fact*) that do not hold.
func settingOffLine(id string, on bool, facts map[string]string) (string, bool) {
	if !on {
		return "", false
	}
	rep := nodefeas.Settings(map[string]bool{id: true}, componentstate.Legacy().Result.Decisions, facts)
	v, ok := rep.Get(id)
	if !ok || v.Effect != feasibility.EffectOff {
		return "", false
	}
	return v.Line(), true
}

// feasibleDashboardAddr returns addr when the dashboard may bind it, and
// otherwise 127.0.0.1 on the same port, writing one WARN line to w from the
// dashboard.addr.nonloopback rule. checkRemoteBind (dashboard.CheckRemoteBind) stays the one
// owner of "may this address be exposed": a non-loopback bind needs a ready
// remote substrate. The downgrade keeps the dashboard unexposed, which is
// what the old refusal protected, and the daemon still starts (D-514).
func feasibleDashboardAddr(addr string, rc dashapi.RemoteController, w io.Writer) string {
	err := checkRemoteBind(addr, rc)
	if err == nil {
		return addr
	}
	line, off := settingOffLine("dashboard.addr.nonloopback", true, map[string]string{nodefeas.FactRemoteReady: err.Error()})
	if !off {
		line = "Dashboard on a non-loopback address ([dashboard].addr) is off: " + err.Error()
	}
	_, port, perr := net.SplitHostPort(addr)
	if perr != nil || port == "" {
		port = "8081"
	}
	safe := net.JoinHostPort("127.0.0.1", port)
	if w != nil {
		fmt.Fprintf(w, "WARN dashboard: %s Binding %s instead.\n", line, safe)
	}
	return safe
}
