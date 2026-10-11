package termsvc

import (
	"fmt"
	"os"
	"path/filepath"
)

// canonicalDir mirrors internal/termcore's unexported helper (the project-root
// validators moved there in lane MP-L1K, T-715) for the sandbox tests that
// compute the expected canonical workspace path.
func canonicalDir(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve real path (must exist): %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", real)
	}
	return real, nil
}
