package components

import "sort"

// view.go is the ONE JSON shape of the catalog, shared by `observer
// components list --json`, `observer components show --json` and the
// dashboard's read-only GET /api/components, so the CLI and the API can
// never disagree. Field names are snake_case and additive only.

// LevelsView is the JSON form of Levels.
type LevelsView struct {
	Implemented []Level `json:"implemented"`
	Target      []Level `json:"target"`
	Phase       string  `json:"phase,omitempty"`
}

// ListenerView is the JSON form of Listener.
type ListenerView struct {
	Proto     string  `json:"proto"`
	Addr      string  `json:"addr"`
	Loopback  bool    `json:"loopback"`
	Auth      string  `json:"auth"`
	ConfigKey string  `json:"config_key,omitempty"`
	Trigger   Trigger `json:"trigger"`
}

// EgressView is the JSON form of Egress.
type EgressView struct {
	Host    string  `json:"host"`
	Purpose string  `json:"purpose"`
	Gate    string  `json:"gate"`
	Trigger Trigger `json:"trigger"`
}

// SurfaceView is the JSON form of Surface.
type SurfaceView struct {
	Listeners     []ListenerView `json:"listeners"`
	Egress        []EgressView   `json:"egress"`
	FilesWritten  []string       `json:"files_written"`
	VendorConfigs []string       `json:"vendor_configs"`
	OSIntegration []string       `json:"os_integration"`
	Processes     []string       `json:"processes"`
	Credentials   []string       `json:"credentials"`
}

// TeardownView is the JSON form of TeardownStep.
type TeardownView struct {
	Owner string `json:"owner"`
	What  string `json:"what"`
}

// MigrationView is the JSON form of MigrationRef.
type MigrationView struct {
	File  string         `json:"file"`
	Class MigrationClass `json:"class"`
	Note  string         `json:"note,omitempty"`
}

// ComponentView is one catalog row as JSON, plus whether this binary
// was built with it.
type ComponentView struct {
	ID               string                 `json:"id"`
	Class            Class                  `json:"class"`
	Binary           Binary                 `json:"binary"`
	Title            string                 `json:"title"`
	About            string                 `json:"about"`
	Stability        Stability              `json:"stability"`
	Levels           LevelsView             `json:"levels"`
	BuildTag         string                 `json:"build_tag,omitempty"`
	Compiled         bool                   `json:"compiled"`
	Distros          map[Distro]Default     `json:"distros,omitempty"`
	Images           map[Image]ImageDefault `json:"images,omitempty"`
	Packages         []string               `json:"packages"`
	HostedIn         []string               `json:"hosted_in"`
	DirectModules    []string               `json:"direct_modules"`
	Requires         []string               `json:"requires"`
	RequiresCompiled []string               `json:"requires_compiled"`
	Bundles          []string               `json:"bundles"`
	Recommends       []string               `json:"recommends"`
	Conflicts        []string               `json:"conflicts"`
	Surface          SurfaceView            `json:"surface"`
	Tables           []string               `json:"tables"`
	Migrations       []MigrationView        `json:"migrations"`
	Routes           []string               `json:"routes"`
	Verbs            []string               `json:"verbs"`
	ConfigKeys       []string               `json:"config_keys"`
	Teardown         []TeardownView         `json:"teardown"`
	Ledger           []string               `json:"ledger"`
	RestartClass     string                 `json:"restart_class,omitempty"`
	Note             string                 `json:"note,omitempty"`
}

// ArtifactView is one external artifact row as JSON.
type ArtifactView struct {
	ID        string       `json:"id"`
	Kind      ArtifactKind `json:"kind"`
	Title     string       `json:"title"`
	About     string       `json:"about"`
	Platforms []string     `json:"platforms"`
	Source    string       `json:"source"`
	Signer    string       `json:"signer"`
	Protocol  string       `json:"protocol"`
	States    []string     `json:"states"`
	Requires  []string     `json:"requires"`
	Surface   SurfaceView  `json:"surface"`
	// The lifecycle fields (lane MP-ART, T-601); additive.
	NotInstalled string `json:"not_installed"`
	Removal      string `json:"removal"`
	Lockstep     string `json:"lockstep"`
	VersionFile  string `json:"version_file,omitempty"`
}

// Report is the whole catalog as one binary sees it.
type Report struct {
	CatalogVersion   int      `json:"catalog_version"`
	Binary           Binary   `json:"binary"`
	Distro           Distro   `json:"distro"`
	ComponentsDigest string   `json:"components_digest"`
	CompiledCount    int      `json:"compiled_count"`
	Omitted          []string `json:"omitted"`
	// DistroOut lists the components this build's distro excludes whose
	// code is still in the binary (inert and unselectable, L2), so no
	// surface reads a distro build as having compiled them out (L1).
	DistroOut []string `json:"distro_out"`
	// Custom describes a custom build (plan 5.3, lane MP-CUST): its custom
	// distro id, which the update rail selects artifacts by, and the
	// manifest it was built from. Absent in every other build; additive.
	Custom     *CustomView     `json:"custom,omitempty"`
	Components []ComponentView `json:"components"`
	Artifacts  []ArtifactView  `json:"artifacts"`
	// Note says plainly what this phase does and does not do.
	Note string `json:"note"`
}

// ReportNote is the honest one-liner every surface shows with the catalog.
// It names the two levels a node can see today (phase P1 runtime gate, P2
// compile-out) without claiming any component is compiled out: the
// report's own compiled and omitted lists say which ones are (T-670).
const ReportNote = "A component this build does not compile is compiled out (L1): its code is not in the binary. A compiled component you deactivate is installed but inert (L2): it starts nothing, its dashboard routes answer component_not_installed and its CLI verbs refuse. A distro build that excludes a component whose code this release still carries lists it under distro_out: inert and not selectable (L2), not compiled out. Choose in Settings > Components or with observer components."

// BuildReport assembles the report for the running binary b. Rows of other
// binaries are included only when all is true (they are never compiled
// into b).
func BuildReport(b Binary, all bool) Report {
	compiled := toSet(Compiled(b))
	ids := Compiled(b)
	rep := Report{
		CatalogVersion:   CatalogVersion,
		Binary:           b,
		ComponentsDigest: Digest(ids),
		CompiledCount:    len(ids),
		Omitted:          nonNil(Omitted()),
		DistroOut:        []string{},
		Note:             ReportNote,
	}
	if b == BinaryNode {
		rep.Distro = DistroOf(ids)
		rep.DistroOut = nonNil(DistroOut())
		rep.Custom = customView()
	}
	for _, c := range All() {
		if c.Binary != b && !all {
			continue
		}
		rep.Components = append(rep.Components, ViewOf(c, c.Binary == b && compiled[c.ID]))
	}
	for _, a := range Artifacts() {
		rep.Artifacts = append(rep.Artifacts, ArtifactView{
			ID: a.ID, Kind: a.Kind, Title: a.Title, About: a.About, Platforms: nonNil(a.Platforms),
			Source: a.Source, Signer: a.Signer, Protocol: a.Protocol, States: nonNil(a.States),
			Requires: nonNil(a.Requires), Surface: surfaceView(a.Surface),
			NotInstalled: a.NotInstalled, Removal: a.Removal, Lockstep: a.Lockstep, VersionFile: a.VersionFile,
		})
	}
	return rep
}

// ViewOf converts one row. Nil slices become empty so JSON consumers never
// see null.
func ViewOf(c Component, compiled bool) ComponentView {
	v := ComponentView{
		ID: c.ID, Class: c.Class, Binary: c.Binary, Title: c.Title, About: c.About, Stability: c.Stability,
		Levels:   LevelsView{Implemented: nonNilLevels(c.Levels.Implemented), Target: nonNilLevels(c.Levels.Target), Phase: c.Levels.Phase},
		BuildTag: c.BuildTag(), Compiled: compiled, Distros: c.Distros, Images: c.Images,
		Packages: nonNil(c.Packages), HostedIn: nonNil(c.HostedIn), DirectModules: sortedCopy(c.DirectModules),
		Requires: nonNil(c.Requires), RequiresCompiled: nonNil(c.RequiresCompiled), Bundles: nonNil(c.Bundles),
		Recommends: nonNil(c.Recommends), Conflicts: nonNil(c.Conflicts), Surface: surfaceView(c.Surface),
		Tables: nonNil(c.Tables), Routes: nonNil(c.Routes), Verbs: nonNil(c.Verbs), ConfigKeys: nonNil(c.ConfigKeys),
		Ledger: nonNil(c.Ledger), RestartClass: c.RestartClass, Note: c.Note,
		Migrations: []MigrationView{}, Teardown: []TeardownView{},
	}
	for _, m := range c.Migrations {
		v.Migrations = append(v.Migrations, MigrationView{File: m.File, Class: m.Class, Note: m.Note})
	}
	for _, t := range c.Teardown {
		v.Teardown = append(v.Teardown, TeardownView{Owner: t.Owner, What: t.What})
	}
	return v
}

func surfaceView(s Surface) SurfaceView {
	v := SurfaceView{
		Listeners: []ListenerView{}, Egress: []EgressView{},
		FilesWritten: nonNil(s.FilesWritten), VendorConfigs: nonNil(s.VendorConfigs),
		OSIntegration: nonNil(s.OSIntegration), Processes: nonNil(s.Processes), Credentials: nonNil(s.Credentials),
	}
	for _, l := range s.Listeners {
		v.Listeners = append(v.Listeners, ListenerView{Proto: l.Proto, Addr: l.Addr, Loopback: l.Loopback, Auth: l.Auth, ConfigKey: l.ConfigKey, Trigger: l.Trigger})
	}
	for _, e := range s.Egress {
		v.Egress = append(v.Egress, EgressView{Host: e.Host, Purpose: e.Purpose, Gate: e.Gate, Trigger: e.Trigger})
	}
	return v
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func nonNilLevels(xs []Level) []Level {
	if xs == nil {
		return []Level{}
	}
	return xs
}

func sortedCopy(xs []string) []string {
	out := append([]string{}, xs...)
	sort.Strings(out)
	return out
}
