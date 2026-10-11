package pluginshim

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed observer-shim.ts
var shimTemplate []byte

// profilePlaceholder is the one substitution point in the template.
const profilePlaceholder = "{{OBSERVER_SHIM_PROFILE}}"

// Marker is a string unique to the shim template (its header line, never
// substituted). LooksLikeObserverShim uses it to tell an observer-written
// shim from an unrelated file at the same path.
const Marker = "SuperBased Observer plugin shim"

// Plugin contracts the shim implements (the PROFILE "shape"). One adapter
// function per contract inside the shim; a host names the one it speaks.
const (
	// ShapeOpenCode: a module whose default export is {id, server}, server
	// returning a hooks object (OpenCode, Kilo CLI).
	ShapeOpenCode = "opencode"
	// ShapePi: a default-exported extension factory (pi).
	ShapePi = "pi"
	// ShapeOpenClaw: a plugin entry {id, name, description, register(api)}.
	ShapeOpenClaw = "openclaw"
	// ShapeCordis: a Cordis plugin {name, apply(ctx)} (DeepSeek Harness).
	ShapeCordis = "cordis"
	// ShapeCommandCode: a Mods SDK default-exported factory calling
	// cmd.hooks({...}) (commandcode).
	ShapeCommandCode = "commandcode"
)

// Default deadlines. The gate deadline is the shim's own wall clock for a
// gate event; observer is told to finish MarginMS earlier (`--max-runtime`)
// so its own fail-closed answer always beats the shim's kill.
const (
	defaultGateMS    = 10000
	defaultObserveMS = 5000
	defaultMarginMS  = 1500
)

// Host is one in-process plugin host: data only.
type Host struct {
	// Tool is the internal/integration registry tool id, which is also the
	// `observer hook <tool>` argument.
	Tool string
	// Shape is the plugin contract (Shape* constants).
	Shape string
	// Ask is true when the shim maps an ask decision onto the host's own
	// approval prompt; otherwise ask is answered as a deny with a reason.
	Ask bool
	// GateMS / ObserveMS / MarginMS override the defaults (0 = default).
	GateMS, ObserveMS, MarginMS int
	// FileName is the leaf file the writer places (default
	// "superbased-observer.ts").
	FileName string
	// XDGRel, when set, places the file under $XDG_CONFIG_HOME/<XDGRel>
	// (falling back to <home>/.config/<XDGRel>).
	XDGRel string
	// EnvDir + EnvSub, when EnvDir names a set environment variable, place
	// the file under $EnvDir/<EnvSub>.
	EnvDir, EnvSub string
	// HomeRel places the file under <home>/<HomeRel> when neither of the
	// above applies.
	HomeRel string
	// Note explains a host with no grounded install location (Dir returns
	// an error carrying it).
	Note string
}

// hosts is the host table. A host with no location fields has no writer:
// its install path is not grounded, so Observer renders the shim but never
// guesses where the host loads it from.
var hosts = map[string]Host{
	"opencode": {Tool: "opencode", Shape: ShapeOpenCode, XDGRel: "opencode/plugins"},
	// Kilo CLI (the OpenCode fork) loads ~/.config/kilo/plugin/*.ts
	// (kilo.ai/docs/automate/extending/plugins, fetched 2026-10-03).
	"kilo-code-cli": {Tool: "kilo-code-cli", Shape: ShapeOpenCode, XDGRel: "kilo/plugin"},
	"pi": {
		Tool: "pi", Shape: ShapePi,
		EnvDir: "PI_CODING_AGENT_DIR", EnvSub: "extensions", HomeRel: ".pi/agent/extensions",
	},
	// OpenClaw fails a before_tool_call handler CLOSED at its own 15 s
	// default; the shim answers inside 12 s.
	"openclaw": {
		Tool: "openclaw", Shape: ShapeOpenClaw, Ask: true, GateMS: 12000,
		Note: "OpenClaw loads a plugin only after `openclaw plugins install --link <dir>` records and enables it; Observer does not edit OpenClaw's config",
	},
	"deepseek": {
		Tool: "deepseek", Shape: ShapeCordis,
		Note: "DeepSeek Harness loads Cordis plugins through its profile layers (cordis.patch.yml); no user plugin directory is documented",
	},
	// commandcode's mod keeps its historical file name so an existing
	// install is refreshed in place, and its 5 s budget.
	"command-code": {
		Tool: "command-code", Shape: ShapeCommandCode, GateMS: 5000, MarginMS: 1000,
		FileName: "observer-guard.ts", HomeRel: ".commandcode/mods",
	},
}

// HostFor returns the host row for an `observer hook` tool id.
func HostFor(tool string) (Host, bool) {
	h, ok := hosts[tool]
	return h, ok
}

// Tools returns every host's tool id, sorted.
func Tools() []string {
	out := make([]string, 0, len(hosts))
	for t := range hosts {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// File returns the leaf file name the writer places for h.
func (h Host) File() string {
	if h.FileName != "" {
		return h.FileName
	}
	return "superbased-observer.ts"
}

// Writable reports whether h has a grounded install location.
func (h Host) Writable() bool { return h.XDGRel != "" || h.HomeRel != "" || h.EnvDir != "" }

// Dir resolves the directory the shim is placed in. getenv is injected (the
// writer passes os.Getenv, tests a map). An error names why a host has no
// location.
func (h Host) Dir(home string, getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	switch {
	case h.EnvDir != "" && strings.TrimSpace(getenv(h.EnvDir)) != "":
		return filepath.Join(strings.TrimSpace(getenv(h.EnvDir)), h.EnvSub), nil
	case h.XDGRel != "":
		if x := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); x != "" {
			return filepath.Join(x, filepath.FromSlash(h.XDGRel)), nil
		}
		if home == "" {
			return "", errors.New("pluginshim: no home directory")
		}
		return filepath.Join(home, ".config", filepath.FromSlash(h.XDGRel)), nil
	case h.HomeRel != "":
		if home == "" {
			return "", errors.New("pluginshim: no home directory")
		}
		return filepath.Join(home, filepath.FromSlash(h.HomeRel)), nil
	}
	if h.Note != "" {
		return "", fmt.Errorf("pluginshim: %s has %w: %s", h.Tool, ErrNoInstallLocation, h.Note)
	}
	return "", fmt.Errorf("pluginshim: %s has %w", h.Tool, ErrNoInstallLocation)
}

// ErrNoInstallLocation is a host row with no documented plugin directory:
// Observer cannot install a shim there, so it never wrote one, and removing
// it is a no-op rather than an error (T-732: a teardown on a clean machine
// reported deepseek and openclaw as errors).
var ErrNoInstallLocation = errors.New("no install location")

// RenderOptions are the install-time values baked into a rendered shim.
type RenderOptions struct {
	// Events maps each subscribed native event to the abstract capability
	// it provides (internal/integration HookCap values). Only these events
	// are hooked; an empty map renders an inert shim.
	Events map[string]string
	// Bin is the observer binary (default "observer", PATH lookup; the
	// OBSERVER_BIN environment variable still wins at run time).
	Bin string
	// Config is the observer --config path ("" = default discovery).
	Config string
	// WSLBin / WSLDistro select the cross-OS bridge: on win32 the shim runs
	// `wsl.exe -d <distro> -- <WSLBin> hook ...`.
	WSLBin, WSLDistro string
	// PosturePath / SpoolPath are guard-posture.json and
	// guard-hookfail.jsonl beside the global config; PostureSchema is the
	// posture schema the shim accepts.
	PosturePath, SpoolPath string
	PostureSchema          int
	// ConfigFile is the global config.toml the posture was derived from;
	// a posture written before it last changed is stale (T-050). "" skips
	// that check.
	ConfigFile string
	// PostureMaxAgeMS / PostureSkewMS are the posture freshness window and
	// the tolerated future skew (guardposture.MaxAge / MaxFutureSkew, passed
	// by the writer; 0 = the package defaults below, the same values).
	PostureMaxAgeMS, PostureSkewMS int
}

// Default posture freshness, in milliseconds: guardposture.MaxAge (3 min)
// and guardposture.MaxFutureSkew (1 min). The writer passes the live values;
// these only cover a caller that does not.
const (
	defaultPostureMaxAgeMS = 3 * 60 * 1000
	defaultPostureSkewMS   = 60 * 1000
)

// profile is the PROFILE literal (stable JSON key order: struct order).
type profile struct {
	Host          string            `json:"host"`
	Tool          string            `json:"tool"`
	Shape         string            `json:"shape"`
	Events        map[string]string `json:"events"`
	Ask           bool              `json:"ask"`
	GateMS        int               `json:"gate_ms"`
	ObserveMS     int               `json:"observe_ms"`
	MarginMS      int               `json:"margin_ms"`
	Bin           string            `json:"bin"`
	Config        string            `json:"config"`
	WSLBin        string            `json:"wsl_bin"`
	WSLDistro     string            `json:"wsl_distro"`
	Posture       string            `json:"posture"`
	PostureSchema int               `json:"posture_schema"`
	Spool         string            `json:"spool"`
	ConfigFile    string            `json:"config_file"`
	PostureMaxAge int               `json:"posture_max_age_ms"`
	PostureSkew   int               `json:"posture_skew_ms"`
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// Render returns the shim for h with o baked in.
func Render(h Host, o RenderOptions) ([]byte, error) {
	if h.Tool == "" || h.Shape == "" {
		return nil, errors.New("pluginshim.Render: host has no tool or shape")
	}
	ev := map[string]string{}
	for k, v := range o.Events {
		ev[k] = v
	}
	bin := o.Bin
	if bin == "" {
		bin = "observer"
	}
	p := profile{
		Host: h.Tool, Tool: h.Tool, Shape: h.Shape, Events: ev, Ask: h.Ask,
		GateMS: orDefault(h.GateMS, defaultGateMS), ObserveMS: orDefault(h.ObserveMS, defaultObserveMS),
		MarginMS: orDefault(h.MarginMS, defaultMarginMS),
		Bin:      bin, Config: o.Config, WSLBin: o.WSLBin, WSLDistro: o.WSLDistro,
		Posture: o.PosturePath, PostureSchema: o.PostureSchema, Spool: o.SpoolPath,
		ConfigFile:    o.ConfigFile,
		PostureMaxAge: orDefault(o.PostureMaxAgeMS, defaultPostureMaxAgeMS),
		PostureSkew:   orDefault(o.PostureSkewMS, defaultPostureSkewMS),
	}
	js, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("pluginshim.Render: %w", err)
	}
	if !bytes.Contains(shimTemplate, []byte(profilePlaceholder)) {
		return nil, errors.New("pluginshim.Render: template has no profile placeholder")
	}
	return bytes.Replace(shimTemplate, []byte(profilePlaceholder), js, 1), nil
}

// Write renders the shim and writes it into dir (created 0755, file 0600),
// overwriting an earlier copy: re-running is the upgrade path.
func Write(dir string, h Host, o RenderOptions) (string, error) {
	data, err := Render(h, o)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("pluginshim.Write: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, h.File())
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("pluginshim.Write: %w", err)
	}
	return path, nil
}

// Remove deletes the shim from dir; an absent file is not an error.
func Remove(dir string, h Host) error {
	if err := os.Remove(filepath.Join(dir, h.File())); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pluginshim.Remove: %w", err)
	}
	return nil
}

// LooksLikeObserverShim reports whether data is an observer-written shim
// (current template, or the commandcode prompt bridge it replaced).
func LooksLikeObserverShim(data []byte) bool {
	return bytes.Contains(data, []byte(Marker)) || bytes.Contains(data, []byte(legacyCommandCodeMarker))
}

// legacyCommandCodeMarker is the header of the pre-shim commandcode mod
// (internal/hook/commandcodemod before Lane D), still recognized so an old
// install is refreshed instead of refused as a foreign file.
const legacyCommandCodeMarker = "SuperBased Observer prompt-submit guard bridge"
