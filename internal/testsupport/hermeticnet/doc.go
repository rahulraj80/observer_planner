// Package hermeticnet is an in-process network for egress tests (T-668): a
// fixed name table in place of DNS and a dialer that reaches only listeners
// the test itself registered (httptest servers on loopback, inside the test
// process). Every other dial is refused before any socket is opened and is
// recorded, so even a mutated (deliberately broken) egress check can never
// reach the network: it fails the test instead.
//
// TEST SUPPORT ONLY: no production command or package may import it
// (tests/invariant/vendormock_boundary_test.go). Every egress client a test
// builds passes Net as its Resolver and Net.Dial as its dialer, pinned by
// tests/invariant/egress_hermetic_test.go.
package hermeticnet
