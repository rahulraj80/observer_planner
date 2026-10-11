package credfile

import (
	"errors"
	"fmt"
	"io/fs"
)

// ErrPatch is returned when a patch cannot be built or does not verify. It
// never carries file contents.
var ErrPatch = errors.New("the patched file did not verify; nothing was written")

// ErrDetailWithheld is wrapped by every error returned in place of a message
// that could quote a credential-bearing file.
var ErrDetailWithheld = errors.New("detail withheld because the file holds credentials")

// SafeError is an error whose message the caller composed only from paths,
// key names and event names, never from file contents. Withhold passes it
// through.
type SafeError struct{ msg string }

// Error implements error.
func (e *SafeError) Error() string { return e.msg }

// SafeErrorf builds a SafeError. Arguments must be paths, key names, event
// names or sizes: never a value read from a file.
func SafeErrorf(format string, args ...any) error {
	return &SafeError{msg: fmt.Sprintf(format, args...)}
}

// Withhold returns err unchanged when its message cannot carry file contents
// (a SafeError, an fs.PathError), and otherwise a fixed message
// naming only what and path, wrapping ErrDetailWithheld.
func Withhold(what, path string, err error) error {
	if err == nil {
		return nil
	}
	var safe *SafeError
	var pathErr *fs.PathError
	if errors.As(err, &safe) || errors.As(err, &pathErr) {
		return err
	}
	return fmt.Errorf("%s: %s was left unchanged (check that it parses and is writable): %w", what, path, ErrDetailWithheld)
}
