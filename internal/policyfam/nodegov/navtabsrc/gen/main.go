// Command gen writes internal/policyfam/nodegov/navtabs_gen.go from the node
// dashboard's page tab tables. Run through `go generate
// ./internal/policyfam/nodegov`.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov/navtabsrc"
)

func main() {
	routes := flag.String("routes", "", "path to web/src/lib/pageTabRoutes.ts")
	tabs := flag.String("tabs", "", "path to web/src/lib/nodePageTabs.ts")
	out := flag.String("out", "", "path of the Go file to write")
	flag.Parse()
	if err := run(*routes, *tabs, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(routesPath, tabsPath, outPath string) error {
	if routesPath == "" || tabsPath == "" || outPath == "" {
		return fmt.Errorf("gen: -routes, -tabs and -out are required")
	}
	routes, err := os.ReadFile(routesPath)
	if err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	tabs, err := os.ReadFile(tabsPath)
	if err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	pages, err := navtabsrc.Parse(routes, tabs)
	if err != nil {
		return err
	}
	src, err := navtabsrc.Render(pages)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, src, 0o644); err != nil { //nolint:gosec // generated source file, world-readable like every other .go file
		return fmt.Errorf("gen: %w", err)
	}
	return nil
}
