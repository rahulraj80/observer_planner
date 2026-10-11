//go:build sbo_omit_ingest_otlp

package dashboard

func init() { testOmitted["ingest.otlp"] = true }
