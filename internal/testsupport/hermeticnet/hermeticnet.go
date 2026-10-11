package hermeticnet

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"sort"
	"sync"
)

// ErrRefused is the answer to a dial of an address no test registered.
var ErrRefused = errors.New("hermeticnet: address is not an in-process listener; the dial was refused before any socket opened")

// Net is one test's in-process network. The zero value is not usable; call
// New.
type Net struct {
	mu      sync.Mutex
	names   map[string][]net.IP
	allowed map[string]bool
	refused []string
	lookups []string
	dialer  net.Dialer
}

// New returns an empty network: no name resolves and no address dials.
func New() *Net {
	return &Net{names: map[string][]net.IP{}, allowed: map[string]bool{}}
}

// AddHost makes host resolve to ips (in order). The addresses are answers
// only: dialing one still needs Allow or Serve.
func (n *Net) AddHost(host string, ips ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil {
			n.names[host] = append(n.names[host], ip)
		}
	}
}

// Serve registers an httptest server's listener as dialable and returns the
// server, so a test can write n.Serve(httptest.NewServer(h)).
func (n *Net) Serve(srv *httptest.Server) *httptest.Server {
	n.Allow(srv.Listener.Addr().String())
	return srv
}

// Allow registers one host:port of an in-process loopback listener. A
// non-loopback address is ignored: this network never reaches past the test
// process.
func (n *Net) Allow(addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.allowed[addr] = true
}

// LookupIPAddr answers from the name table only (it satisfies
// egress.Resolver). An unknown name is "not found"; nothing is sent to a DNS
// server.
func (n *Net) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lookups = append(n.lookups, host)
	ips, ok := n.names[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host (hermetic name table)", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		out = append(out, net.IPAddr{IP: ip})
	}
	return out, nil
}

// Dial connects only to a registered in-process listener. Anything else is
// recorded and refused with ErrRefused before a socket is opened.
func (n *Net) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	n.mu.Lock()
	ok := n.allowed[addr]
	if !ok {
		n.refused = append(n.refused, addr)
	}
	n.mu.Unlock()
	if !ok {
		return nil, ErrRefused
	}
	return n.dialer.DialContext(ctx, network, addr)
}

// Refused returns every address a dial was refused for, sorted.
func (n *Net) Refused() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := append([]string(nil), n.refused...)
	sort.Strings(out)
	return out
}

// Lookups returns every name resolved through the table, in order.
func (n *Net) Lookups() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.lookups...)
}
