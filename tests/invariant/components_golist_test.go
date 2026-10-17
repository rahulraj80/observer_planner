package invariant

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// components_golist_test.go is the shared go list -deps helper behind the
// modular-packaging catalog truth tests (plan section 3.2, lane MP-CAT).
// Package absence in `go list -deps` is the only L1 proof the plan
// accepts, so these tests read the real dependency graph rather than
// trusting the catalog.

const componentsModule = "github.com/marmutapp/superbased-observer"

// depPkg is one non-standard package in a binary's closure.
type depPkg struct {
	path    string // full import path
	module  string // module path ("" for none)
	imports []string
}

// depGraph is one binary's closure for one GOOS.
type depGraph struct {
	pkgs map[string]depPkg
}

// own reports whether a full import path is this module's.
func ownPkg(p string) bool {
	return p == componentsModule || strings.HasPrefix(p, componentsModule+"/")
}

// relPkg strips the module prefix.
func relPkg(p string) string { return strings.TrimPrefix(p, componentsModule+"/") }

var (
	depMu    sync.Mutex
	depCache = map[string]*depGraph{}
)

// goListDeps returns the closure of cmdPath for goos (and optional build
// tags), cached for the test binary's lifetime. It skips the test when the
// go tool is unavailable or cmdPath is absent from this tree (the public
// tree strips the org server).
func goListDeps(t *testing.T, cmdPath, goos, tags string) *depGraph {
	t.Helper()
	toolchainTest(t)
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(cmdPath))); err != nil {
		t.Skipf("%s is not in this tree", cmdPath)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		// The package-absence and fence checks are the only L1 proof
		// (plan 3.2 item 1), so CI may never skip them silently.
		if os.Getenv("CI") != "" {
			t.Fatal("go tool not on PATH under CI: the components L1 proof cannot run")
		}
		t.Skip("go tool not on PATH")
	}
	key := cmdPath + "|" + goos + "|" + tags
	depMu.Lock()
	defer depMu.Unlock()
	if g, ok := depCache[key]; ok {
		return g
	}
	args := []string{"list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}|{{with .Module}}{{.Path}}{{end}}|{{join .Imports \",\"}}{{end}}"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "./"+cmdPath)
	cmd := exec.Command(goBin, args...)
	cmd.Dir = root
	// -mod=mod is never passed: a dependency listing must not rewrite
	// go.mod. CGO is off to match the release build.
	cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	g := &depGraph{pkgs: map[string]depPkg{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		var imps []string
		if parts[2] != "" {
			imps = strings.Split(parts[2], ",")
		}
		g.pkgs[parts[0]] = depPkg{path: parts[0], module: parts[1], imports: imps}
	}
	depCache[key] = g
	return g
}

// hasRel reports whether the closure contains a module-relative package.
func (g *depGraph) hasRel(rel string) bool {
	_, ok := g.pkgs[componentsModule+"/"+rel]
	return ok
}

// moduleOf returns the module of a full import path in the closure.
func (g *depGraph) moduleOf(p string) string { return g.pkgs[p].module }

// componentsGOOS returns the GOOS values the node checks run over: linux
// always, windows and darwin unless -short (go-winio and dpapi exist only
// on windows, so a linux-only analysis misses them; plan risk R10).
func componentsGOOS() []string {
	if testing.Short() {
		return []string{"linux"}
	}
	return []string{"linux", "windows", "darwin"}
}

// binaryGOOS returns the GOOS set to analyse for one binary.
func binaryGOOS(b components.Binary) []string {
	if b == components.BinaryNode {
		return componentsGOOS()
	}
	return []string{"linux"}
}

// ownerIndex maps a module-relative package to the component that owns it,
// and to the components whose code it hosts, for one binary.
type ownerIndex struct {
	owner  map[string]string
	hosted map[string][]string
	byID   map[string]components.Component
}

func newOwnerIndex(b components.Binary) ownerIndex {
	ix := ownerIndex{owner: map[string]string{}, hosted: map[string][]string{}, byID: map[string]components.Component{}}
	for _, c := range components.ForBinary(b) {
		ix.byID[c.ID] = c
		for _, p := range c.Packages {
			ix.owner[p] = c.ID
		}
		for _, p := range c.HostedIn {
			ix.hosted[p] = append(ix.hosted[p], c.ID)
		}
	}
	return ix
}

// coreID returns the core row of a binary ("" when it has none).
func coreID(b components.Binary) string {
	for _, c := range components.ForBinary(b) {
		if c.Class == components.ClassCore {
			return c.ID
		}
	}
	return ""
}

// ownerOf returns the owning component of a package, core when unowned.
func (ix ownerIndex) ownerOf(b components.Binary, rel string) string {
	if o, ok := ix.owner[rel]; ok {
		return o
	}
	return coreID(b)
}
