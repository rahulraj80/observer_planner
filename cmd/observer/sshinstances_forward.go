//go:build !sbo_omit_terminals_ssh && !sbo_omit_terminals

package main

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/sshforward"
)

// newSSHInstances builds the instance switcher's forward manager, read
// through the live terminal config holder (T-488).
func newSSHInstances(termCfg *terminalConfigHolder, logger *slog.Logger) sshInstances {
	return forwardInstances{m: sshforward.New(sshforward.Options{
		Source: termCfg.SSHForward,
		Logger: logger,
	})}
}

// SSHForward is the sshforward.Options.Source: the instance switcher's view of
// the same profile list and switches termsvc enforces (T-488).
func (h *terminalConfigHolder) SSHForward() sshforward.Config {
	tc := h.terminal()
	return sshforward.Config{
		Enabled:    tc.Enabled && tc.SSH.Enabled,
		Profiles:   config.SSHProfiles(tc.SSH),
		SSHOptions: config.SSHOptions(tc.SSH),
	}
}

// forwardInstances projects the sshforward.Manager onto sshInstances. It adds
// no policy of its own: every authorization decision lives in the manager.
type forwardInstances struct{ m *sshforward.Manager }

func (f forwardInstances) Enabled() bool { return f.m.Enabled() }

func (f forwardInstances) List() []dashapi.InstanceInfo {
	rows := f.m.List()
	if len(rows) == 0 {
		return nil
	}
	out := make([]dashapi.InstanceInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, instanceInfo(r))
	}
	return out
}

func (f forwardInstances) Connect(name string) (dashapi.InstanceInfo, error) {
	inst, err := f.m.Connect(name)
	if err != nil {
		return dashapi.InstanceInfo{}, mapInstanceErr(err)
	}
	return instanceInfo(inst), nil
}

func (f forwardInstances) Disconnect(name string) error {
	if err := f.m.Disconnect(name); err != nil {
		return mapInstanceErr(err)
	}
	return nil
}

func (f forwardInstances) Get(name string) (dashapi.InstanceInfo, bool) {
	inst, ok := f.m.Get(name)
	if !ok {
		return dashapi.InstanceInfo{}, false
	}
	return instanceInfo(inst), true
}

func (f forwardInstances) Test(name string) (dashapi.InstanceTestResult, error) {
	r, err := f.m.Test(name)
	if err != nil {
		return dashapi.InstanceTestResult{}, mapInstanceErr(err)
	}
	return instanceTestResult(r), nil
}

func (f forwardInstances) TestProfile(p config.SSHProfileConfig) (dashapi.InstanceTestResult, error) {
	r, err := f.m.TestProfile(config.SSHProfile(p))
	if err != nil {
		return dashapi.InstanceTestResult{}, mapInstanceErr(err)
	}
	return instanceTestResult(r), nil
}

func (f forwardInstances) Close() { f.m.Close() }

// instanceInfo projects one manager row onto the dashboard wire shape.
func instanceInfo(r sshforward.Instance) dashapi.InstanceInfo {
	return dashapi.InstanceInfo{
		Name:          r.Name,
		Label:         r.Label,
		Target:        r.Target,
		DashboardPort: r.DashboardPort,
		HasKey:        r.HasKey,
		KeyHint:       r.KeyHint,
		Jump:          r.Jump,
		State:         string(r.State),
		LocalPort:     r.LocalPort,
		URL:           r.URL,
		Error:         r.Error,
	}
}

// instanceTestResult projects sshforward's TestResult onto the dashboard wire
// shape (milliseconds rather than a time.Duration, so the JSON stays a plain
// number).
func instanceTestResult(r sshforward.TestResult) dashapi.InstanceTestResult {
	return dashapi.InstanceTestResult{
		KnownHostsChecked: r.KnownHostsChecked,
		KnownHostsOK:      r.KnownHostsOK,
		AuthOK:            r.AuthOK,
		LatencyMS:         r.Latency.Milliseconds(),
		Stderr:            r.Stderr,
	}
}

// mapInstanceErr maps sshforward's sentinels onto the dashboard's, keeping the
// manager's own message (which is where the actionable known_hosts guidance
// lives) by wrapping rather than replacing.
func mapInstanceErr(err error) error {
	switch {
	case errors.Is(err, sshforward.ErrDisabled):
		return dashapi.ErrInstancesDisabled
	case errors.Is(err, sshforward.ErrUnknownProfile):
		return fmt.Errorf("%w: %w", dashapi.ErrInstanceUnknown, err)
	case errors.Is(err, sshforward.ErrInvalidProfile):
		return fmt.Errorf("%w: %w", dashapi.ErrInstanceInvalid, err)
	case errors.Is(err, sshforward.ErrHostKeyUnknown):
		return fmt.Errorf("%w: %w", dashapi.ErrInstanceHostKeyUnknown, err)
	case errors.Is(err, sshforward.ErrForwardFailed):
		return fmt.Errorf("%w: %w", dashapi.ErrInstanceForwardFailed, err)
	default:
		return err
	}
}
