package proxy

import (
	"context"
	"net/http"
	"time"
)

// captureToolVersion offers the client's own build version, read off the
// request's User-Agent product token (toolVersionFromHeaders), to the
// optional ToolVersionSink. It runs ONLY when the request is already bound
// to a session id: an unbound request has no session to stamp, and the
// proxy never invents one. Each session id costs at most one in-flight
// sink call per build version, and none at all once the sink reports
// that (session, version) settled; the write itself runs off the request
// path.
//
// OTLP is deliberately NOT a source for this column: gemini-cli reports
// the Node.js runtime, not its own build, in resource service.version, so
// a generic OTLP service.version mapping would stamp a runtime version as
// the tool's. Pinned by TestOTLPServiceVersionIsNotAToolVersionSource.
func (p *Proxy) captureToolVersion(h http.Header, sessionID string) {
	if p.toolVersionSink == nil || sessionID == "" {
		return
	}
	tool, version, ok := toolVersionFromHeaders(h)
	if !ok {
		return
	}
	// Keyed by (session, version), not session alone: a session whose
	// client is upgraded mid-session sends a NEW version under the same id,
	// and that is exactly the stamp the store's tool_version_last needs.
	key := sessionID + "\x00" + version
	if _, loaded := p.toolVersionSeen.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		settled, err := p.toolVersionSink.StampSessionToolVersion(ctx, sessionID, tool, version)
		if err != nil {
			p.logger.Warn("proxy: stamp tool version", "err", err)
		}
		if err != nil || !settled {
			// Not ingested yet (or a transient error): let a later
			// request for the same session and build try again.
			p.toolVersionSeen.Delete(key)
		}
	}()
}
