package orgclient

import (
	"bufio"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"
)

// Rails without a name constant elsewhere (the others live in orgpin.go,
// displaypolicy.go and update.go).
const (
	budgetPolicyRail  = "budget-policy"
	pricingPolicyRail = "pricing-policy"
	intelResultsRail  = "intel-results"
)

// railReply is the ONE classification of an answer from a node-pulled signed
// rail (display policy, announcement, routing, budget, pricing, intel, update
// manifest, policy bundle, policy resource), shared so every rail treats an
// older org server the same way.
//
// The compat invariant (CLAUDE.md "Teams / org-server invariants"): a newer
// node must work against an older server. A server that predates a rail does
// not answer with a clean 404 everywhere: its SPA catch-all serves the
// dashboard's index.html with a 200 for any unknown GET (demo org server,
// 2026-10-03: `observer org push-now` printed "Dashboard cost display: org
// policy not refreshed (... invalid character '<' ...)"). Decoding that page
// as the rail's document is an error on every push cycle; it is in fact the
// server saying "I do not serve this".
type railReply int

const (
	// railReplyAnswered means the server answered in the rail's own
	// vocabulary: a JSON 200, a 304, an auth refusal, a 409, a 5xx. The
	// rail's own status ladder applies.
	railReplyAnswered railReply = iota
	// railReplyNotSupported means the server does not serve this rail: a
	// 404 / 405 / 501, or a 200 whose body is not a JSON document. Every
	// rail keeps its last verified state on it and reports no error.
	railReplyNotSupported
)

// railNotSupportedStatuses are the statuses that mean "no such rail here".
var railNotSupportedStatuses = map[int]bool{
	http.StatusNotFound:         true,
	http.StatusMethodNotAllowed: true,
	http.StatusNotImplemented:   true,
}

// railSniffBytes bounds how much of a 200 body is inspected.
const railSniffBytes = 512

// classifyRailReply classifies resp and returns a short reason for a
// not-supported answer ("status 404", "html page", ...). For a 200 it peeks at
// the body WITHOUT consuming it: resp.Body is replaced by a reader that
// replays the peeked bytes, so the rail decodes exactly what was sent.
func classifyRailReply(resp *http.Response) (railReply, string) {
	if resp == nil {
		return railReplyAnswered, ""
	}
	if railNotSupportedStatuses[resp.StatusCode] {
		return railReplyNotSupported, "status " + strconv.Itoa(resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || resp.Body == nil {
		return railReplyAnswered, ""
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && mt == "text/html" {
		return railReplyNotSupported, "html page"
	}
	br := bufio.NewReaderSize(resp.Body, railSniffBytes)
	resp.Body = struct {
		io.Reader
		io.Closer
	}{br, resp.Body}
	peek, _ := br.Peek(railSniffBytes) //nolint:errcheck // a short peek (EOF) still shows the first bytes
	for _, b := range peek {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', '[':
			return railReplyAnswered, ""
		case '<':
			return railReplyNotSupported, "html page"
		default:
			return railReplyNotSupported, "non-JSON body"
		}
	}
	// An empty 200 is not an older server's page; the rail's decoder reports
	// it as the malformed answer it is.
	return railReplyAnswered, ""
}

// railNotSupportedLogged records which rails already logged their
// not-supported line in this process (at most once per rail).
var railNotSupportedLogged sync.Map

// classifyRail is classifyRailReply plus the one quiet log line: the first
// time a rail meets an UNAMBIGUOUS "not served" answer in this process it is
// logged at Info. A bare 404 is not logged (on several rails it also means
// "nothing published", a steady state the rails already handle quietly).
func (c *Client) classifyRail(rail string, resp *http.Response) railReply {
	class, reason := classifyRailReply(resp)
	if class != railReplyNotSupported || resp.StatusCode == http.StatusNotFound {
		return class
	}
	if _, seen := railNotSupportedLogged.LoadOrStore(rail, true); !seen && c != nil && c.logger != nil {
		c.logger.Info("org server does not serve this rail (it predates it); keeping the last verified state",
			"rail", rail, "answer", reason)
	}
	return class
}

// railStatus maps a not-supported answer onto the status the rail's own ladder
// already treats as "not supported / nothing published" (404), and passes
// every other status through. For rails whose ladder is a status switch.
func (c *Client) railStatus(rail string, resp *http.Response) int {
	if c.classifyRail(rail, resp) == railReplyNotSupported {
		return http.StatusNotFound
	}
	return resp.StatusCode
}
