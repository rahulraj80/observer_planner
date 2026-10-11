//go:build sbo_omit_integration_shell_wrap

package dashapi

// ShellWrapService has no methods in a build without
// integration.shell-wrap: nothing can serve it, and this package does not
// import internal/shellwrap or internal/shellwrapsvc (lane MP-L1E; moved
// here from the dashboard's shellwrap_omit.go by lane MP-L1M).
type ShellWrapService interface{}
