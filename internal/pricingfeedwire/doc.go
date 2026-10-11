// Package pricingfeedwire holds the public pricing feed's wire types: the
// signed Envelope, its Rows and their optional Economics. It is a neutral core
// package (only internal/orgcontract for the embedded rate row) so the node's
// persisted feed copy (internal/store), the context-window ladder
// (internal/sessiongauge) and the cost engine wiring can read a stored feed
// without linking the egress.pricing-feed component; verification, the
// canonical bytes and the fetch client stay in internal/pricingfeed (lane
// MP-L1H, T-715).
package pricingfeedwire
