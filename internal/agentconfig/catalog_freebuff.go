package agentconfig

// Freebuff (Codebuff engine) catalogue, doc-authored (phase P2). The vendor
// documents no user settings file; node 1 has a 4-key one (audit section 6
// finding 5), so those rows are live. The project codebuff.json keys come
// from a search snippet of a config page that returned 404 (audit section 5
// "freebuff", re-checked 2026-10-04).
const (
	freebuffConfigRef = "https://www.codebuff.com/docs/advanced/config"
	freebuffLiveSrc   = "node 1 live probe 2026-10-03 (audit section 6 finding 5, freebuff)"
	freebuffAdapter   = "docs/freebuff-adapter.md"
)

func freebuffLiveRow(key string, typ ValueType, owner Owner, desc, note string) SettingRow {
	return SettingRow{
		ID: "freebuff/" + key, Tool: "freebuff", FileID: "freebuff/user-settings", Path: KeyPath{key}, Type: typ,
		Desc: desc, Source: freebuffLiveSrc, Verified: VerifiedLive, Owner: owner,
		Note: "Seen on node 1; not in the vendor docs. " + note,
	}
}

var freebuffCatalog = toolCatalog{
	Tool: "freebuff",
	VerifiedAgainst: "node 1 live key names 2026-10-03 for ~/.config/manicode/settings.json (audit section 6 finding 5); " +
		"codebuff.json keys from a search snippet of www.codebuff.com/docs/advanced/config (404 on fetch 2026-10-03 and 2026-10-04)",
	Files: []FileRow{
		{
			ID: "freebuff/user-settings", Tool: "freebuff", Shape: "freebuff/user-settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.config/manicode/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.config/manicode/settings.json carries adsEnabled, freebuffModelDefaultMigration, hasSubmittedFirstPrompt, mode"),
			Note:      "Undocumented; the CLI keeps ~/.config/manicode/ on every OS, Windows included. Mixes a user preference with tool state.",
		},
		{
			ID: "freebuff/project-config", Tool: "freebuff", Shape: "freebuff/project-config", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/codebuff.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(freebuffConfigRef),
			Note:      "Codebuff's project file (Freebuff is built on the same engine; Freebuff-specific divergence UNVERIFIED). The config page returned 404, so only the two snippet-grounded keys have rows.",
		},
		{
			ID: "freebuff/credentials", Tool: "freebuff", Shape: "freebuff/credentials", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.config/manicode/credentials.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.config/manicode/credentials.json present (not opened)"),
			Note:      "Login credentials: never read, never written.",
		},
		{
			ID: "freebuff/device-key", Tool: "freebuff", Shape: "freebuff/device-key", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.config/manicode/device-key.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.config/manicode/device-key.json present (not opened)"),
			Note:      "Device key: never read, never written.",
		},
		{
			ID: "freebuff/desktop-state", Tool: "freebuff", Shape: "freebuff/desktop-state", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.config/freebuff-desktop/state.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: freebuffAdapter, Date: auditDate},
			Note:      "Freebuff Desktop's state, holding an auth token: never read, never written.",
		},
	},
	Extra: []SettingRow{
		freebuffLiveRow("adsEnabled", TypeBool, OwnerUser, "Whether ads are shown.", "The audit records the name only; the type is inferred from it."),
		freebuffLiveRow("mode", TypeString, OwnerUser, "Session mode.", "A string; its values and meaning are undocumented, so it has no approval-mode mapping."),
		freebuffLiveRow("freebuffModelDefaultMigration", TypeString, OwnerToolState, "Marker of a model-default migration.", "The audit records the name only; maintained by the tool."),
		freebuffLiveRow("hasSubmittedFirstPrompt", TypeBool, OwnerToolState, "Whether the first prompt was submitted.", "The audit records the name only; maintained by the tool."),
		{
			ID: "freebuff/startupProcesses", Tool: "freebuff", FileID: "freebuff/project-config", Path: KeyPath{"startupProcesses"}, Type: TypeObject,
			Desc: "Background processes started with the session.", Source: freebuffConfigRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Danger: DangerConfirm,
			Note:   "Runs arbitrary shell commands automatically on start; a repository can ship them. Entry fields UNVERIFIED (config page 404).",
		},
		{
			ID: "freebuff/fileChangeHooks", Tool: "freebuff", FileID: "freebuff/project-config", Path: KeyPath{"fileChangeHooks"}, Type: TypeObject,
			Desc: "Commands run after the agent changes files.", Source: freebuffConfigRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Danger: DangerConfirm,
			Note:   "Runs arbitrary shell commands after edits (post-change, not a gate); a repository can ship them. Entry fields UNVERIFIED (config page 404).",
		},
	},
}
