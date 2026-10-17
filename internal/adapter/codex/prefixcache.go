package codex

import (
	"bytes"
	"maps"
	"os"
	"sync"
)

// RESUMABLE PREFIX SCANS (T-413).
//
// An incremental parse of a rollout rebuilds its resume state by scanning
// the file from byte 0 to the cursor (prefetchSessionContext), and decodes
// every record on the way. A watcher poll of a growing rollout therefore
// cost a full re-read of everything before the cursor: on a 100 MB rollout,
// about a second of JSON decoding per poll, for a few new lines. The scan's
// result is a pure function of the bytes before the cursor, and a rollout
// only ever grows, so the scan state at the previous cursor is kept per path
// and the next poll advances it over the new bytes only.
//
// A cached scan is reused only for the SAME file (os.SameFile), only when
// the file has not shrunk below the cached position, and only when the
// bytes at the start of the file and just before the cached position are
// unchanged; anything else (a rotated, truncated or rewritten file) starts
// from byte 0 again, exactly as before.

// prefixScan is the resumable state of one prefix scan.
type prefixScan struct {
	pre       resumePrefix
	bytesRead int64
	lineNum   int
	out       sessionContext
	track     forkReplayTracker
}

// result is the resumePrefix the scan reached, as an independent copy: the
// parse mutates what it is handed, and the cached scan must not see that.
func (ps *prefixScan) result() resumePrefix {
	pre := ps.pre
	pre.ctx = ps.out
	pre.track = ps.track
	pre.lineCount = ps.lineNum
	pre.systemPrompts = maps.Clone(ps.pre.systemPrompts)
	pre.rateLimits = ps.pre.rateLimits.Clone()
	return pre
}

// clone is an independent copy of the scan, for the cache.
func (ps *prefixScan) clone() prefixScan {
	c := *ps
	c.pre.systemPrompts = maps.Clone(ps.pre.systemPrompts)
	c.pre.rateLimits = ps.pre.rateLimits.Clone()
	return c
}

const (
	// maxPrefixEntries bounds the cache: one entry per rollout the watcher
	// is tailing, oldest evicted first.
	maxPrefixEntries = 64
	// prefixGuardBytes is how many bytes at the start of the file and just
	// before the cached position must still match for a scan to resume.
	prefixGuardBytes = 64
)

type prefixEntry struct {
	fi   os.FileInfo
	scan prefixScan
	head []byte
	tail []byte
	tick uint64
}

type prefixCache struct {
	mu      sync.Mutex
	tick    uint64
	entries map[string]*prefixEntry
}

// resumePrefixes is the process-wide scan cache. It is keyed by path and
// holds no adapter state, so every adapter instance (codex and its retags)
// shares it safely.
var resumePrefixes = &prefixCache{entries: map[string]*prefixEntry{}}

// prefetchCached is prefetchSessionContext with the per-path scan cache: it
// resumes the scan cached for path when that scan is still valid for f and
// does not lie past until, and caches the advanced scan.
func prefetchCached(path string, f *os.File, until int64) (resumePrefix, bool) {
	fi, err := f.Stat()
	if err != nil {
		return prefetchSessionContext(f, until)
	}
	scan := resumePrefixes.take(path, f, fi, until)
	if !scan.advance(f, until) {
		return resumePrefix{}, false
	}
	resumePrefixes.put(path, f, fi, scan.clone())
	return scan.result(), true
}

// take returns a copy of the cached scan for path when it can be resumed
// toward until, or a zero scan (start from byte 0).
func (c *prefixCache) take(path string, f *os.File, fi os.FileInfo, until int64) prefixScan {
	c.mu.Lock()
	e := c.entries[path]
	var scan prefixScan
	var head, tail []byte
	ok := e != nil && os.SameFile(e.fi, fi) && e.scan.bytesRead <= until && e.scan.bytesRead <= fi.Size()
	if ok {
		scan, head, tail = e.scan.clone(), e.head, e.tail
	}
	c.mu.Unlock()
	if !ok || scan.bytesRead == 0 {
		return prefixScan{}
	}
	if gotHead, gotTail := prefixGuards(f, scan.bytesRead); !bytes.Equal(gotHead, head) || !bytes.Equal(gotTail, tail) {
		return prefixScan{}
	}
	return scan
}

// put caches scan for path, evicting the least recently stored entry when
// the cache is full.
func (c *prefixCache) put(path string, f *os.File, fi os.FileInfo, scan prefixScan) {
	head, tail := prefixGuards(f, scan.bytesRead)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	if _, ok := c.entries[path]; !ok && len(c.entries) >= maxPrefixEntries {
		var oldest string
		var oldestTick uint64
		first := true
		for k, v := range c.entries {
			if first || v.tick < oldestTick {
				oldest, oldestTick, first = k, v.tick, false
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[path] = &prefixEntry{fi: fi, scan: scan, head: head, tail: tail, tick: c.tick}
}

// prefixGuards reads the guard bytes: the first prefixGuardBytes of the file
// and the prefixGuardBytes ending at pos. A read error yields nil slices,
// which only match another failed read of the same position.
func prefixGuards(f *os.File, pos int64) (head, tail []byte) {
	n := int64(prefixGuardBytes)
	if pos < n {
		n = pos
	}
	if n <= 0 {
		return nil, nil
	}
	head = make([]byte, n)
	if _, err := f.ReadAt(head, 0); err != nil {
		head = nil
	}
	tail = make([]byte, n)
	if _, err := f.ReadAt(tail, pos-n); err != nil {
		tail = nil
	}
	return head, tail
}
