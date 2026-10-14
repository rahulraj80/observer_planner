package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// check is one lint: run returns one message per finding.
type check struct {
	name string
	run  func(root string) []string
}

var checks []check

// register adds a check; each check file calls it from init().
func register(name string, run func(root string) []string) {
	checks = append(checks, check{name: name, run: run})
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "copylint:", err)
		os.Exit(2)
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].name < checks[j].name })
	failed := 0
	for _, c := range checks {
		for _, msg := range c.run(root) {
			fmt.Printf("copylint: %s: %s\n", c.name, msg)
			failed++
		}
	}
	if failed > 0 {
		fmt.Printf("copylint: %d finding(s)\n", failed)
		os.Exit(1)
	}
	fmt.Printf("copylint: %d checks clean\n", len(checks))
}

// repoRoot walks up from the working directory to the directory holding
// go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the working directory")
		}
		dir = parent
	}
}

// inPublicTree reports whether root is the public repository, which carries
// no cmd/observer-org (scripts/release.sh PRIVATE_ONLY_PATHS).
func inPublicTree(root string) bool {
	info, err := os.Stat(filepath.Join(root, "cmd", "observer-org"))
	return err != nil || !info.IsDir()
}
