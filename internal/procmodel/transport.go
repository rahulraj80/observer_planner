package procmodel

import "time"

// TransportStats is a plain, backend-agnostic snapshot of a cross-OS capture
// TRANSPORT's connection health - the "is the other end actually talking to
// us?" half of the feature, which no other health signal reports.
//
// It exists because the two most likely real-world failures of a capturer
// that dials IN are otherwise completely silent: the elevated task was never
// set up (nothing ever connects - a WAITING state, not an error) and every
// connection is refused at the handshake (AuthFailures climbs while
// Connections stays 0). Those two look identical in every other surface, and
// only the counters below tell them apart. WHY a connection was refused is a
// separate fact from HOW MANY were - hence LastAuthError, which is the only
// thing that may be presented as the cause.
//
// The struct deliberately carries counters and timestamps only, never a
// transport-specific handle: it crosses the Backend seam and is re-published
// out of process (diag.ProcessHealth), so it must stay a value.
type TransportStats struct {
	// Addr is the transport's resolved endpoint (for the accept listener,
	// the bind address the capturer must dial). Empty when the transport
	// does not have one.
	Addr string
	// Connections counts remote capturers that connected AND authenticated
	// successfully, for the transport's lifetime.
	Connections int64
	// AuthFailures counts connections refused at the handshake, for ANY
	// reason: a bad shared token, a protocol version this daemon does not
	// speak, a malformed opening line, an unrelated local process probing
	// the port. Non-zero with Connections == 0 is the actionable case -
	// something is dialling and nothing is getting through - but the COUNTER
	// ALONE NAMES NO CAUSE. Read LastAuthError for what actually happened.
	AuthFailures int64
	// LastAuthError is the daemon's verbatim record of the most recent
	// refusal ("capturer speaks protocol v2, this daemon speaks v1",
	// "invalid token", "not a SBO-PROCESS-BRIDGE client", …). It exists
	// because AuthFailures conflates every handshake failure, and a surface
	// that turns that one counter into one named cause is asserting an
	// unverified diagnosis. Empty means no refusal has been recorded - never
	// "the reason was a token".
	LastAuthError string
	// LastAuthErrorClass is the BOUNDED classification of LastAuthError, one
	// of the TransportAuthClass* constants.
	//
	// It exists because LastAuthError is free-form text that quotes a
	// REMOTE-SUPPLIED fragment (the protocol version an unknown client
	// presented), and this listener's bind is reachable from any process on
	// the Windows host under WSL's localhostForwarding. Publishing that text
	// as a Prometheus label lets a hostile local process mint a fresh label
	// value per connection, and Prometheus retains every distinct series it
	// has ever seen - a memory-cost amplifier that no length clamp bounds,
	// because the clamp bounds each string's SIZE, not the NUMBER of distinct
	// strings. So the metrics surface carries THIS field and the text surface
	// (one line, one value, re-read on demand) carries the verbatim reason.
	//
	// Empty means no refusal has been classified - including a record written
	// by a daemon predating this field. Consumers render that as "unknown",
	// never as a specific cause.
	LastAuthErrorClass string
	// LastAuthFailureAt stamps LastAuthError. It is not published to the
	// operator surfaces; it exists so an aggregate over several transports
	// can pick the genuinely most recent reason instead of an arbitrary one.
	LastAuthFailureAt time.Time
	// Connected reports whether a capturer is streaming right now.
	Connected bool
	// LastConnectAt / LastDisconnectAt are zero until they first happen.
	// Both set, with Connected false, is a capturer that came and went -
	// which makes a flapping capturer visible instead of invisible.
	LastConnectAt    time.Time
	LastDisconnectAt time.Time

	// CapturerDecode carries the REMOTE capturer's own OS-telemetry decoder
	// counters, and is meaningful ONLY when CapturerDecodeReported is true.
	//
	// It rides here rather than on a capability of its own because the fact
	// is a property OF THIS TRANSPORT: it exists only while a capturer is
	// connected, it arrives only over this wire, and it dies with the
	// capturer that reported it. TransportStats is already the value that
	// crosses the Backend seam and is re-published out of process, so a
	// second capability would mean a second absence-vs-zero rule to
	// re-implement in every surface - which is the trap this whole file
	// exists to close.
	CapturerDecode CapturerDecodeStats
	// CapturerDecodeReported is the LOAD-BEARING presence flag, and false is
	// its zero value on purpose. A capturer that is not decoding OS network
	// telemetry at all (the common non-elevated run, where the ETW session
	// never started) reports NOTHING, and "0 events failed to decode" is a
	// completely different claim from "nothing was decoded": the first says
	// the payload-length assumptions HELD, the second says they were never
	// tested. Surfaces must render false as absence, never as a clean zero.
	CapturerDecodeReported bool
	// CapturerDecodeAt is when the daemon RECEIVED the most recent report.
	// It is stamped locally on receipt and never taken from the wire - a
	// remote-supplied clock is not a fact this daemon can stand behind.
	// Zero until the first report.
	CapturerDecodeAt time.Time
}

// CapturerDecodeStats is a remote capturer's report of how its own OS network-
// telemetry decoder is faring. It is plain counters and nothing else; presence
// is carried by TransportStats.CapturerDecodeReported (and by the bool of the
// capturer-side seam that produces it), never by a zero value in here.
//
// NetworkDropped is the single most important validation signal the cross-OS
// feed has: the capturer decodes fixed-layout kernel payloads by OFFSET, so a
// non-zero drop count means the payload-length assumption is wrong on that
// host - which is a wrong-number-with-no-error failure, not a noisy log line.
// Until this crossed the wire it reached no surface at all.
type CapturerDecodeStats struct {
	// NetworkDropped counts network data events the capturer's decoder
	// REFUSED because the payload was short or shaped unexpectedly. A
	// non-zero value means the fixed-offset layout assumptions do not hold
	// on that host and the byte totals cannot be trusted.
	NetworkDropped int64
	// NetworkUnsupportedVersion counts data events refused because the
	// provider stamped an event version the capturer's layout table does not
	// describe. Broken out from NetworkDropped because it names a specific,
	// actionable cause: the OS shipped a new template and the offsets may no
	// longer apply.
	NetworkUnsupportedVersion int64
	// NetworkDecoded counts data events the capturer's decoder ACCEPTED and
	// handed on to its byte accumulator - the positive half of the report,
	// and the only counter that says the decoder measured anything at all.
	//
	// It is here because "refused nothing" is not "decoded something": every
	// byte total the capture produces comes from an accepted event, so
	// NetworkDecoded == 0 means the per-process byte totals are necessarily
	// zero no matter how clean the two refusal counters look.
	NetworkDecoded int64
	// NetworkIgnored counts events the capturer's decoder saw and classified
	// as not-a-data-event - control-plane events, TCP connect / disconnect /
	// accept / retransmit, and UDP when no UDP handler is wired.
	//
	// A LARGE VALUE IS NORMAL AND HEALTHY on its own: those events genuinely
	// are not data events, and a busy host produces far more of them than of
	// data events. It is deliberately NOT part of Any() for exactly that
	// reason - see NothingClassified for the one shape in which it is a
	// signal, which is a conjunction and never this counter alone.
	NetworkIgnored int64
}

// Any reports whether either REFUSAL counter is non-zero - the "something is
// wrong with the decode" predicate, kept in one place so no surface re-derives
// it.
//
// NetworkIgnored is deliberately excluded. An ignored event is one the decoder
// correctly declined to treat as data, and a healthy elevated capture ignores
// a great many of them; folding that into this predicate would make every
// healthy capturer report a fault. NetworkDecoded is excluded for the mirror-
// image reason: it is a success count, and a decoder that has accepted nothing
// yet has still refused nothing.
func (c CapturerDecodeStats) Any() bool {
	return c.NetworkDropped > 0 || c.NetworkUnsupportedVersion > 0
}

// NothingClassified reports the ONE shape in which a clean-looking decode
// report is evidence of a broken decoder: events ARRIVED, none of them was
// classified as a data event, and none was refused either.
//
// It is a CONJUNCTION and it has to be, because no single counter expresses
// this failure. If the OS ships a Kernel-Network provider whose event ids are
// renumbered, Classify sends every event to ClassIgnored - by design, so that
// an unknown id degrades to "ignored" rather than being decoded against a
// layout that no longer describes it. The capturer then reports dropped 0,
// unsupported_version 0 and produces zero bytes, and every refusal-shaped
// check PASSES while the decoder measures nothing. Ignored alone cannot say
// that (a healthy capture ignores plenty); decoded == 0 alone cannot say it
// (a decoder that has not started, or a genuinely idle instant, also decodes
// nothing); "bytes are zero" alone cannot say it (an idle host is zero too).
// Only all three together do.
//
// It is mutually exclusive with Any() BY CONSTRUCTION: a report that refused
// something is already speaking through Any(), and two predicates that can
// both fire on the same report would leave every surface deciding which one
// to render.
//
// It is a SUSPICION, not a diagnosis, and surfaces must word it that way. The
// honest reading is "this decoder has not been shown to decode anything" -
// which on a host that was moving TCP traffic means the provider no longer
// matches this build's layout table, and on a genuinely idle host means the
// validation has not yet produced the evidence it needs. Both are states an
// operator must act on; neither is a clean pass.
func (c CapturerDecodeStats) NothingClassified() bool {
	return c.NetworkIgnored > 0 && c.NetworkDecoded == 0 && !c.Any()
}

// Dial-in capture transport states (HealthSnapshot.TransportState). They are
// the transport's counterpart to the NetworkAccounting* modes and exist for
// the same reason: "the operator did not ask for this" and "the operator
// asked for this and it is NOT running" are different facts, and rendering
// the second as the first (silence) is indistinguishable from a feature that
// was never enabled.
const (
	// TransportStateNone - no dial-in transport was requested. The surfaces
	// must say NOTHING about a capturer link; this is the 99% install.
	TransportStateNone = "none"
	// TransportStateUnavailable - a dial-in transport WAS requested and
	// could not be created (a bind conflict on the listen address, a token
	// file that cannot be written, the ETW block left disabled). No capturer
	// can ever connect, so it is louder than "nobody has connected yet", and
	// the reason is the whole point.
	TransportStateUnavailable = "unavailable"
	// TransportStateConfigured - a dial-in transport exists and its counters
	// are real, INCLUDING when they are all zero (the never-connected
	// waiting state).
	TransportStateConfigured = "configured"
)

// Handshake-refusal classes (TransportStats.LastAuthErrorClass). They are a
// CLOSED vocabulary on purpose: the class is what the metrics exporter turns
// into a label, so its cardinality must be a property of THIS build, never of
// what a remote sent. The verbatim reason keeps the detail on the text
// surfaces, where there is exactly one value at a time.
const (
	// TransportAuthClassTokenMismatch - the presented shared secret did not
	// match the daemon’s.
	TransportAuthClassTokenMismatch = "token_mismatch"
	// TransportAuthClassProtocolVersion - the client named a wire protocol
	// version this daemon does not speak, or one that would not parse. The
	// upgrade-skew case: the two halves are different observer builds.
	TransportAuthClassProtocolVersion = "protocol_version"
	// TransportAuthClassMalformed - the opening line was not a well-formed
	// capturer handshake at all (a port scanner, a stray browser, an
	// unrelated local process probing the port).
	TransportAuthClassMalformed = "malformed"
	// TransportAuthClassTransport - the handshake never completed for a
	// LOCAL reason: the socket died, or a deadline could not be set. Not a
	// statement about the client.
	TransportAuthClassTransport = "transport"
	// TransportAuthClassUnknown - a refusal was recorded with no class. It is
	// what a record written by an older daemon decodes to, and it is stated
	// as unknown rather than folded into one of the causes above.
	TransportAuthClassUnknown = "unknown"
)

// transportAuthClasses is the closed set, and the one owner of it.
var transportAuthClasses = []string{
	TransportAuthClassTokenMismatch,
	TransportAuthClassProtocolVersion,
	TransportAuthClassMalformed,
	TransportAuthClassTransport,
	TransportAuthClassUnknown,
}

// NormalizeTransportAuthClass maps any string onto the closed class
// vocabulary, returning TransportAuthClassUnknown for anything this build
// does not define (including the empty string).
//
// Consumers call it at the point they turn a class into a metric label, so
// the label's value set is bounded by this file no matter what the on-disk
// health record happens to contain.
func NormalizeTransportAuthClass(class string) string {
	for _, c := range transportAuthClasses {
		if c == class {
			return c
		}
	}
	return TransportAuthClassUnknown
}
