package processobs

// TransportStatsSource is an OPTIONAL Backend capability: a backend whose
// events arrive over a transport a REMOTE capturer connects to, and which can
// therefore report whether that capturer has ever shown up.
//
// It is a capability, not a backend name (CLAUDE.md rule 3): the accept
// listener implements it today, and any future dial-in transport implements
// the same method rather than being special-cased by name upstream.
//
// The bool exists because a container backend (Composite) can only answer
// "yes" for its children: ok=false means NO transport of this kind exists,
// which every consumer must render as SILENCE, never as a zeroed transport.
// Zero connections on a configured transport and no transport at all are
// different facts and are the same honesty trap as MetricSample.NetMeasured.
type TransportStatsSource interface {
	TransportStats() (TransportStats, bool)
}

// TransportStatsOf probes any Backend for the TransportStatsSource capability
// and returns its stats. ok=false means the backend has no dial-in transport
// (the normal case: poll, eBPF, the spawn bridge) — the caller must then
// report nothing at all rather than a transport with zero connections.
func TransportStatsOf(b Backend) (TransportStats, bool) {
	src, ok := b.(TransportStatsSource)
	if !ok {
		return TransportStats{}, false
	}
	return src.TransportStats()
}

// TransportUnavailableSource is an OPTIONAL Backend capability: a backend
// that was assembled AFTER a requested dial-in transport failed to come up,
// and carries the reason it failed.
//
// It is the "requested but not running" half of the tri-state, and it is a
// capability rather than a config field so the fact travels with the backend
// that lost the transport — the daemon assembles that backend in one place
// and never has to re-derive whether a transport was wanted (CLAUDE.md rule
// 4: one owner per piece of state; Health is the single resolver).
//
// An empty reason means "nothing to report", never "unavailable for unknown
// reasons": a state with no reason is not actionable, so a backend that
// cannot say why must not claim the state.
type TransportUnavailableSource interface {
	TransportUnavailableReason() string
}

// TransportUnavailableReasonOf probes any Backend for the
// TransportUnavailableSource capability. An empty string means no requested
// transport failed — which callers must render as silence, exactly like an
// absent TransportStatsSource.
func TransportUnavailableReasonOf(b Backend) string {
	src, ok := b.(TransportUnavailableSource)
	if !ok {
		return ""
	}
	return src.TransportUnavailableReason()
}

// mergeTransportStats folds one child's stats into an aggregate. Counters SUM
// (two transports that each refused a connection refused two), Connected ORs
// (any live capturer means the feed is live), and the timestamps take the
// MAX (the most recent event is the one an operator is asking about). Addrs
// are joined so a multi-transport aggregate names every endpoint rather than
// silently reporting one of them.
//
// The auth-failure REASON is not merged like a counter: it is a single fact
// about a single refusal, so the aggregate keeps the one with the newest
// timestamp (a tie, or an unstamped reason, keeps the first non-empty). It is
// never concatenated — a spliced reason would be a sentence no daemon ever
// wrote.
func mergeTransportStats(agg, child TransportStats) TransportStats {
	agg.Connections += child.Connections
	agg.AuthFailures += child.AuthFailures
	if child.LastAuthError != "" &&
		(agg.LastAuthError == "" || child.LastAuthFailureAt.After(agg.LastAuthFailureAt)) {
		agg.LastAuthError = child.LastAuthError
		// The class is part of the SAME single fact as the reason and travels
		// with it: a class from one transport beside a reason from another
		// would be a diagnosis nothing recorded.
		agg.LastAuthErrorClass = child.LastAuthErrorClass
		agg.LastAuthFailureAt = child.LastAuthFailureAt
	}
	agg.Connected = agg.Connected || child.Connected
	if child.LastConnectAt.After(agg.LastConnectAt) {
		agg.LastConnectAt = child.LastConnectAt
	}
	if child.LastDisconnectAt.After(agg.LastDisconnectAt) {
		agg.LastDisconnectAt = child.LastDisconnectAt
	}
	if child.Addr != "" {
		if agg.Addr == "" {
			agg.Addr = child.Addr
		} else if agg.Addr != child.Addr {
			agg.Addr += "," + child.Addr
		}
	}
	// The capturer-decode counters SUM across transports for the same reason
	// the connection counters do — two capturers that each dropped an event
	// dropped two — but the PRESENCE flag ORs, and only a child that actually
	// reported may contribute. A child that never reported must not pull the
	// aggregate's flag true with its zeroes: "nothing was decoded" would then
	// render as "nothing failed to decode", which is the inversion this field
	// exists to prevent.
	if child.CapturerDecodeReported {
		agg.CapturerDecodeReported = true
		agg.CapturerDecode.NetworkDropped += child.CapturerDecode.NetworkDropped
		agg.CapturerDecode.NetworkUnsupportedVersion += child.CapturerDecode.NetworkUnsupportedVersion
		// The classified/ignored pair sums for the same reason, and the
		// aggregate's NothingClassified then reads across the whole feed: one
		// capturer that decoded data events is enough to say the layout table
		// still matches SOMEWHERE, which is the honest aggregate answer. A
		// per-host reading needs the per-host record, which is what the
		// health file already is.
		agg.CapturerDecode.NetworkDecoded += child.CapturerDecode.NetworkDecoded
		agg.CapturerDecode.NetworkIgnored += child.CapturerDecode.NetworkIgnored
		if child.CapturerDecodeAt.After(agg.CapturerDecodeAt) {
			agg.CapturerDecodeAt = child.CapturerDecodeAt
		}
	}
	return agg
}
