package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// index.go is the `vendorsign index` verb (modular packaging plan 5.6, lane
// MP-REL2): it builds release-index.json over every agent archive of one
// release, full and per-distro, and signs it with the vendor key. The org
// server refuses to import a distro archive the index does not list, and a
// node verifies the index against the key compiled into it, so this file is
// the one place a distro artifact and its components digest are vouched for.
//
// The components digest of each entry is the CATALOG's digest of that
// distro's compiled set (components.Digest(components.InDistro(d))), the
// value `sbo-components proof` checks the built binary against before this
// step runs, and the value a node's pre-swap probe (`components list --json`)
// compares the staged binary with. Release automation calls it twice: the
// dispatch-only staging workflow with an ephemeral test key, and, since
// v1.36.0 (T-729), npm-release.yml's public_release job with the real vendor
// key once the full and edition archives of a tag are signed.

// BuildIndexOptions is one index build.
type BuildIndexOptions struct {
	Dir     string
	Version string
	Key     ed25519.PrivateKey
}

// buildReleaseIndex hashes every agent archive in dir (full and distro
// names), checks it against SHA256SUMS and its .sig, and returns the
// canonical index bytes and the base64 signature over them.
func buildReleaseIndex(opts BuildIndexOptions) ([]byte, string, update.ReleaseIndex, error) {
	if opts.Key == nil {
		return nil, "", update.ReleaseIndex{}, errors.New("vendorsign index: no signing key")
	}
	sums, err := readSums(filepath.Join(opts.Dir, SumsFileName))
	if err != nil {
		return nil, "", update.ReleaseIndex{}, err
	}
	des, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, "", update.ReleaseIndex{}, fmt.Errorf("vendorsign: %w", err)
	}
	ix := update.ReleaseIndex{Schema: update.ReleaseIndexSchemaV1, Version: opts.Version, CatalogVersion: components.CatalogVersion}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		n, perr := update.ParseDistroArtifactName(de.Name())
		if errors.Is(perr, update.ErrArchiveTypeMismatch) {
			return nil, "", update.ReleaseIndex{}, fmt.Errorf("vendorsign: %w", perr)
		}
		if perr != nil {
			continue // SHA256SUMS, provenance, SBOMs, manifests, the index itself
		}
		if n.Version != opts.Version {
			return nil, "", update.ReleaseIndex{}, fmt.Errorf("vendorsign: %s is version %s but this index is for %s", de.Name(), n.Version, opts.Version)
		}
		a, aerr := buildArtifact(opts.Dir, de.Name(), n.OS, n.Arch, n.ArchiveType, sums)
		if aerr != nil {
			return nil, "", update.ReleaseIndex{}, aerr
		}
		distro := update.NormalizeDistro(n.Distro)
		digest, derr := entryComponentsDigest(opts.Dir, distro)
		if derr != nil {
			return nil, "", update.ReleaseIndex{}, derr
		}
		ix.Artifacts = append(ix.Artifacts, update.ReleaseIndexEntry{
			Filename: a.Filename, Kind: a.Kind, Distro: distro, OS: a.OS, Arch: a.Arch,
			ArchiveType: a.ArchiveType, SizeBytes: a.SizeBytes, SHA256: a.SHA256,
			Member: a.Member, MemberSHA256: a.MemberSHA256, AliasMembers: a.AliasMembers,
			ComponentsDigest: digest,
		})
	}
	if len(ix.Artifacts) == 0 {
		return nil, "", update.ReleaseIndex{}, fmt.Errorf("vendorsign index: no %s agent archives in %s", opts.Version, opts.Dir)
	}
	sort.Slice(ix.Artifacts, func(i, j int) bool { return ix.Artifacts[i].Filename < ix.Artifacts[j].Filename })
	body, err := update.CanonicalReleaseIndex(ix)
	if err != nil {
		return nil, "", update.ReleaseIndex{}, err
	}
	sig, err := update.SignReleaseIndex(opts.Key, body)
	if err != nil {
		return nil, "", update.ReleaseIndex{}, err
	}
	return body, sig, ix, nil
}

// entryComponentsDigest is the components digest one index entry vouches
// for. A prebuilt distro's is the catalog's digest of its compiled set. A
// custom build's (plan 5.3, lane MP-CUST) comes from the canonical manifest
// the custom build's outputs carry beside its archives
// (components.CustomManifestFileName): the manifest must hash to the id in
// the archive name and resolve through this tree's catalog, so the digest
// the vendor signs is the catalog's resolution of that manifest, never a
// value read from a file. A custom archive without its manifest is refused.
func entryComponentsDigest(dir, distro string) (string, error) {
	if !update.IsCustomDistro(distro) {
		return components.Digest(components.InDistro(components.Distro(distro))), nil
	}
	path := filepath.Join(dir, components.CustomManifestFileName(distro))
	raw, err := os.ReadFile(path) //nolint:gosec // a release asset directory the release job pointed this verb at
	if err != nil {
		return "", fmt.Errorf("vendorsign index: the custom archive %s needs its manifest %s: %w", distro, filepath.Base(path), err)
	}
	m, err := components.DecodeManifestJSON(raw)
	if err != nil {
		return "", fmt.Errorf("vendorsign index: %s: %w", filepath.Base(path), err)
	}
	cb, problems := components.ResolveManifest(m)
	if len(problems) > 0 {
		return "", fmt.Errorf("vendorsign index: %s does not resolve: %v", filepath.Base(path), problems)
	}
	if cb.Distro != distro {
		return "", fmt.Errorf("vendorsign index: %s hashes to %s, not the archive's %s", filepath.Base(path), cb.Distro, distro)
	}
	return cb.ComponentsDigest, nil
}

// runIndex writes release-index.json and release-index.json.sig into dir.
func runIndex(args []string, out *os.File) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	keyPath := fs.String("key", "", "path to the Ed25519 signing key (required)")
	dir := fs.String("dir", ".", "directory holding the release assets")
	version := fs.String("version", "", "release version, e.g. v1.36.0 (required)")
	allowUnpinned := fs.Bool("allow-unpinned-key", false,
		"sign with a key this tree's agents do NOT accept (the staging workflow's ephemeral key)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" || *version == "" {
		return errors.New("vendorsign index: -key and -version are required")
	}
	priv, err := loadPrivateKey(*keyPath)
	if err != nil {
		return err
	}
	warn, err := checkKeyIsPinned(priv, *allowUnpinned)
	if err != nil {
		return err
	}
	if warn != "" {
		fmt.Fprintln(out, warn)
	}
	body, sig, ix, err := buildReleaseIndex(BuildIndexOptions{Dir: *dir, Version: *version, Key: priv})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*dir, update.ReleaseIndexFileName), body, 0o644); err != nil { //nolint:gosec // a public release asset
		return fmt.Errorf("vendorsign index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*dir, update.ReleaseIndexSigFileName), []byte(sig+"\n"), 0o644); err != nil { //nolint:gosec // a public release asset
		return fmt.Errorf("vendorsign index: %w", err)
	}
	fmt.Fprintf(out, "wrote %s and %s (%d artifacts, catalog v%d)\n",
		update.ReleaseIndexFileName, update.ReleaseIndexSigFileName, len(ix.Artifacts), ix.CatalogVersion)
	for _, a := range ix.Artifacts {
		fmt.Fprintf(out, "  %-9s %-8s %-6s %s\n", a.Distro, a.OS, a.Arch, a.Filename)
	}
	return nil
}
