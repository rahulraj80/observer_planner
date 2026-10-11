package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// FileDefinesKey reports whether the TOML file at path sets the dotted key
// keys (for example "codeintel", "enabled") itself, as opposed to the key
// taking its Default() value. A missing file defines nothing (false, nil);
// a file that cannot be read or parsed returns the error, so a caller that
// only wants a hint can treat any error as "unknown" and stay quiet.
//
// It exists for upgrade notes about a changed default (D-307: an install
// that never wrote [codeintel].enabled moved from on to off), where "the
// effective value is X" is not enough: only an install that inherited the
// default is affected by a default change.
func FileDefinesKey(path string, keys ...string) (bool, error) {
	if path == "" || len(keys) == 0 {
		return false, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config.FileDefinesKey: read %s: %w", path, err)
	}
	var sink map[string]any
	meta, err := toml.Decode(string(body), &sink)
	if err != nil {
		return false, fmt.Errorf("config.FileDefinesKey: parse %s: %w", path, err)
	}
	return meta.IsDefined(keys...), nil
}
