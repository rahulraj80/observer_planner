package config

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// EncodeResolvedCompression renders a resolved compression parameter set as
// TOML under [compression], using the config file's own key names. It is the
// one renderer behind `observer profile show` and the dashboard's profile
// view, so both print the same keys a user can then set.
func EncodeResolvedCompression(c CompressionConfig) (string, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(struct {
		Compression CompressionConfig `toml:"compression"`
	}{c}); err != nil {
		return "", fmt.Errorf("config.EncodeResolvedCompression: %w", err)
	}
	return buf.String(), nil
}
