//go:build unix && !darwin

package managedconfig

// unixCeilingPath is the one Linux / BSD source. It is a constant location,
// never resolved from a flag, environment variable or project file.
const unixCeilingPath = "/etc/superbased/managed.toml"

func probeOS() Probe {
	return readOwnedFile(unixCeilingPath, "toml")
}
