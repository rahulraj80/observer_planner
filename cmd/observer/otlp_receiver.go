package main

import (
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// otlp_receiver.go is the core half of the OTLP receiver wiring: the generic
// receiver (internal/ingest/otlp) is core because it serves Plane A traces
// as well as vendor logs (ruling D-436), so its token file is core too.

// otlpIngestTokenFile is the file (beside the DB) holding the receiver token a
// non-loopback OTLP receiver requires.
const otlpIngestTokenFile = "otlp-ingest-token" //nolint:gosec // G101: a file NAME, not a credential value

// otlpIngestTokenPath is where the receiver token lives for this config.
func otlpIngestTokenPath(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Observer.DBPath), otlpIngestTokenFile)
}

// doctorHome is the home directory doctor inspects: the report's override
// when set (tests, the dashboard's per-tool run), else the user's home.
func doctorHome(override string) string {
	if override != "" {
		return override
	}
	return userHomeOrEmpty()
}
