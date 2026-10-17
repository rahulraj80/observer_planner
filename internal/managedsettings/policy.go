package managedsettings

import (
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Lever A of the org AI tool policy (adapter config plan section 6.2, phase
// P8; D-100, T-119 / T-433 / T-434): the composed org policy rendered into
// each vendor's OWN admin-managed settings layer, for IT to deploy through
// device management (MDM profiles, Group Policy / Intune registry values,
// root-owned files). Lever B (the node writing the user's own files, with
// drift detection) is tamper-EVIDENT; a value in a vendor-enforced,
// admin-owned layer is tamper-PROOF where the vendor makes it one. FromPolicy
// renders from the SAME composed body a managed node receives, so the two
// levers never disagree.
//
// Everything here is table data plus one placement walk: the managed layers
// (layers.go), the per-setting key rename for layers whose keys are not the
// user file's (Copilot's VS Code policy names) and the values a setting
// derives into an enforcement file (Codex requirements). No branch names a
// tool.

// Strength is how firmly a managed layer holds a value.
type Strength string

// The closed Strength vocabulary, strongest first.
const (
	// StrengthTamperProof: the vendor enforces the value over every user
	// and project file, and the layer is writable only by an administrator.
	StrengthTamperProof Strength = "tamper_proof"
	// StrengthLaunchReset: an admin-owned value the tool re-applies on every
	// launch; a developer can change it inside one session, not keep it.
	StrengthLaunchReset Strength = "reapplied_each_launch"
	// StrengthBypassable: the vendor enforces the value over the user's
	// files, but the user can step around it (an environment variable the
	// user sets relocates the managed file, or outranks it).
	StrengthBypassable Strength = "user_bypassable"
	// StrengthRestrictOnly: the layer can only deny or ask (never grant or
	// set a value).
	StrengthRestrictOnly Strength = "restrict_only"
	// StrengthDefaultOnly: an admin-owned default the developer's own files
	// override (the right layer for a policy DEFAULT).
	StrengthDefaultOnly Strength = "default_only"
	// StrengthTamperEvident: no admin-owned layer on this OS (none exists,
	// or the vendor's "managed" path is user-writable): Observer's lever B
	// writes, detects and reports a hand edit, but cannot prevent it.
	StrengthTamperEvident Strength = "tamper_evident"
)

// strengthRank orders Strength strongest first.
var strengthRank = map[Strength]int{
	StrengthTamperProof: 0, StrengthLaunchReset: 1, StrengthBypassable: 2, StrengthRestrictOnly: 3,
	StrengthDefaultOnly: 4, StrengthTamperEvident: 5,
}

// Stronger reports whether a holds a value more firmly than b.
func Stronger(a, b Strength) bool { return strengthRank[a] < strengthRank[b] }

// Channel names how an artifact reaches a machine.
type Channel string

// The closed Channel vocabulary.
const (
	ChannelFile     Channel = "file"
	ChannelProfile  Channel = "macos_profile"
	ChannelRegistry Channel = "windows_registry"
)

// Encoding is the byte encoding of an artifact's Content.
type Encoding string

// The closed Encoding vocabulary.
const (
	EncodingUTF8    Encoding = "utf-8"
	EncodingUTF16LE Encoding = "utf-16le-bom"
)

// Skip reasons (closed vocabulary; the web page labels them).
const (
	ReasonProjectScope    = "project_scope"
	ReasonNoManagedLayer  = "no_managed_layer"
	ReasonNotInLayer      = "not_in_managed_layer"
	ReasonNoDefaultLayer  = "no_default_layer"
	ReasonUnknownSetting  = "unknown_setting"
	ReasonUndecodable     = "value_not_decodable"
	ReasonValueNotWritten = "value_not_writable"
	ReasonValueRefused    = "value_refused_by_layer"
)

// InstallStep is how to put one artifact in place on one OS.
type InstallStep struct {
	OS string `json:"os"`
	// Path is the destination (empty for a profile or a registry import).
	Path  string   `json:"path,omitempty"`
	Steps []string `json:"steps"`
}

// Artifact is one deploy-ready file.
type Artifact struct {
	// Name is the path inside the bundle ("claude-code/managed-settings.json").
	Name string `json:"name"`
	// FileID is the catalogue file row the artifact renders.
	FileID   string   `json:"file_id"`
	Channel  Channel  `json:"channel"`
	OS       []string `json:"os"`
	Format   string   `json:"format"`
	Encoding Encoding `json:"encoding"`
	Content  string   `json:"content"`
	// Strength per OS this artifact gives ("linux" -> tamper_proof).
	Strength map[string]Strength `json:"strength"`
	Install  []InstallStep       `json:"install"`
	Note     string              `json:"note,omitempty"`
}

// Placed is one policy value carried by a tool's artifacts.
type Placed struct {
	Setting string `json:"setting"`
	Mode    string `json:"mode"`
	// FileID is the catalogue file row it landed in; Key the key there.
	FileID   string   `json:"file_id"`
	Key      string   `json:"key"`
	Strength Strength `json:"strength"`
	// Derived names the rule when the value is a derived constraint (Codex
	// requirements from an enforced approval policy).
	Derived string `json:"derived,omitempty"`
}

// Skip is one policy value no managed layer carries, with the reason. Lever
// B still delivers it on managed nodes.
type Skip struct {
	Setting string `json:"setting"`
	Mode    string `json:"mode"`
	Project string `json:"project,omitempty"`
	Reason  string `json:"reason"`
}

// ToolBundle is one tool's artifacts.
type ToolBundle struct {
	Tool string `json:"tool"`
	// Strength is the firmest hold any placed value gets on any OS.
	Strength  Strength   `json:"strength"`
	Artifacts []Artifact `json:"artifacts"`
	Placed    []Placed   `json:"placed"`
	Skipped   []Skip     `json:"skipped"`
	Sources   []string   `json:"sources"`
	// Verified is "doc" when every layer this tool uses is grounded in the
	// vendor's documentation, "unverified" otherwise.
	Verified string `json:"verified"`
	Readme   string `json:"readme"`
}

// ToolLock is the honest lock posture of one catalogued tool, whether or
// not the current policy sets anything for it.
type ToolLock struct {
	Tool string `json:"tool"`
	// Best is the firmest hold the tool's managed layers can give (per OS
	// in PerOS); tamper_evident when it has none.
	Best  Strength            `json:"best"`
	PerOS map[string]Strength `json:"per_os"`
	Note  string              `json:"note"`
	// Layers are the catalogue file rows Observer renders for it.
	Layers   []string `json:"layers,omitempty"`
	Verified string   `json:"verified"`
}

// Bundle is the whole generated output for one audience.
type Bundle struct {
	Audience      string       `json:"audience"`
	PolicyVersion int64        `json:"policy_version"`
	Policies      []string     `json:"policies"`
	Tools         []ToolBundle `json:"tools"`
	// Unplaced lists values no tool's managed layer carries.
	Unplaced []Skip `json:"unplaced"`
	// Locks is the per-tool honesty table (every catalogued tool).
	Locks  []ToolLock `json:"locks"`
	Readme string     `json:"readme"`
}

// PolicyInput is what FromPolicy renders.
type PolicyInput struct {
	// Body is the composed policy (agentconfigorg.Compose for the audience).
	Body orgcontract.AgentConfigPolicyBody
	// Audience labels the bundle ("the whole organization", "team Apps").
	Audience string
	Version  int64
	// Identifier is the reverse-DNS stem of generated profile ids
	// ("com.example.observer"); a default is used when empty.
	Identifier string
}

// DefaultIdentifier is the profile id stem when the input names none.
const DefaultIdentifier = "app.superbased.observer.policy"
