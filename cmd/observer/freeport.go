package main

import "net"

// freeport.go holds freeTCPPort, which the benchmark ephemeral proxy and
// tests use. It is not in benchmark_ephemeral.go because that file compiles
// out with the benchmark component (lane MP-L1E).

// freeTCPPort asks the kernel for an unused loopback TCP port. There is a small
// TOCTOU window between close and the proxy re-binding it; acceptable for a
// short-lived benchmark daemon (the proxy bind is the authority - a lost race
// surfaces as a readiness timeout, not silent corruption).
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
