package procmodel

// Seed is a session attribution resolved from an external source (the
// pidbridge, or an adapter-specific session DB). The caller injects the
// lookup; this package never imports internal/pidbridge or internal/store.
type Seed struct {
	SessionID  string
	Tool       string
	ProjectID  int64
	Source     AttributionSource
	Confidence Confidence
}
