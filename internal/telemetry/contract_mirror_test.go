package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenContractDoc is the subset of golden contract.json the mirror pins
// need (mirrors the contract package's reader).
type goldenContractDoc struct {
	SchemaVersion string `json:"schema_version"`
	Events        map[string]struct {
		Classification string `json:"classification"`
		Purpose        string `json:"purpose"`
	} `json:"events"`
}

func readContractGolden(t *testing.T) *goldenContractDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("contract", "golden", "contract.json"))
	if err != nil {
		t.Fatalf("read golden contract.json: %v", err)
	}
	var doc goldenContractDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse golden contract.json: %v", err)
	}
	return &doc
}

// TestContractPairsMatchGolden pins the collector-side event →
// (classification, purpose) map against the golden contract.json, so the
// batcher's stamping cannot fork from the contract the server validates.
func TestContractPairsMatchGolden(t *testing.T) {
	doc := readContractGolden(t)
	for name, spec := range doc.Events {
		pair, ok := contractPairs[name]
		if !ok {
			t.Errorf("golden event %q missing from contractPairs", name)
			continue
		}
		if pair[0] != spec.Classification || pair[1] != spec.Purpose {
			t.Errorf("contractPairs[%q] = (%s,%s), golden = (%s,%s)",
				name, pair[0], pair[1], spec.Classification, spec.Purpose)
		}
	}
	if len(contractPairs) != len(doc.Events) {
		t.Errorf("contractPairs has %d entries, golden has %d", len(contractPairs), len(doc.Events))
	}
}

// TestSchemaVersionMatchesGolden pins the schema timestamp the collector
// stamps on every event against the golden's declared schema_version.
func TestSchemaVersionMatchesGolden(t *testing.T) {
	doc := readContractGolden(t)
	if SchemaVersion != doc.SchemaVersion {
		t.Errorf("collector SchemaVersion %s != golden %s", SchemaVersion, doc.SchemaVersion)
	}
}
