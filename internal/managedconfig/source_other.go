//go:build !unix && !windows

package managedconfig

// probeOS on a platform with no defined ceiling source reports none.
func probeOS() Probe { return Probe{} }
