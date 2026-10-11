//go:build sbo_omit_terminals_ssh

package termsvc

// sshInBuild is false: ssh_argv_omit.go refuses every SSH launch in a build
// without terminals.ssh (D-535).
const sshInBuild = false
