package update

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrUnrecognizedArtifact is returned by ParseArtifactName for a filename
// that is not an agent release archive. Callers that scan a whole release
// directory (SHA256SUMS, provenance, SBOMs, the observer-org archives ride
// beside the agent archives) skip on it; callers handed one file refuse.
var ErrUnrecognizedArtifact = errors.New("update: unrecognized artifact filename")

// ErrArchiveTypeMismatch wraps ErrUnrecognizedArtifact for a name that IS an
// agent archive but pairs a target with the wrong archive type (a zip for a
// POSIX target, a tarball for windows). A directory scanner must NOT skip
// this one silently: it is a mis-built release, not a foreign file.
var ErrArchiveTypeMismatch = errors.New("update: archive type does not match the target")

// artifactNameRe parses a release archive name into version / target /
// extension: `observer-v1.33.0-linux-x64.tar.gz`,
// `observer-v1.33.0-win32-x64.zip`, `observer-v1.34.0-rc.2-linux-x64.tar.gz`.
//
// THE ONE OWNER of this rule (tests/invariant pins it). Until 2026-09-17 the
// org server's importer and the release pipeline's manifest producer
// (scripts/vendorsign) each carried a copy; the first rc tag ever cut broke
// on the pre-release suffix, the copy in the importer was fixed, and the
// second rc broke on the copy the pipeline actually runs.
//
// A regexp rather than a split ladder because the target itself contains
// the separator, and because a name that does NOT match must be refused,
// not guessed at. The version group is a semver core with an optional
// pre-release suffix whose class excludes "-", so the os/arch tokens that
// follow can never be swallowed by it, and a plain core backtracks to no
// suffix. `observer-org-v1.33.0-linux-x64.tar.gz` fails at the `v[0-9]`
// group, which is what keeps the org-server archives out of the agent
// manifest.
var artifactNameRe = regexp.MustCompile(`^observer-(v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?)-([a-z0-9]+)-([a-z0-9_]+)\.(tar\.gz|zip)$`)

// ParseArtifactName derives the manifest facts a release archive's filename
// encodes: the version tag, the normalized GOOS / GOARCH (the pipeline's
// `win32`/`x64` spellings and Go's `windows`/`amd64` key identically) and
// the archive type, which must be the one DefaultArchiveTypeFor emits for
// that target.
//
// It parses ONLY the full build's name (today's unsuffixed archive). A
// distro archive (`observer-v1.36.0-minimal-linux-x64.tar.gz`) is refused
// with ErrDistroArtifact wrapped in ErrUnrecognizedArtifact, so a scanner
// building the v1 release manifest (which delivers only full, plan 5.6)
// skips it, and nothing ever records "v1.36.0-minimal" as a version.
// ParseDistroArtifactName parses both shapes.
func ParseArtifactName(name string) (version, goos, goarch, archiveType string, err error) {
	n, err := ParseDistroArtifactName(name)
	if err != nil {
		return "", "", "", "", err
	}
	if n.Distro != DistroFull || n.ExplicitDistro {
		return "", "", "", "", fmt.Errorf("%w: %w: %q is the %s distro (only the unsuffixed full archive belongs in a v1 manifest)",
			ErrUnrecognizedArtifact, ErrDistroArtifact, name, n.Distro)
	}
	return n.Version, n.OS, n.Arch, n.ArchiveType, nil
}

// ErrDistroArtifact marks a distro archive name handed to the v1-only
// parser.
var ErrDistroArtifact = errors.New("update: a distro archive is not a v1 artifact")

// ArtifactName is a parsed release archive name.
type ArtifactName struct {
	Version     string
	Distro      string
	OS, Arch    string
	ArchiveType string
	// ExplicitDistro reports that the name spelled a distro (even full),
	// as opposed to today's unsuffixed name, which means full.
	ExplicitDistro bool
}

// ParseDistroArtifactName parses a release archive name in either shape:
// today's `observer-<version>-<os>-<arch>.<ext>` (the full build) or the
// distro shape `observer-<version>-<distro>-<os>-<arch>.<ext>` (plan 5.4).
//
// The regexp's optional pre-release group is one dash-free segment, so in
// `observer-v1.36.0-minimal-linux-x64` it first swallows "-minimal" as a
// pre-release and leaves two platform tokens. A pre-release that is
// EXACTLY a distro name is therefore read as the distro: no release is
// ever tagged v1.36.0-minimal, while every rc and beta tag still parses as
// before. With a real pre-release the distro takes the extra segment
// (`observer-v1.36.0-rc.2-minimal-linux-x64`).
func ParseDistroArtifactName(name string) (ArtifactName, error) {
	var n ArtifactName
	// A custom build (plan 5.3, lane MP-CUST) spells its distro id
	// `custom-<64 hex>`, which carries the separator itself, so it has its
	// own shape and is tried first.
	if c := customArtifactNameRe.FindStringSubmatch(name); c != nil {
		n = ArtifactName{Version: c[1], Distro: c[2], OS: NormalizeOS(c[3]), Arch: NormalizeArch(c[4]), ArchiveType: c[5], ExplicitDistro: true}
		if want := DefaultArchiveTypeFor(n.OS); want != n.ArchiveType {
			return ArtifactName{}, fmt.Errorf("%w: %w: %q is a %s but the pipeline emits %s for %s",
				ErrUnrecognizedArtifact, ErrArchiveTypeMismatch, name, n.ArchiveType, want, n.OS)
		}
		return n, nil
	}
	m := artifactNameRe.FindStringSubmatch(name)
	if m == nil {
		m = distroArtifactNameRe.FindStringSubmatch(name)
		if m == nil {
			return n, fmt.Errorf("%w: %q (want observer-<version>[-<distro>]-<os>-<arch>.tar.gz|zip)", ErrUnrecognizedArtifact, name)
		}
		if !KnownDistro(m[2]) {
			return n, fmt.Errorf("%w: %q names unknown distro %q", ErrUnrecognizedArtifact, name, m[2])
		}
		n = ArtifactName{Version: m[1], Distro: m[2], OS: NormalizeOS(m[3]), Arch: NormalizeArch(m[4]), ArchiveType: m[5], ExplicitDistro: true}
	} else {
		n = ArtifactName{Version: m[1], Distro: DistroFull, OS: NormalizeOS(m[2]), Arch: NormalizeArch(m[3]), ArchiveType: m[4]}
		if i := strings.LastIndex(n.Version, "-"); i > 0 {
			if pre := n.Version[i+1:]; pre != "" && pre == strings.ToLower(pre) && KnownDistro(pre) {
				n.Version, n.Distro, n.ExplicitDistro = n.Version[:i], pre, true
			}
		}
	}
	if want := DefaultArchiveTypeFor(n.OS); want != n.ArchiveType {
		return ArtifactName{}, fmt.Errorf("%w: %w: %q is a %s but the pipeline emits %s for %s",
			ErrUnrecognizedArtifact, ErrArchiveTypeMismatch, name, n.ArchiveType, want, n.OS)
	}
	return n, nil
}

// distroArtifactNameRe is the distro shape with a real pre-release, which
// the main regexp cannot match (three tokens follow the version).
var distroArtifactNameRe = regexp.MustCompile(`^observer-(v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?)-([a-z]+)-([a-z0-9]+)-([a-z0-9_]+)\.(tar\.gz|zip)$`)

// customArtifactNameRe is the custom build shape:
// `observer-<version>-custom-<64 hex>-<os>-<arch>.<ext>`.
var customArtifactNameRe = regexp.MustCompile(`^observer-(v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?)-(custom-[0-9a-f]{64})-([a-z0-9]+)-([a-z0-9_]+)\.(tar\.gz|zip)$`)

// DistroArtifactName composes the distro archive name (plan 5.4):
// `observer-<version>-<distro>-<os>-<arch>.<ext>` with the release
// pipeline's platform spellings. The full build keeps today's unsuffixed
// name, which is what every v1 manifest and every pre-modular node knows.
func DistroArtifactName(version, distro, goos, goarch string) string {
	osTok, archTok := NormalizeOS(goos), NormalizeArch(goarch)
	if osTok == "windows" {
		osTok = "win32"
	}
	if archTok == "amd64" {
		archTok = "x64"
	}
	ext := DefaultArchiveTypeFor(goos)
	if d := NormalizeDistro(distro); d != DistroFull {
		return fmt.Sprintf("observer-%s-%s-%s-%s.%s", version, d, osTok, archTok, ext)
	}
	return fmt.Sprintf("observer-%s-%s-%s.%s", version, osTok, archTok, ext)
}
