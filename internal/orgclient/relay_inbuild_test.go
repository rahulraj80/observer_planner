//go:build !sbo_omit_agentaccess_relay

package orgclient

// relayInBuild mirrors store/mcprelaysummary.go's build line: the relay
// record chain is linked, so the push composes relay rows
// (relay_inbuild_omit_test.go holds the other side, D-535).
const relayInBuild = true
