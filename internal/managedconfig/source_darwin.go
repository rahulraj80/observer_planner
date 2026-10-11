//go:build darwin

package managedconfig

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// The two macOS sources, in order: the MDM managed-preferences domain, then
// a root-owned file. Constant locations.
const (
	darwinPlistPath = "/Library/Managed Preferences/app.superbased.observer.plist"
	darwinTOMLPath  = "/Library/Application Support/SuperBased/managed.toml"
	// plutilPath is the system plist converter, by absolute path so PATH
	// cannot redirect it.
	plutilPath = "/usr/bin/plutil"
)

func probeOS() Probe {
	if p := readOwnedFile(darwinPlistPath, "plist"); p.Found {
		if p.FailedCheck != "" {
			return p
		}
		return plistToJSON(p)
	}
	return readOwnedFile(darwinTOMLPath, "toml")
}

// plistToJSON converts a (binary or XML) managed-preferences plist that
// already passed the ownership checks into JSON with plutil, reading the
// bytes from stdin so the converted document is exactly the checked one.
func plistToJSON(p Probe) Probe {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, plutilPath, "-convert", "json", "-o", "-", "--", "-")
	cmd.Stdin = bytes.NewReader(p.Data)
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil || len(out) > MaxBytes {
		p.FailedCheck, p.Data = "plist_convert", nil
		return p
	}
	p.Format, p.Data = "json", out
	return p
}
