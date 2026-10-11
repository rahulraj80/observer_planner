package proxyroute

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/credfile"
)

// The probe-lane route WRITERS (kimicode.go, qwencode.go, crush.go) and
// their removers (probe_remove.go) share one discipline (T-657, D-268 /
// D-270): each of the three files can hold plaintext provider keys, so a
// writer
//
//   - edits through the comment- and order-preserving cfgfmt engine with
//     ForbidReserialize: only the route keys change, every other byte stays,
//     and an edit cfgfmt could only make by re-serializing the file (a TOML
//     inline table on the path, for example) is refused;
//   - refuses a layout it cannot own: a config path that is a symbolic link
//     (replacing it would cut the link) or not a regular file, and a key on
//     the route path that holds a value of the wrong shape;
//   - keeps the file's permission bits and writes atomically (unique temp
//     file plus rename, writeFileKeepMode);
//   - leaves NO backup copy of the file (an older build wrote <file>.bak
//     beside it; that copy is the developer's to remove, never read here);
//   - never puts a value read from the file, nor a decoder's message, in an
//     error (credfile.SafeErrorf / credfile.Withhold).

// errRouteFileMissing marks an absent config file (a benign skip for a
// writer, "gone" for a remover).
var errRouteFileMissing = errors.New("config file absent")

// readOwnedRouteFile reads path after checking it is a layout the route
// writers own: a regular file, not a symbolic link. An absent file returns
// errRouteFileMissing.
func readOwnedRouteFile(what, path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errRouteFileMissing
	}
	if err != nil {
		return nil, credfile.Withhold(what, path, err)
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return nil, credfile.SafeErrorf("%s: %s is a symbolic link; Observer edits only a regular file it can replace in place, so nothing was written (point the tool at the real file or set the route by hand)", what, path)
	case !fi.Mode().IsRegular():
		return nil, credfile.SafeErrorf("%s: %s is not a regular file; nothing was written", what, path)
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errRouteFileMissing
	}
	if err != nil {
		return nil, credfile.Withhold(what, path, err)
	}
	return raw, nil
}

// parseOwnedRouteFile parses raw with f, refusing any edit that would need
// a re-serialization.
func parseOwnedRouteFile(what string, f cfgfmt.Format, path string, raw []byte) (cfgfmt.Doc, error) {
	doc, err := cfgfmt.ParseWith(f, raw, cfgfmt.Options{ForbidReserialize: true})
	if err != nil {
		return nil, credfile.Withhold(what, path, err)
	}
	return doc, nil
}

// routeEditError turns an edit failure into a safe message: an edit cfgfmt
// can only make by re-serializing names the shape, everything else is
// withheld.
func routeEditError(what, path string, key cfgfmt.Path, err error) error {
	if errors.Is(err, cfgfmt.ErrNeedsReserialize) || errors.Is(err, cfgfmt.ErrUnsupported) || errors.Is(err, cfgfmt.ErrTypeMismatch) {
		return credfile.SafeErrorf("%s: %s holds %s in a form Observer cannot edit in place (an inline table, or a value of another type on the path); nothing was written", what, path, key)
	}
	return credfile.Withhold(what, path, err)
}

// getTable returns the table / object at key (ok=false when absent or not
// a table).
func getTable(doc cfgfmt.Doc, key cfgfmt.Path) (map[string]any, bool) {
	v, found, err := doc.Get(key)
	if err != nil || !found {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// routeStringAt reads the string at key. present reports the key exists;
// an existing non-string value is a layout the writer does not own.
func routeStringAt(what, path string, doc cfgfmt.Doc, key cfgfmt.Path) (value string, present bool, err error) {
	v, found, gerr := doc.Get(key)
	if gerr != nil || !found {
		return "", false, nil //nolint:nilerr // a path crossing a non-table is handled by the caller's table check
	}
	s, ok := v.(string)
	if !ok {
		return "", true, credfile.SafeErrorf("%s: %s in %s is not a string; nothing was written", what, key, path)
	}
	return strings.TrimSpace(s), true, nil
}

// foreignRouteError is the refusal for a base URL that points somewhere
// other than an Observer proxy. The value is not quoted: it was read from a
// file that may hold credentials beside it.
func foreignRouteError(what, path string, key cfgfmt.Path) error {
	return credfile.SafeErrorf("%s: %s in %s already points at a non-observer URL; refusing to overwrite it", what, key, path)
}

// commitRouteDoc writes the edited document back keeping the file's mode.
func commitRouteDoc(what, path string, doc cfgfmt.Doc) error {
	if err := writeFileKeepMode(path, doc.Bytes()); err != nil {
		return credfile.Withhold(what, path, fmt.Errorf("write: %w", err))
	}
	return nil
}
