//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The source scan below finds every declared keychain service, the sandbox
// drivers' among them, so it runs where the sandbox is built and the
// sandbox seam (uninstall_keystore_sandbox.go) is filled.

// TestObserverKeystoreServicesAreListed: every keychain service name the
// source declares is in observerKeystoreServices, so uninstall removes it.
func TestObserverKeystoreServicesAreListed(t *testing.T) {
	root := filepath.Join("..", "..")
	re := regexp.MustCompile(`(?i)(KeystoreService|ServiceName|KeychainID)\b[^"\n]*"(sbo-[a-z0-9-]+-v[0-9]+)"`)
	seen := map[string]string{}
	for _, dir := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "orgserver") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				seen[m[2]] = p
			}
			return nil
		})
	}
	if len(seen) < 6 {
		t.Fatalf("the scan found only %v", seen)
	}
	for svc, at := range seen {
		if !slices.Contains(staticKeystoreServices(), svc) {
			t.Errorf("%s (declared in %s) is not in observerKeystoreServices or a sandbox row's KeystoreServices", svc, at)
		}
	}
}
