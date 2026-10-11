package natslog

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

// storeDirRemoveBudget bounds how long a test waits for a stopped broker's
// store directory to go quiet before reporting it could not be removed.
const storeDirRemoveBudget = 10 * time.Second

// testStoreDir returns a fresh JetStream store directory for an embedded
// broker. It replaces t.TempDir() for broker stores (T-312): nats-server's
// file store can still finish an asynchronous write into the directory after
// Shutdown + WaitForShutdown returned, so t.TempDir's single RemoveAll lost
// that race under load and failed an otherwise passing test with "directory
// not empty". Call it BEFORE the broker is created so its removal is
// registered first and therefore runs after the broker's Close cleanup.
func testStoreDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "natslog-store-")
	if err != nil {
		t.Fatalf("store dir: %v", err)
	}
	t.Cleanup(func() {
		if err := removeAllSettled(dir, storeDirRemoveBudget); err != nil {
			t.Errorf("remove broker store %s: %v", dir, err)
		}
	})
	return dir
}

// removeAllSettled removes dir, retrying with a bounded backoff while a late
// writer is still adding entries, until it succeeds or budget runs out.
func removeAllSettled(dir string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	delay := 5 * time.Millisecond
	for {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
		if delay < 200*time.Millisecond {
			delay *= 2
		}
	}
}

// deleteConsumerSettled deletes a durable and waits until the broker has
// removed its store directory. nats-server finishes a consumer delete
// asynchronously, so a same-name durable created at once can have its fresh
// state file removed from under it: under load the re-create failed with a
// 500 "rename .../obs/<name>/o.dat.tmp ...: no such file or directory" (the
// sibling flake T-312's load proof surfaced). A test that drops and re-creates
// a durable waits here first, as a broker restart would.
func deleteConsumerSettled(t *testing.T, l *Log, name string) {
	t.Helper()
	srv, ok := l.srv.(*natsserver.Server)
	if !ok {
		t.Fatal("deleteConsumerSettled needs the embedded broker")
	}
	var dir string
	for _, root := range []string{srv.StoreDir(), filepath.Join(srv.StoreDir(), "jetstream")} {
		candidate := filepath.Join(root, "$G", "streams", l.opts.Stream, "obs", name)
		if _, err := os.Stat(candidate); err == nil {
			dir = candidate
			break
		}
	}
	if dir == "" {
		t.Fatalf("no store directory for durable %q under %s", name, srv.StoreDir())
	}
	if err := l.js.DeleteConsumer(t.Context(), l.opts.Stream, name); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(storeDirRemoveBudget)
	for {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("durable %q store directory still present %s after delete", name, storeDirRemoveBudget)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStoreDirRemovalOutlastsALateWriter pins T-312's cleanup mechanism: a
// writer that keeps adding files to the store after removal started (what a
// stopped broker's file store did under load) cannot fail the removal.
func TestStoreDirRemovalOutlastsALateWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	sub := filepath.Join(root, "jetstream", "streams", "s", "obs", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Like a file-store flush: write into the existing directory, never
		// recreate it once it is gone.
		for i := 0; i < 400; i++ {
			_ = os.WriteFile(filepath.Join(sub, "o"+strconv.Itoa(i)+".dat"), []byte("state"), 0o600)
			time.Sleep(250 * time.Microsecond)
		}
	}()
	err := removeAllSettled(root, storeDirRemoveBudget)
	wg.Wait()
	if err != nil {
		t.Fatalf("removal lost to the late writer: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store root still present after removal: %v", err)
	}
}

// TestBrokerStoresAreNotTempDirs (T-312): no embedded broker in this
// package's tests stores into t.TempDir(), whose one-shot RemoveAll is what
// failed under load; every broker store comes from testStoreDir.
func TestBrokerStoresAreNotTempDirs(t *testing.T) {
	bad := []*regexp.Regexp{
		regexp.MustCompile(`Embedded\([^,]+,\s*t\.TempDir\(\)`),
		regexp.MustCompile(`StoreDir:\s*t\.TempDir\(\)`),
		// A store dir held in a variable before it reaches Embedded.
		regexp.MustCompile(`dir\s*:?=\s*t\.TempDir\(\)`),
	}
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "storedir_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, re := range bad {
				if re.MatchString(line) {
					t.Errorf("%s:%d stores a broker in t.TempDir(); use testStoreDir(t): %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
