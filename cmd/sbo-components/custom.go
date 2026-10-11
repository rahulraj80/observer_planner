package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// custom.go is the custom-build half of sbo-components (modular packaging
// plan 5.3 and phase P7, lane MP-CUST): it reads a component manifest
// (`sbo-distro.v1`, TOML or its canonical JSON), resolves it through the
// catalog (components.ResolveManifest, which refuses an inconsistent set
// and names why) and turns it into the build tags, the stamp, the proof
// expectations, the SBOM label and a reproducible build:
//
//	sbo-components manifest --manifest m.toml [--json] [-o canonical.json]
//	sbo-components tags     --manifest m.toml
//	sbo-components ldflags  --manifest m.toml [--version v]
//	sbo-components ids      --manifest m.toml
//	sbo-components proof    --manifest m.toml --binary <p> [--run]
//	sbo-components sbom     --manifest m.toml --binary <p>
//	sbo-components build    --manifest m.toml [--goos --goarch --version --out --proof --no-sbom]
//
// It never signs, publishes, tags or pushes anything: the vendor build
// service's signing step is `vendorsign sign` / `vendorsign index`, run by a
// workflow with an ephemeral key in this phase (.github/workflows/custom-build.yml).

// maxManifestBytes caps a manifest file: a full node catalog listed twice is
// a few KiB.
const maxManifestBytes = 64 * 1024

// loadManifest reads and resolves a manifest file. A .json file is the
// canonical form (the one a custom build's outputs carry); anything else is
// TOML. Unknown keys are refused in both, so a typo never silently drops a
// component from the build.
func loadManifest(path string) (components.CustomBuild, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the manifest the operator pointed this command at
	if err != nil {
		return components.CustomBuild{}, err
	}
	if len(raw) > maxManifestBytes {
		return components.CustomBuild{}, fmt.Errorf("%s: %d bytes, the cap is %d", path, len(raw), maxManifestBytes)
	}
	var m components.Manifest
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if m, err = components.DecodeManifestJSON(raw); err != nil {
			return components.CustomBuild{}, fmt.Errorf("%s: %w", path, err)
		}
	} else {
		md, derr := toml.Decode(string(raw), &m)
		if derr != nil {
			return components.CustomBuild{}, fmt.Errorf("%s: %w: %w", path, components.ErrManifestInvalid, derr)
		}
		if und := md.Undecoded(); len(und) > 0 {
			keys := make([]string, 0, len(und))
			for _, k := range und {
				keys = append(keys, k.String())
			}
			return components.CustomBuild{}, fmt.Errorf("%s: %w: unknown keys %v (schema, name, base, add, omit)", path, components.ErrManifestInvalid, keys)
		}
	}
	cb, problems := components.ResolveManifest(m)
	if len(problems) > 0 {
		return components.CustomBuild{}, fmt.Errorf("%s: %w:\n  %s", path, components.ErrManifestInvalid, strings.Join(problems, "\n  "))
	}
	return cb, nil
}

// customTarget is the proof expectation for a custom build.
func customTarget(cb components.CustomBuild) proofTarget {
	c := cb
	return proofTarget{
		label: cb.Distro, binary: components.BinaryNode, cmdPath: "./cmd/observer",
		wantTags: append([]string(nil), cb.Tags...), absentL1: cb.AbsentL1, l2Only: cb.L2Only,
		digest: cb.ComponentsDigest, distro: components.DistroCustom, custom: &c,
	}
}

// customLDFlags is the -ldflags value a custom build passes: the release
// build's (-s -w, the version) plus the stamp. Identical inputs give
// identical flags, which the reproducible double build relies on.
func customLDFlags(cb components.CustomBuild, version string) string {
	if version == "" {
		version = "dev"
	}
	return "-s -w -X main.version=" + version + " " + cb.LDFlag()
}

// customBinaryName is the custom binary's file name, the distro shape of
// scripts/build-distro.sh: observer-<custom distro>-<goos>-<goarch>[.exe].
func customBinaryName(cb components.CustomBuild, goos, goarch string) string {
	n := "observer-" + cb.Distro + "-" + goos + "-" + goarch
	if goos == "windows" {
		n += ".exe"
	}
	return n
}

// customBuildArgs is the go build argv (after "go") for one custom binary.
func customBuildArgs(cb components.CustomBuild, version, out string) []string {
	args := []string{"build", "-trimpath", "-buildvcs=false"}
	if len(cb.Tags) > 0 {
		args = append(args, "-tags", strings.Join(cb.Tags, ","))
	}
	return append(args, "-ldflags="+customLDFlags(cb, version), "-o", out, "./cmd/observer")
}

// runGo runs the go tool (a variable so tests never build).
var runGo = func(root string, env []string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// manifestFlag pulls --manifest out of args (any position), so the
// existing subcommands keep their flag sets.
func manifestFlag(args []string) (path string, rest []string, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--manifest" || a == "-manifest":
			if i+1 >= len(args) {
				return "", nil, false
			}
			path = args[i+1]
			i++
		case strings.HasPrefix(a, "--manifest=") || strings.HasPrefix(a, "-manifest="):
			_, path, _ = strings.Cut(a, "=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest, true
}

// runCustom handles every subcommand given --manifest. handled is false
// when args carry no --manifest, so the prebuilt paths run unchanged.
func runCustom(cmd string, args []string, stdout, stderr io.Writer) (code int, handled bool) {
	path, rest, ok := manifestFlag(args)
	if !ok {
		fmt.Fprintln(stderr, "--manifest needs a path")
		return 2, true
	}
	if path == "" {
		if cmd == "manifest" || cmd == "build" || cmd == "ldflags" {
			fmt.Fprintf(stderr, "usage: sbo-components %s --manifest <file.toml|file.json>\n", cmd)
			return 2, true
		}
		return 0, false
	}
	cb, err := loadManifest(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, true
	}
	switch cmd {
	case "manifest":
		return runManifestCmd(cb, rest, stdout, stderr), true
	case "tags":
		fmt.Fprintln(stdout, strings.Join(cb.Tags, ","))
		return 0, true
	case "ids":
		for _, id := range cb.Compiled {
			fmt.Fprintln(stdout, id)
		}
		return 0, true
	case "validate":
		fmt.Fprintf(stdout, "%s: %d components, digest %s\n", cb.Distro, len(cb.Compiled), cb.ComponentsDigest)
		return 0, true
	case "ldflags":
		fs := newFlagSet("ldflags", stderr)
		version := fs.String("version", "dev", "stamped into main.version")
		if err := fs.Parse(rest); err != nil {
			return 2, true
		}
		fmt.Fprintln(stdout, customLDFlags(cb, *version))
		return 0, true
	case "proof":
		return runProofTarget(customTarget(cb), rest, stdout, stderr), true
	case "sbom":
		return runSBOMTarget(customTarget(cb), rest, stdout, stderr), true
	case "build":
		return runBuild(cb, rest, stdout, stderr), true
	}
	fmt.Fprintf(stderr, "%s does not take --manifest\n", cmd)
	return 2, true
}

// runManifestCmd prints the resolution (or JSON) and optionally writes the
// canonical manifest.
func runManifestCmd(cb components.CustomBuild, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("manifest", stderr)
	asJSON := fs.Bool("json", false, "print the resolution as JSON")
	out := fs.String("o", "", "also write the canonical manifest (JSON) here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out != "" {
		//nolint:gosec // G306: a build input, never credentials or captured data.
		if err := os.WriteFile(*out, append(components.CanonicalManifest(cb.Manifest, true), '\n'), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if *asJSON {
		js, _ := json.MarshalIndent(cb, "", "  ")
		fmt.Fprintln(stdout, string(js))
		return 0
	}
	fmt.Fprintf(stdout, "%s (base %s, catalog v%d)\n", cb.Distro, cb.Manifest.Base, cb.CatalogVersion)
	fmt.Fprintf(stdout, "  manifest digest   %s\n", cb.ManifestDigest)
	fmt.Fprintf(stdout, "  components digest %s (%d components)\n", cb.ComponentsDigest, len(cb.Compiled))
	fmt.Fprintf(stdout, "  compiled out (L1) %d %v\n", len(cb.AbsentL1), cb.AbsentL1)
	fmt.Fprintf(stdout, "  inert only (L2)   %d %v\n", len(cb.L2Only), cb.L2Only)
	fmt.Fprintf(stdout, "  default active    %d\n", len(cb.DefaultSelection))
	fmt.Fprintf(stdout, "  tags              %s\n", strings.Join(cb.Tags, ","))
	return 0
}

// runBuild is `build --manifest`: the same reproducible build the release
// pipeline runs for a distro (scripts/build-distro.sh), plus the canonical
// manifest beside the binary for `vendorsign index`.
func runBuild(cb components.CustomBuild, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("build", stderr)
	goos := fs.String("goos", runtime.GOOS, "target GOOS")
	goarch := fs.String("goarch", runtime.GOARCH, "target GOARCH")
	version := fs.String("version", "dev", "stamped into main.version")
	out := fs.String("out", "bin/custom", "output directory")
	root := fs.String("root", ".", "repository root")
	proof := fs.Bool("proof", false, "run the proof gate (adds --run when the target is this host)")
	noSBOM := fs.Bool("no-sbom", false, "skip the CycloneDX SBOM")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := os.MkdirAll(*out, 0o755); err != nil { //nolint:gosec // a build output directory
		fmt.Fprintln(stderr, err)
		return 1
	}
	bin := filepath.Join(*out, customBinaryName(cb, *goos, *goarch))
	absBin, err := filepath.Abs(bin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	env := []string{"GOOS=" + *goos, "GOARCH=" + *goarch, "CGO_ENABLED=0"}
	fmt.Fprintf(stdout, ">> go build %s (%d tags)\n", filepath.Base(bin), len(cb.Tags))
	if err := runGo(*root, env, stdout, stderr, customBuildArgs(cb, *version, absBin)...); err != nil {
		fmt.Fprintf(stderr, "go build: %v\n", err)
		return 1
	}
	var info bytes.Buffer
	if err := runGo(*root, nil, &info, stderr, "version", "-m", absBin); err != nil {
		fmt.Fprintf(stderr, "go version -m: %v\n", err)
		return 1
	}
	//nolint:gosec // G306: release evidence, never credentials or captured data.
	if err := os.WriteFile(bin+".buildinfo.txt", info.Bytes(), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	//nolint:gosec // G306: the canonical manifest, a public build input.
	if err := os.WriteFile(filepath.Join(*out, components.CustomManifestFileName(cb.Distro)), append(components.CanonicalManifest(cb.Manifest, true), '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	t := customTarget(cb)
	if !*noSBOM {
		if code := runSBOMTarget(t, []string{"--binary", bin, "-o", bin + ".cdx.json"}, io.Discard, stderr); code != 0 {
			return code
		}
	}
	if *proof {
		pargs := []string{"--binary", bin, "-o", bin + ".proof.json", "--root", *root}
		if *goos == runtime.GOOS && *goarch == runtime.GOARCH {
			pargs = append(pargs, "--run")
		}
		if code := runProofTarget(t, pargs, stdout, stderr); code != 0 {
			return code
		}
	}
	fmt.Fprintf(stdout, "wrote %s\n", bin)
	return 0
}
