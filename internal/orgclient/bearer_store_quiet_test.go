package orgclient

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestOpenBearerStoreQuietNeitherWarnsNorArms pins T-735 (D-547): a node
// that is not enrolled opens the bearer store without the keychain-downgrade
// WARN and without the sentinel, so the first real enrolment still warns.
func TestOpenBearerStoreQuietNeitherWarnsNorArms(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "org-bearer", ".keychain-unavailable")

	bs := OpenBearerStoreQuiet("sbo-test", dir)
	if bs.Backend() != "file" {
		t.Fatalf("backend = %q, want file", bs.Backend())
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("the quiet open armed the sentinel")
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	OpenBearerStore("sbo-test", dir, logger)
	if !bytes.Contains(logs.Bytes(), []byte("OS keychain unavailable")) {
		t.Fatalf("the first loud open after a quiet one did not warn:\n%s", logs.String())
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("the loud open did not arm the sentinel: %v", err)
	}
}
