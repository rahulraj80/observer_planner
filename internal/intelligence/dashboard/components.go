package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/artifactstate"
	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/components/sbom"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// handleComponents serves GET /api/components: the read-only component
// catalog as this binary sees it (modular packaging plan, phase P0, lane
// MP-CAT). It is the same components.Report shape `observer components
// list --json` prints, built from the pure internal/components catalog and
// the binary's own compiled-set registry, so the CLI and the API cannot
// disagree.
//
// Registered VIEW and SectionNone, like /api/governance: it is how the SPA
// will learn which components exist (the nav filter and NotInstalled state
// of phase P1 consume it), so governance can never hide it. It reads no
// database, touches no file and makes no network call. `?all=1` adds the
// org server rows for reference; they are never compiled into the node.
//
// With Options.ComponentArtifacts wired, the report also carries
// artifact_states (T-707): the observed state of each separately installed
// artifact, the same statuses `observer components artifacts --json` prints,
// minus the filesystem paths of their items (the CLI shows those; this route
// is readable by a remote viewer). The field is additive: every catalog
// field keeps its shape.
func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	all := r.URL.Query().Get("all") == "1"
	rep := components.BuildReport(components.BinaryNode, all)
	var install *dashapi.BinaryInstall
	if s.opts.BinaryInstall != nil {
		bi := s.opts.BinaryInstall()
		install = &bi
	}
	if s.opts.ComponentArtifacts == nil && install == nil {
		writeJSON(w, rep)
		return
	}
	body := componentsReportWithStates{Report: rep, BinaryInstall: install}
	if s.opts.ComponentArtifacts != nil {
		body.ArtifactStates = artifactStatesForView(s.opts.ComponentArtifacts(r.Context()))
	}
	writeJSON(w, body)
}

// componentsReportWithStates is the /api/components body with the observed
// artifact states beside the catalog.
type componentsReportWithStates struct {
	components.Report
	ArtifactStates []artifactstate.Status `json:"artifact_states"`
	// BinaryInstall is how this binary was installed (U12); the embedded
	// report's "binary" is the product class, not a path.
	BinaryInstall *dashapi.BinaryInstall `json:"binary_install,omitempty"`
}

// artifactStatesForView drops the item paths (home directories, extension
// folders) from the statuses the dashboard serves.
func artifactStatesForView(in []artifactstate.Status) []artifactstate.Status {
	out := make([]artifactstate.Status, 0, len(in))
	for _, st := range in {
		items := make([]artifactstate.Item, 0, len(st.Items))
		for _, it := range st.Items {
			it.Path = ""
			items = append(items, it)
		}
		st.Items = items
		out = append(out, st)
	}
	return out
}

// sbomExeHash caches the hex SHA-256 of the running executable: the binary
// does not change under a running daemon, and the hash makes the download
// name the same artifact (and carry the same serial number) as the
// release's per-artifact SBOM.
var (
	sbomExeOnce sync.Once
	sbomExeHash string
)

func runningExecutableSHA256() string {
	sbomExeOnce.Do(func() {
		p, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.Open(p) //nolint:gosec // G304: this process's own executable.
		if err != nil {
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err == nil {
			sbomExeHash = hex.EncodeToString(h.Sum(nil))
		}
	})
	return sbomExeHash
}

// handleComponentsSBOM serves GET /api/components/sbom: the CycloneDX SBOM
// of this running binary (T-670, the Components page download; modular
// packaging plan 5.5, lane MP-REL). It is rendered by the same pure
// internal/components/sbom package the release pipeline uses per artifact
// (`sbo-components sbom`), from the process's own build information, so it
// lists exactly the modules this binary links and names its distro, build
// tags and components digest. Read-only and registered VIEW like
// /api/components: it reads the executable once to hash it and makes no
// network call.
func (s *Server) handleComponentsSBOM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		http.Error(w, "this binary carries no build information", http.StatusNotFound)
		return
	}
	rep := components.BuildReport(components.BinaryNode, false)
	doc, err := sbom.Render(bi, sbom.Meta{
		Distro:           string(rep.Distro),
		CatalogVersion:   rep.CatalogVersion,
		ComponentsDigest: rep.ComponentsDigest,
		BinarySHA256:     runningExecutableSHA256(),
		Tool:             "observer",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	version := strings.TrimPrefix(s.opts.Version, "v")
	if version == "" {
		version = "dev"
	}
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
	w.Header().Set("Content-Disposition", `attachment; filename="observer-`+string(rep.Distro)+`-`+version+`.cdx.json"`)
	_, _ = w.Write(doc)
}
