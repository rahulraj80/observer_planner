package vendorapi

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Offset paging (lane ENTAPI-L16C, T-710). Some admin APIs page by a
// record offset plus a page size and report the total (OpenRouter
// organization members, Prime team members: offset, limit, total_count). An
// offset cannot repeat the way a cursor token can, so the loop rule is
// carried in the cursor instead: the runner's opaque cursor is
// "<next offset>:<16 hex of sha256(last record id of the page)>", the request
// sends only the offset, and a page whose records include the previous
// page's last record is a paging loop (the same page served again, or an
// offset the vendor ignored), refused as schema drift by the driver. The
// driver also refuses a total that does not cover the page it served.

// OffsetLoopTail is the length of the record-id digest an offset cursor carries.
const OffsetLoopTail = 16

var offsetCursorRE = regexp.MustCompile(`^[0-9]{1,9}:[0-9a-f]{16}$`)

// OffsetOf reads the offset an offset cursor names: 0 for the first page
// (an empty cursor); ok false for anything that is not an offset cursor.
func OffsetOf(cursor string) (int, bool) {
	if cursor == "" {
		return 0, true
	}
	if !offsetCursorRE.MatchString(cursor) {
		return 0, false
	}
	n, err := strconv.Atoi(cursor[:strings.IndexByte(cursor, ':')])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func idDigest(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:OffsetLoopTail]
}

// OffsetLoop reports whether a page asked with cursor repeats the last record
// of the previous page (one of ids hashes to the cursor's tail).
func OffsetLoop(cursor string, ids []string) bool {
	if cursor == "" || !offsetCursorRE.MatchString(cursor) {
		return false
	}
	tail := cursor[strings.IndexByte(cursor, ':')+1:]
	for _, id := range ids {
		if idDigest(id) == tail {
			return true
		}
	}
	return false
}

// NextOffsetCursor returns the cursor of the page after one served at offset
// with ids (in order) out of total records: "" when the page set is complete
// (offset plus the page reaches the total). ok false means the page
// contradicts its own total (more records than the total covers, or an
// empty page while the total promises more): schema drift.
func NextOffsetCursor(offset, total int, ids []string) (string, bool) {
	n := len(ids)
	if offset < 0 || total < 0 || offset+n > total {
		return "", false
	}
	if offset+n == total {
		return "", true
	}
	if n == 0 {
		return "", false
	}
	return strconv.Itoa(offset+n) + ":" + idDigest(ids[n-1]), true
}
