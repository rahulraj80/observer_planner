//go:build sbo_omit_agentaccess_relay

package orgclient

// relayInBuild is false: store/mcprelay_omit.go composes no relay rows in a
// build without agentaccess.relay (D-535).
const relayInBuild = false
