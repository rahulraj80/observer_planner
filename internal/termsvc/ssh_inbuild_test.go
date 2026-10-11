//go:build !sbo_omit_terminals_ssh

package termsvc

// sshInBuild mirrors ssh_argv.go's build line: an SSH launch composes an argv
// (ssh_inbuild_omit_test.go holds the other side, D-535).
const sshInBuild = true
