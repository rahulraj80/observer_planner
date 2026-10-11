package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
)

// TestStorageListsReclaimLeftovers pins T-493's read half: GET /api/storage
// lists the original database `observer archive reclaim` renamed aside
// (grouped with its -wal / -shm), and nothing that only resembles one.
func TestStorageListsReclaimLeftovers(t *testing.T) {
	tdir := t.TempDir()
	dbPath := filepath.Join(tdir, "observer.db")
	database, err := openTestDB(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	for name, size := range map[string]int{
		"observer.db.pre-reclaim-20260917T043021Z":     300,
		"observer.db.pre-reclaim-20260917T043021Z-wal": 20,
		"other.db.pre-reclaim-20260917T043021Z":        999, // another database
		"observer.db.pre-reclaim-latest":               999, // not a reclaim stamp
	} {
		if err := os.WriteFile(filepath.Join(tdir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server, err := New(Options{DB: database, DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/storage", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/storage = %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Leftovers []struct {
			Stamp string `json:"stamp"`
			Bytes int64  `json:"bytes"`
			Files []struct {
				Name string `json:"name"`
			} `json:"files"`
		} `json:"reclaim_leftovers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Leftovers) != 1 {
		t.Fatalf("reclaim_leftovers = %+v, want exactly one", resp.Leftovers)
	}
	l := resp.Leftovers[0]
	if l.Stamp != "20260917T043021Z" || l.Bytes != 320 || len(l.Files) != 2 || l.Files[0].Name != "observer.db.pre-reclaim-20260917T043021Z" {
		t.Fatalf("leftover = %+v", l)
	}
}
