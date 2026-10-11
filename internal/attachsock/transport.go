package attachsock

import (
	"net"

	"github.com/marmutapp/superbased-observer/internal/localipc"
)

// transport.go re-exports the attach channel's OS transport, which moved to
// core internal/localipc (lane MP-L1K, T-715) because the MCP relay's IPC
// rides the same owner-only socket / named-pipe model. The protocol, server
// and client in this package are unchanged and stay in the terminals
// component.

// Transport is localipc.Transport.
type Transport = localipc.Transport

// Transport names, reported by Describe() for logs and doctor output.
const (
	TransportUnixSocket = localipc.TransportUnixSocket
	TransportNamedPipe  = localipc.TransportNamedPipe
	TransportNone       = localipc.TransportNone
)

// ErrUnsupported is localipc.ErrUnsupported.
var ErrUnsupported = localipc.ErrUnsupported

// ErrSocketLiveDaemon is localipc.ErrSocketLiveDaemon.
var ErrSocketLiveDaemon = localipc.ErrSocketLiveDaemon

// DefaultTransport is localipc.DefaultTransport.
func DefaultTransport() Transport { return localipc.DefaultTransport() }

// Supported is localipc.Supported.
func Supported() bool { return localipc.Supported() }

// Endpoint is localipc.Endpoint.
func Endpoint(dbPath string) (string, error) { return localipc.Endpoint(dbPath) }

// SocketPath is localipc.SocketPath.
func SocketPath(dbPath string) string { return localipc.SocketPath(dbPath) }

// ListenSocket is localipc.ListenSocket.
func ListenSocket(endpoint string) (net.Listener, error) { return localipc.ListenSocket(endpoint) }
