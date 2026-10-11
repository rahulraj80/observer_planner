package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/components/sbom"
)

// proof.go is the per-artifact proof gate (modular packaging plan 5.5 and
// 5.9 "Release" row, lane MP-REL): a node distro binary, or an observer-org
// image binary (the P6b core-image proof, plan 9 row P6). It never trusts
// the build flags it was told: it reads the tags out of the binary itself,
// recomputes the dependency closure with `go list -deps` under those tags,
// and (for a node binary that runs on this host) asks the binary for its
// own compiled-set report. A proof passes only when:
//
//   - the binary's -tags equal the catalog's tags for the distro or image
//     (DistroTags / ImageTags), CGO is off and the build used -trimpath;
//   - every component the artifact excludes that CLAIMS L1 has none of its
//     packages in the closure (package absence is the only L1 proof the
//     plan accepts, 3.2 item 1);
//   - the modules the binary links (go version -m) equal the closure's
//     modules: the binary is this tree built with these tags, nothing more;
//     so a module an excluded component declares that the binary does not
//     link is proved absent, and one it still links is listed as shared
//     (another, included package imports it);
//   - with --run (node only): the binary reports the distro, its omitted set
//     equals the excluded L1 components and its distro_out set equals the
//     excluded components that are still L2 only.
//
// Excluded components that only TARGET L1 stay in the binary, inert and
// unselectable; the proof lists them under "l2_only" instead of failing, so
// the record says plainly how far each artifact is from the plan.

// proofRecord is the JSON evidence written per artifact.
type proofRecord struct {
	Distro           string            `json:"distro,omitempty"`
	CustomDistro     string            `json:"custom_distro,omitempty"`
	ManifestDigest   string            `json:"manifest_digest,omitempty"`
	Image            string            `json:"image,omitempty"`
	Binary           string            `json:"binary"`
	BinarySHA256     string            `json:"binary_sha256"`
	BinaryBytes      int64             `json:"binary_bytes"`
	GOOS             string            `json:"goos"`
	GOARCH           string            `json:"goarch"`
	Tags             []string          `json:"tags"`
	CatalogVersion   int               `json:"catalog_version"`
	ComponentsDigest string            `json:"components_digest"`
	AbsentL1         []string          `json:"absent_l1"`
	L2Only           []string          `json:"l2_only"`
	AbsentModules    []string          `json:"absent_modules"`
	SharedModules    map[string]string `json:"shared_modules"`
	Ran              bool              `json:"ran"`
	Failures         []string          `json:"failures"`
	Pass             bool              `json:"pass"`
}

// proofTarget is what one artifact must be, from the catalog alone.
type proofTarget struct {
	label    string
	binary   components.Binary
	cmdPath  string
	wantTags []string
	absentL1 []string
	l2Only   []string
	digest   string
	distro   components.Distro // node targets only
	image    components.Image  // org image targets only
	// custom is set for a custom build (custom.go): distro is then
	// DistroCustom and the binary must carry the manifest stamp.
	custom *components.CustomBuild
}

// distroTarget is the expectation for a prebuilt node distro.
func distroTarget(d components.Distro) proofTarget {
	in := map[string]bool{}
	for _, id := range components.InDistro(d) {
		in[id] = true
	}
	t := proofTarget{
		label: string(d), binary: components.BinaryNode, cmdPath: "./cmd/observer",
		wantTags: components.DistroTags(d), digest: components.Digest(components.InDistro(d)), distro: d,
	}
	t.absentL1, t.l2Only = splitExcluded(components.ForBinary(components.BinaryNode), func(c components.Component) bool { return !in[c.ID] })
	return t
}

// imageTarget is the expectation for an observer-org image (plan 3.4).
func imageTarget(i components.Image) proofTarget {
	t := proofTarget{
		label: "image " + string(i), binary: components.BinaryOrg, cmdPath: "./cmd/observer-org",
		wantTags: components.ImageTags(i), digest: components.Digest(components.InImage(i)), image: i,
	}
	t.absentL1, t.l2Only = splitExcluded(components.ForBinary(components.BinaryOrg), func(c components.Component) bool {
		return c.Images != nil && c.Images[i] == components.ImageOut
	})
	return t
}

// splitExcluded splits the excluded, non-core rows into the ones that claim
// L1 (must be absent) and the ones that are L2 only.
func splitExcluded(rows []components.Component, excluded func(components.Component) bool) (absentL1, l2Only []string) {
	absentL1, l2Only = []string{}, []string{}
	for _, c := range rows {
		if c.Class == components.ClassCore || !excluded(c) {
			continue
		}
		if c.Levels.Has(components.L1) {
			absentL1 = append(absentL1, c.ID)
		} else {
			l2Only = append(l2Only, c.ID)
		}
	}
	sort.Strings(absentL1)
	sort.Strings(l2Only)
	return absentL1, l2Only
}

// expectedSets is distroTarget's split, kept for the tests and callers that
// only need the node sets.
func expectedSets(d components.Distro) (absentL1, l2Only []string) {
	t := distroTarget(d)
	return t.absentL1, t.l2Only
}

// proofDeps is the go list result the proof needs.
type proofDeps struct {
	pkgs    map[string]bool // module-relative own packages
	modules map[string]bool // path@version of non-main modules
}

// goListDeps runs go list -deps for cmdPath under tags for goos/goarch.
var goListDeps = func(root, cmdPath, tags, goos, goarch string) (proofDeps, error) {
	args := []string{"list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}|{{with .Module}}{{.Path}}@{{.Version}}{{with .Replace}}={{.Path}}@{{.Version}}{{end}}{{end}}{{end}}"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, cmdPath)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return proofDeps{}, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	d := proofDeps{pkgs: map[string]bool{}, modules: map[string]bool{}}
	const mod = "github.com/marmutapp/superbased-observer"
	for _, line := range strings.Split(string(out), "\n") {
		path, m, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		if path == mod || strings.HasPrefix(path, mod+"/") {
			d.pkgs[strings.TrimPrefix(path, mod+"/")] = true
			continue
		}
		if m == "" {
			continue
		}
		if _, repl, ok := strings.Cut(m, "="); ok {
			m = repl
		}
		d.modules[m] = true
	}
	return d, nil
}

// runReport executes `<bin> components list --json` in a throwaway HOME.
var runReport = func(bin string) (components.Report, error) {
	home, err := os.MkdirTemp("", "sbo-proof-home-")
	if err != nil {
		return components.Report{}, err
	}
	defer os.RemoveAll(home)
	// The command runs in the throwaway HOME, so a relative path would
	// resolve against it.
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	cmd := exec.Command(bin, "components", "list", "--json") //nolint:gosec // G204: the artifact under proof.
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "PATH=/usr/bin:/bin"}
	cmd.Dir = home
	out, err := cmd.Output()
	if err != nil {
		return components.Report{}, fmt.Errorf("%s components list --json: %w", bin, err)
	}
	var rep components.Report
	if err := json.Unmarshal(out, &rep); err != nil {
		return components.Report{}, fmt.Errorf("decode the components report: %w", err)
	}
	return rep, nil
}

// prove runs the gate for a node distro artifact.
func prove(root string, d components.Distro, bin string, run bool) (proofRecord, error) {
	return proveTarget(root, distroTarget(d), bin, run)
}

// proveTarget runs the gate for one artifact.
func proveTarget(root string, t proofTarget, bin string, run bool) (proofRecord, error) {
	rec := proofRecord{
		Distro: string(t.distro), Image: string(t.image), Binary: filepath.Base(bin),
		CatalogVersion: components.CatalogVersion, ComponentsDigest: t.digest,
		Tags: []string{}, AbsentL1: t.absentL1, L2Only: t.l2Only, AbsentModules: []string{},
		SharedModules: map[string]string{}, Failures: []string{},
	}
	raw, err := os.ReadFile(bin)
	if err != nil {
		return rec, err
	}
	sum := sha256.Sum256(raw)
	rec.BinarySHA256 = hex.EncodeToString(sum[:])
	rec.BinaryBytes = int64(len(raw))
	bi, err := buildinfo.ReadFile(bin)
	if err != nil {
		return rec, fmt.Errorf("read build info of %s: %w", bin, err)
	}
	rec.GOOS, rec.GOARCH = sbom.Setting(bi, "GOOS"), sbom.Setting(bi, "GOARCH")
	rec.Tags = sbom.Tags(bi)
	fail := func(f string, a ...any) { rec.Failures = append(rec.Failures, fmt.Sprintf(f, a...)) }
	checkCustomStamp(&rec, t, raw, fail)

	want := append([]string{}, t.wantTags...)
	sort.Strings(want)
	if strings.Join(rec.Tags, ",") != strings.Join(want, ",") {
		fail("build tags %v, want the catalog's %s tags %v", rec.Tags, t.label, want)
	}
	if v := sbom.Setting(bi, "CGO_ENABLED"); v != "0" {
		fail("CGO_ENABLED=%q, want 0", v)
	}
	if v := sbom.Setting(bi, "-trimpath"); v != "true" {
		fail("built without -trimpath (reproducibility)")
	}

	deps, err := goListDeps(root, t.cmdPath, strings.Join(rec.Tags, ","), rec.GOOS, rec.GOARCH)
	if err != nil {
		return rec, err
	}
	linked := map[string]bool{}
	linkedPath := map[string]bool{}
	for _, m := range sbom.Modules(bi) {
		linked[m] = true
		p, _, _ := strings.Cut(m, "@")
		linkedPath[p] = true
	}
	for _, id := range t.absentL1 {
		c, _ := components.Get(id)
		for _, p := range c.Packages {
			if deps.pkgs[p] {
				fail("%s claims L1 and %s excludes it, but its package %s is in the closure", id, t.label, p)
			}
		}
		for _, m := range c.DirectModules {
			if linkedPath[m] {
				rec.SharedModules[m] = "linked: an included package imports it (package absence is the L1 proof)"
			} else if !contains(rec.AbsentModules, m) {
				rec.AbsentModules = append(rec.AbsentModules, m)
			}
		}
	}
	sort.Strings(rec.AbsentModules)
	for m := range linked {
		if !deps.modules[m] {
			fail("the binary links %s, which go list -deps with its tags does not reach", m)
		}
	}
	for m := range deps.modules {
		if !linked[m] {
			fail("go list -deps with the binary's tags reaches %s, which the binary does not link", m)
		}
	}

	if run && t.binary == components.BinaryNode {
		rec.Ran = true
		rep, err := runReport(bin)
		if err != nil {
			fail("%v", err)
		} else {
			if string(rep.Distro) != string(t.distro) {
				fail("the binary reports distro %q, want %q", rep.Distro, t.distro)
			}
			if got := strings.Join(rep.Omitted, ","); got != strings.Join(t.absentL1, ",") {
				fail("the binary reports omitted %v, want the excluded L1 components %v", rep.Omitted, t.absentL1)
			}
			if got := strings.Join(rep.DistroOut, ","); got != strings.Join(t.l2Only, ",") {
				fail("the binary reports distro_out %v, want the excluded L2-only components %v", rep.DistroOut, t.l2Only)
			}
			if rep.ComponentsDigest != t.digest {
				fail("the binary reports components digest %s, want %s (the %s compiled set)", rep.ComponentsDigest, t.digest, t.label)
			}
			checkCustomReport(rep, t, fail)
		}
	}
	rec.Pass = len(rec.Failures) == 0
	return rec, nil
}

// checkCustomStamp is the custom build's static check: the stamp is the -X
// string, so its bytes sit in the binary's data whatever the platform; build
// info does not record -ldflags under -trimpath, so this is how a
// cross-built binary is checked. A no-op for any other target.
func checkCustomStamp(rec *proofRecord, t proofTarget, raw []byte, fail func(string, ...any)) {
	if t.custom == nil {
		return
	}
	rec.CustomDistro, rec.ManifestDigest = t.custom.Distro, t.custom.ManifestDigest
	if !bytes.Contains(raw, []byte(t.custom.Stamp)) {
		fail("the binary does not carry the custom manifest stamp (%s): build it with sbo-components build --manifest", components.CustomStampVar)
	}
}

// checkCustomReport is the custom build's --run check: the binary resolves
// its own stamp to the expected custom distro id.
func checkCustomReport(rep components.Report, t proofTarget, fail func(string, ...any)) {
	if t.custom == nil {
		return
	}
	switch {
	case rep.Custom == nil:
		fail("the binary reports no custom build block")
	case rep.Custom.Error != "":
		fail("the binary's custom stamp does not resolve: %s", rep.Custom.Error)
	case rep.Custom.Distro != t.custom.Distro:
		fail("the binary reports custom distro %q, want %q", rep.Custom.Distro, t.custom.Distro)
	}
}

// targetFromFlags resolves --distro / --image.
func targetFromFlags(distro, image string) (proofTarget, bool) {
	switch {
	case distro != "" && image == "":
		d := components.Distro(distro)
		if !d.Valid() {
			return proofTarget{}, false
		}
		return distroTarget(d), true
	case image != "" && distro == "":
		i := components.Image(image)
		if i != components.ImageFull && i != components.ImageCore {
			return proofTarget{}, false
		}
		return imageTarget(i), true
	}
	return proofTarget{}, false
}

// runProof is the `proof` subcommand.
func runProof(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("proof", stderr)
	distro := fs.String("distro", "", "prebuilt node distro")
	image := fs.String("image", "", "observer-org image: full or core")
	bin := fs.String("binary", "", "the built observer (or observer-org) binary")
	run := fs.Bool("run", false, "node only: also run the binary (host GOOS/GOARCH) and compare its own report")
	out := fs.String("o", "", "write the JSON proof record here")
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	t, ok := targetFromFlags(*distro, *image)
	if !ok || *bin == "" {
		fmt.Fprintln(stderr, "usage: sbo-components proof (--distro <minimal|developer|managed|full> | --image <full|core> | --manifest <m.toml>) --binary <path> [--run] [-o proof.json]")
		return 2
	}
	return proveAndReport(t, *root, *bin, *out, *run, stdout, stderr)
}

// runProofTarget is `proof` for a target resolved elsewhere (a custom
// manifest): the same flags without --distro / --image.
func runProofTarget(t proofTarget, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("proof", stderr)
	bin := fs.String("binary", "", "the built observer binary")
	run := fs.Bool("run", false, "also run the binary (host GOOS/GOARCH) and compare its own report")
	out := fs.String("o", "", "write the JSON proof record here")
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bin == "" {
		fmt.Fprintln(stderr, "usage: sbo-components proof --manifest <m.toml> --binary <path> [--run] [-o proof.json]")
		return 2
	}
	return proveAndReport(t, *root, *bin, *out, *run, stdout, stderr)
}

// proveAndReport runs the gate, writes the record and prints the verdict.
func proveAndReport(t proofTarget, root, bin, out string, run bool, stdout, stderr io.Writer) int {
	rec, err := proveTarget(root, t, bin, run)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if rec.Ran && (rec.GOOS != runtime.GOOS || rec.GOARCH != runtime.GOARCH) {
		fmt.Fprintf(stderr, "note: --run executed a %s/%s binary on %s/%s\n", rec.GOOS, rec.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if out != "" {
		js, _ := json.MarshalIndent(rec, "", "  ")
		//nolint:gosec // G306: release evidence, never credentials or captured data.
		if err := os.WriteFile(out, append(js, '\n'), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	verdict := "PASS"
	if !rec.Pass {
		verdict = "FAIL"
	}
	fmt.Fprintf(stdout, "%s %s %s/%s %d bytes: %d compiled out (L1) %v, %d inert only (L2), declared modules absent %d shared %d, tags %d\n",
		verdict, t.label, rec.GOOS, rec.GOARCH, rec.BinaryBytes, len(rec.AbsentL1), rec.AbsentL1, len(rec.L2Only),
		len(rec.AbsentModules), len(rec.SharedModules), len(rec.Tags))
	for _, f := range rec.Failures {
		fmt.Fprintln(stdout, "  fail: "+f)
	}
	if !rec.Pass {
		return 1
	}
	return 0
}

// runSBOM is the `sbom` subcommand: the CycloneDX document of one artifact.
func runSBOM(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("sbom", stderr)
	distro := fs.String("distro", "", "prebuilt node distro the binary was built as")
	image := fs.String("image", "", "observer-org image the binary was built as: full or core")
	bin := fs.String("binary", "", "the built observer (or observer-org) binary")
	out := fs.String("o", "", "write the SBOM here (default stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	t, ok := targetFromFlags(*distro, *image)
	if !ok || *bin == "" {
		fmt.Fprintln(stderr, "usage: sbo-components sbom (--distro <minimal|developer|managed|full> | --image <full|core> | --manifest <m.toml>) --binary <path> [-o file.cdx.json]")
		return 2
	}
	return sbomFor(t, *bin, *out, stdout, stderr)
}

// runSBOMTarget is `sbom` for a target resolved elsewhere (a custom
// manifest).
func runSBOMTarget(t proofTarget, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("sbom", stderr)
	bin := fs.String("binary", "", "the built observer binary")
	out := fs.String("o", "", "write the SBOM here (default stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bin == "" {
		fmt.Fprintln(stderr, "usage: sbo-components sbom --manifest <m.toml> --binary <path> [-o file.cdx.json]")
		return 2
	}
	return sbomFor(t, *bin, *out, stdout, stderr)
}

// sbomFor renders and writes one artifact's SBOM.
func sbomFor(t proofTarget, bin, out string, stdout, stderr io.Writer) int {
	raw, err := os.ReadFile(bin) //nolint:gosec // the artifact under evidence
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	bi, err := buildinfo.ReadFile(bin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	sum := sha256.Sum256(raw)
	label := string(t.distro)
	switch {
	case t.image != "":
		label = "image-" + string(t.image)
	case t.custom != nil:
		label = t.custom.Distro
	}
	meta := sbom.Meta{
		Distro: label, CatalogVersion: components.CatalogVersion,
		ComponentsDigest: t.digest, BinarySHA256: hex.EncodeToString(sum[:]), Tool: "sbo-components",
	}
	if e := os.Getenv("SOURCE_DATE_EPOCH"); e != "" {
		var secs int64
		if _, err := fmt.Sscan(e, &secs); err == nil {
			meta.Timestamp = time.Unix(secs, 0)
		}
	}
	doc, err := sbom.Render(bi, meta)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if out == "" {
		_, _ = stdout.Write(doc)
		return 0
	}
	//nolint:gosec // G306: release evidence, never credentials or captured data.
	if err := os.WriteFile(out, doc, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", out)
	return 0
}

// contains reports whether xs holds x.
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
