package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// recordEnds returns the byte offset after every terminated record.
func recordEnds(b []byte) []int64 {
	var out []int64
	for i, c := range b {
		if c == '\n' {
			out = append(out, int64(i+1))
		}
	}
	return out
}

// TestPrefixScanAdvanceMatchesFreshScan pins T-413's resumable prefix scan:
// advancing a scan step by step over record boundaries ends in exactly the
// state of one scan from byte 0, for every fixture rollout and every
// boundary. That equivalence is what makes caching the scan safe.
func TestPrefixScanAdvanceMatchesFreshScan(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "codex", "*.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		var step prefixScan
		ends := recordEnds(body)
		for i, end := range ends {
			// Advance to a point mid-record first: a straddling record must
			// be left for the next advance, whole.
			if i > 0 && end-ends[i-1] > 2 {
				if !step.advance(f, end-1) {
					t.Fatalf("%s: advance to %d failed", p, end-1)
				}
			}
			if !step.advance(f, end) {
				t.Fatalf("%s: advance to %d failed", p, end)
			}
			fresh, ok := prefetchSessionContext(f, end)
			if !ok {
				t.Fatalf("%s: fresh scan to %d failed", p, end)
			}
			if got := step.result(); !reflect.DeepEqual(got, fresh) {
				t.Fatalf("%s at %d: stepped scan %+v != fresh scan %+v", filepath.Base(p), end, got, fresh)
			}
		}
		f.Close()
	}
}

// TestPrefetchCachedResumesOnlyTheSameGrowingFile pins the cache's validity
// rules: a grown file resumes from the cached position, and a rewritten or
// truncated file starts from byte 0 again.
func TestPrefetchCachedResumesOnlyTheSameGrowingFile(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "codex", "rollout-gen-timing-real.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ends := recordEnds(body)
	half := ends[len(ends)/2]
	path := filepath.Join(t.TempDir(), "rollout-2026-10-05T10-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee.jsonl")
	if err := os.WriteFile(path, body[:half], 0o600); err != nil {
		t.Fatal(err)
	}
	open := func() (*os.File, os.FileInfo) {
		t.Helper()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		return f, fi
	}

	f, _ := open()
	if _, ok := prefetchCached(path, f, half); !ok {
		t.Fatal("prefetchCached failed")
	}
	// The file grows (append, same inode).
	af, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := af.Write(body[half:]); err != nil {
		t.Fatal(err)
	}
	af.Close()
	f2, fi2 := open()
	full := int64(len(body))
	if got := resumePrefixes.take(path, f2, fi2, full); got.bytesRead != half {
		t.Fatalf("grown file resumed from %d, want the cached %d", got.bytesRead, half)
	}
	cached, ok := prefetchCached(path, f2, full)
	if !ok {
		t.Fatal("prefetchCached failed")
	}
	fresh, _ := prefetchSessionContext(f2, full)
	if !reflect.DeepEqual(cached, fresh) {
		t.Fatalf("resumed scan differs from a fresh scan")
	}

	// Rewritten in place with different bytes before the cached position.
	rewritten := bytes.Replace(body, []byte(`"type"`), []byte(`"tYpe"`), 1)
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
	f3, fi3 := open()
	if got := resumePrefixes.take(path, f3, fi3, full); got.bytesRead != 0 {
		t.Fatalf("rewritten file resumed from %d, want a scan from byte 0", got.bytesRead)
	}
	// Truncated below the cached position.
	if err := os.WriteFile(path, body[:half], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := prefetchCached(path, f3, full); !ok {
		t.Fatal("prefetchCached failed")
	}
	f4, fi4 := open()
	if got := resumePrefixes.take(path, f4, fi4, full); got.bytesRead > fi4.Size() {
		t.Fatalf("truncated file resumed past its end (%d > %d)", got.bytesRead, fi4.Size())
	}
}

// BenchmarkResumeParseGrowingRollout measures one watcher poll of a growing
// rollout: a parse of the last record of a ~16 MB file. cold re-reads the
// whole prefix (the pre-T-413 cost of every poll); warm resumes the cached
// scan.
func BenchmarkResumeParseGrowingRollout(b *testing.B) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "codex", "rollout-gen-timing-real.jsonl"))
	if err != nil {
		b.Fatal(err)
	}
	var big []byte
	for len(big) < 16<<20 {
		big = append(big, body...)
	}
	ends := recordEnds(big)
	from := ends[len(ends)-2]
	path := filepath.Join(b.TempDir(), "rollout-2026-10-05T10-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee.jsonl")
	if err := os.WriteFile(path, big, 0o600); err != nil {
		b.Fatal(err)
	}
	a := New()
	for _, mode := range []string{"cold", "warm"} {
		b.Run(mode, func(b *testing.B) {
			if mode == "warm" {
				if _, err := a.ParseSessionFile(context.Background(), path, from); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "cold" {
					resumePrefixes.mu.Lock()
					delete(resumePrefixes.entries, path)
					resumePrefixes.mu.Unlock()
				}
				if _, err := a.ParseSessionFile(context.Background(), path, from); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
