package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// omitID is a developer component that can leave the developer distro on
// its own, found from the catalog.
func omitID(t *testing.T) string {
	t.Helper()
	for _, id := range components.InDistro(components.DistroDeveloper) {
		if _, p := components.ResolveManifest(components.Manifest{Schema: components.ManifestSchemaV1, Base: components.DistroDeveloper, Omit: []string{id}}); len(p) == 0 {
			return id
		}
	}
	t.Skip("no omittable developer component")
	return ""
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	//nolint:gosec // test fixture
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadManifestTOMLAndJSON(t *testing.T) {
	id := omitID(t)
	dir := t.TempDir()
	good := writeFile(t, dir, "m.toml", "schema = \"sbo-distro.v1\"\nname = \"acme\"\nbase = \"developer\"\nomit = [\""+id+"\"]\n")
	cb, err := loadManifest(good)
	if err != nil {
		t.Fatal(err)
	}
	canon := writeFile(t, dir, "m.json", string(components.CanonicalManifest(cb.Manifest, true)))
	back, err := loadManifest(canon)
	if err != nil || back.Distro != cb.Distro {
		t.Fatalf("canonical JSON round trip: %v %v", back.Distro, err)
	}
	typo := writeFile(t, dir, "typo.toml", "schema = \"sbo-distro.v1\"\nbase = \"developer\"\nomits = [\""+id+"\"]\n")
	if _, err := loadManifest(typo); err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Fatalf("a typo key must be refused: %v", err)
	}
	bad := writeFile(t, dir, "bad.toml", "schema = \"sbo-distro.v1\"\nbase = \"developer\"\nomit = [\"core\"]\n")
	if _, err := loadManifest(bad); err == nil || !strings.Contains(err.Error(), "core is in every build") {
		t.Fatalf("omitting core: %v", err)
	}
}

func TestRunCustomSubcommands(t *testing.T) {
	id := omitID(t)
	dir := t.TempDir()
	m := writeFile(t, dir, "m.toml", "schema = \"sbo-distro.v1\"\nbase = \"developer\"\nomit = [\""+id+"\"]\n")
	cb, err := loadManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) (int, string) {
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		return code, out.String() + errb.String()
	}
	if code, out := call("tags", "--manifest", m); code != 0 || strings.TrimSpace(out) != strings.Join(cb.Tags, ",") {
		t.Fatalf("tags: %d %q", code, out)
	}
	if code, out := call("ids", "--manifest="+m); code != 0 || strings.Count(out, "\n") != len(cb.Compiled) {
		t.Fatalf("ids: %d %q", code, out)
	}
	if code, out := call("validate", "--manifest", m); code != 0 || !strings.Contains(out, cb.Distro) {
		t.Fatalf("validate: %d %q", code, out)
	}
	if code, out := call("ldflags", "--manifest", m, "--version", "v9.9.9"); code != 0 ||
		!strings.Contains(out, "-X main.version=v9.9.9") || !strings.Contains(out, cb.LDFlag()) {
		t.Fatalf("ldflags: %d %q", code, out)
	}
	canon := filepath.Join(dir, "c.json")
	if code, out := call("manifest", "--manifest", m, "-o", canon); code != 0 || !strings.Contains(out, cb.ManifestDigest) {
		t.Fatalf("manifest: %d %q", code, out)
	}
	if raw, err := os.ReadFile(canon); err != nil || !bytes.Equal(bytes.TrimSpace(raw), components.CanonicalManifest(cb.Manifest, true)) {
		t.Fatalf("canonical file: %q %v", raw, err)
	}
	if code, _ := call("manifest"); code != 2 {
		t.Fatal("manifest without --manifest must be a usage error")
	}
	if code, _ := call("gen-docs", "--manifest", m); code != 2 {
		t.Fatal("gen-docs must refuse --manifest")
	}
	// The prebuilt paths are untouched.
	if code, out := call("tags", "--distro", "minimal"); code != 0 || strings.TrimSpace(out) != strings.Join(components.DistroTags(components.DistroMinimal), ",") {
		t.Fatalf("prebuilt tags: %d %q", code, out)
	}
}

// TestBuildComposesTheReproducibleBuild checks the go build argv and the
// files written beside the binary, with the go tool stubbed.
func TestBuildComposesTheReproducibleBuild(t *testing.T) {
	id := omitID(t)
	dir := t.TempDir()
	m := writeFile(t, dir, "m.toml", "schema = \"sbo-distro.v1\"\nbase = \"developer\"\nomit = [\""+id+"\"]\n")
	cb, err := loadManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	prev := runGo
	t.Cleanup(func() { runGo = prev })
	var calls [][]string
	var envs [][]string
	runGo = func(root string, env []string, stdout, stderr io.Writer, args ...string) error {
		calls = append(calls, args)
		envs = append(envs, env)
		if args[0] == "build" {
			return os.WriteFile(args[len(args)-2], []byte("binary"), 0o600)
		}
		_, _ = stdout.Write([]byte("buildinfo\n"))
		return nil
	}
	out := filepath.Join(dir, "out")
	var so, se bytes.Buffer
	if code := run([]string{"build", "--manifest", m, "--goos", "windows", "--goarch", "amd64", "--version", "v1.36.0", "--out", out, "--no-sbom"}, &so, &se); code != 0 {
		t.Fatalf("build: %d %s", code, se.String())
	}
	if len(calls) != 2 || calls[0][0] != "build" || calls[1][0] != "version" {
		t.Fatalf("go calls %v", calls)
	}
	argv := strings.Join(calls[0], " ")
	for _, want := range []string{"-trimpath", "-buildvcs=false", "-tags " + strings.Join(cb.Tags, ","), "-ldflags=-s -w -X main.version=v1.36.0 " + cb.LDFlag(), "./cmd/observer"} {
		if !strings.Contains(argv, want) {
			t.Errorf("go build argv lacks %q:\n%s", want, argv)
		}
	}
	if env := strings.Join(envs[0], " "); !strings.Contains(env, "GOOS=windows") || !strings.Contains(env, "CGO_ENABLED=0") {
		t.Errorf("env %s", env)
	}
	bin := filepath.Join(out, "observer-"+cb.Distro+"-windows-amd64.exe")
	for _, f := range []string{bin, bin + ".buildinfo.txt", filepath.Join(out, components.CustomManifestFileName(cb.Distro))} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

// TestProofRequiresTheStamp: a binary without the manifest stamp fails the
// custom proof even before anything else is compared.
func TestProofRequiresTheStamp(t *testing.T) {
	id := omitID(t)
	cb, p := components.ResolveManifest(components.Manifest{Schema: components.ManifestSchemaV1, Base: components.DistroDeveloper, Omit: []string{id}})
	if len(p) != 0 {
		t.Fatal(p)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prev := goListDeps
	t.Cleanup(func() { goListDeps = prev })
	goListDeps = func(root, cmdPath, tags, goos, goarch string) (proofDeps, error) {
		return proofDeps{pkgs: map[string]bool{}, modules: map[string]bool{}}, nil
	}
	rec, err := proveTarget(".", customTarget(cb), exe, false)
	if err != nil {
		t.Skipf("no build info in the test binary: %v", err)
	}
	if rec.Pass || !strings.Contains(strings.Join(rec.Failures, "\n"), "does not carry the custom manifest stamp") {
		t.Fatalf("failures %v", rec.Failures)
	}
	if rec.CustomDistro != cb.Distro || rec.Distro != string(components.DistroCustom) {
		t.Fatalf("record distro %q custom %q", rec.Distro, rec.CustomDistro)
	}
}
