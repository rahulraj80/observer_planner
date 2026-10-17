package agentconfig

// aider (Aider-AI/aider) doc-authored catalogue. Every CLI switch --foo-bar
// is also a YAML key foo-bar in .aider.conf.yml and an AIDER_FOO_BAR env
// var. Sources: the vendor sample (aider/website/assets/
// sample.aider.conf.yml) and docs/config/aider_conf.md on main, fetched
// 2026-10-04, plus audit section 5 "aider". aider is not installed on
// node 1: doc-only.
const (
	aiderRef      = "https://raw.githubusercontent.com/Aider-AI/aider/main/aider/website/assets/sample.aider.conf.yml"
	aiderConfRef  = "https://raw.githubusercontent.com/Aider-AI/aider/main/aider/website/docs/config/aider_conf.md"
	aiderModelRef = "https://raw.githubusercontent.com/Aider-AI/aider/main/aider/website/docs/config/adv-model-settings.md"
	aiderShape    = "aider/conf"
	aiderKeyNote  = "A plaintext credential in the YAML file; prefer the provider env var (OPENAI_API_KEY, ANTHROPIC_API_KEY, ...)."
)

// aiderRow fills the tool, shape, owner, verification and id of one row.
func aiderRow(r SettingRow) SettingRow {
	r.Tool = "aider"
	if r.FileID == "" {
		r.FileID = aiderShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = aiderRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

var aiderCatalog = toolCatalog{
	Tool: "aider",
	VerifiedAgainst: "Aider-AI/aider sample.aider.conf.yml and docs/config pages on main fetched 2026-10-04 (audit section 5 aider). Not installed on node 1: no live evidence. " +
		"Files load home -> git root -> cwd, last wins; a cwd copy that is not the git root cannot be addressed by a project path and is not catalogued.",
	Files: []FileRow{
		{
			ID: "aider/user-conf", Tool: "aider", Shape: aiderShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.aider.conf.yml"), Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(aiderConfRef),
			Note: "May hold plaintext API keys (openai-api-key, anthropic-api-key, api-key): those rows are secret. --config <file> loads only that one file. ~/.env is a separate dotenv file, not catalogued.",
		},
		{
			ID: "aider/project-conf", Tool: "aider", Shape: aiderShape, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.aider.conf.yml"), Format: FormatYAML, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(aiderConfRef),
			Note: "The git root copy; it overrides the home file.",
		},
		{
			ID: "aider/user-model-settings", Tool: "aider", Shape: "aider/model-settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.aider.model.settings.yml"), Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(aiderModelRef),
			Note: "A YAML list of per-model entries (name, edit_format, use_temperature, extra_params, ...): no fixed key paths, so no rows.",
		},
		{
			ID: "aider/project-model-settings", Tool: "aider", Shape: "aider/model-settings", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.aider.model.settings.yml"), Format: FormatYAML, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(aiderModelRef),
		},
		{
			ID: "aider/user-model-metadata", Tool: "aider", Shape: "aider/model-metadata", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.aider.model.metadata.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(aiderModelRef),
			Note: "litellm model metadata (max_input_tokens, costs) keyed by model name: no fixed key paths, so no rows.",
		},
		{
			ID: "aider/project-model-metadata", Tool: "aider", Shape: "aider/model-metadata", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.aider.model.metadata.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(aiderModelRef),
		},
	},
	Extra: []SettingRow{
		// --- models and API
		aiderRow(SettingRow{Path: KeyPath{"model"}, Type: TypeString, Desc: "Main chat model (a litellm model name)."}),
		aiderRow(SettingRow{Path: KeyPath{"weak-model"}, Type: TypeString, Desc: "Model for commit messages and history summaries (default depends on model)."}),
		aiderRow(SettingRow{Path: KeyPath{"editor-model"}, Type: TypeString, Desc: "Model for architect editor tasks."}),
		aiderRow(SettingRow{Path: KeyPath{"editor-edit-format"}, Type: TypeString, Desc: "Edit format for the editor model."}),
		aiderRow(SettingRow{Path: KeyPath{"openai-api-key"}, Type: TypeString, Secret: true, Desc: "OpenAI API key.", Note: aiderKeyNote}),
		aiderRow(SettingRow{Path: KeyPath{"anthropic-api-key"}, Type: TypeString, Secret: true, Desc: "Anthropic API key.", Note: aiderKeyNote}),
		aiderRow(SettingRow{
			Path: KeyPath{"api-key"}, Type: TypeStringList, Secret: true, Desc: "provider=<key> entries setting PROVIDER_API_KEY.", Note: aiderKeyNote,
		}),
		aiderRow(SettingRow{
			Path: KeyPath{"openai-api-base"}, Type: TypeString, Desc: "OpenAI-compatible API base URL.", Danger: DangerConfirm,
			Note: "Redirects traffic; Observer's aider route is the launcher's OPENAI_API_BASE, and a different value here takes aider off the proxy.",
		}),
		aiderRow(SettingRow{
			Path: KeyPath{"set-env"}, Type: TypeStringList, Desc: "VAR=value environment variables (API settings such as base URLs).",
			Danger: DangerConfirm, Note: "Values may be credentials or base URLs that bypass the proxy (OPENAI_API_BASE, ANTHROPIC_BASE_URL).",
		}),
		aiderRow(SettingRow{Path: KeyPath{"model-settings-file"}, Type: TypeString, Default: "\".aider.model.settings.yml\"", Desc: "Model settings file for unknown models."}),
		aiderRow(SettingRow{Path: KeyPath{"model-metadata-file"}, Type: TypeString, Default: "\".aider.model.metadata.json\"", Desc: "Context window and cost file for unknown models."}),
		aiderRow(SettingRow{Path: KeyPath{"alias"}, Type: TypeStringList, Desc: "Model aliases (alias:model)."}),
		aiderRow(SettingRow{Path: KeyPath{"reasoning-effort"}, Type: TypeString, Desc: "reasoning_effort API parameter (default not set)."}),
		aiderRow(SettingRow{Path: KeyPath{"thinking-tokens"}, Type: TypeString, Desc: "Thinking token budget (e.g. 8k; 0 disables)."}),
		aiderRow(SettingRow{
			Path: KeyPath{"verify-ssl"}, Type: TypeBool, Default: "true", Desc: "Verify SSL certificates for model connections.",
			Danger: DangerConfirm, Note: "false disables certificate checks.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"timeout"}, Type: TypeFloat, Desc: "API call timeout in seconds."}),
		aiderRow(SettingRow{Path: KeyPath{"edit-format"}, Type: TypeString, Desc: "Edit format the LLM uses (default depends on model)."}),
		aiderRow(SettingRow{Path: KeyPath{"architect"}, Type: TypeBool, Default: "false", Desc: "Use the architect edit format."}),
		aiderRow(SettingRow{
			Path: KeyPath{"auto-accept-architect"}, Type: TypeBool, Default: "true", Desc: "Accept architect changes automatically.",
			Danger: DangerConfirm, Note: "true (the default) applies architect edits without review.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"show-model-warnings"}, Type: TypeBool, Default: "true", Desc: "Only work with models that have metadata."}),
		aiderRow(SettingRow{Path: KeyPath{"check-model-accepts-settings"}, Type: TypeBool, Default: "true", Desc: "Check a model accepts reasoning_effort / thinking_tokens."}),
		aiderRow(SettingRow{Path: KeyPath{"max-chat-history-tokens"}, Type: TypeInt, Desc: "Soft chat-history limit before summarization."}),
		aiderRow(SettingRow{Path: KeyPath{"cache-prompts"}, Type: TypeBool, Default: "false", Desc: "Enable prompt caching."}),
		aiderRow(SettingRow{Path: KeyPath{"cache-keepalive-pings"}, Type: TypeInt, Default: "0", Desc: "Pings at 5 minute intervals to keep the prompt cache warm."}),
		aiderRow(SettingRow{Path: KeyPath{"map-tokens"}, Type: TypeInt, Desc: "Repo map token budget (0 disables)."}),
		aiderRow(SettingRow{
			Path: KeyPath{"map-refresh"}, Type: TypeEnum, Enum: []string{"auto", "always", "files", "manual"}, Default: "\"auto\"", Desc: "Repo map refresh policy.",
		}),
		// --- history
		aiderRow(SettingRow{Path: KeyPath{"input-history-file"}, Type: TypeString, Default: "\".aider.input.history\"", Desc: "Chat input history file."}),
		aiderRow(SettingRow{
			Path: KeyPath{"chat-history-file"}, Type: TypeString, Default: "\".aider.chat.history.md\"", Desc: "Chat history transcript file.",
			Danger: DangerRefuseWhileIntegrated, Note: "Observer's aider adapter captures from .aider.chat.history.md; another path moves the transcript out of capture.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"restore-chat-history"}, Type: TypeBool, Default: "false", Desc: "Restore previous chat messages."}),
		aiderRow(SettingRow{
			Path: KeyPath{"llm-history-file"}, Type: TypeString, Desc: "Log the raw LLM conversation to this file.", Note: "The log holds full prompts and responses.",
		}),
		// --- output
		aiderRow(SettingRow{Path: KeyPath{"dark-mode"}, Type: TypeBool, Default: "false", Desc: "Colors for a dark terminal."}),
		aiderRow(SettingRow{Path: KeyPath{"light-mode"}, Type: TypeBool, Default: "false", Desc: "Colors for a light terminal."}),
		aiderRow(SettingRow{Path: KeyPath{"pretty"}, Type: TypeBool, Default: "true", Desc: "Pretty, colorized output."}),
		aiderRow(SettingRow{Path: KeyPath{"stream"}, Type: TypeBool, Default: "true", Desc: "Stream responses."}),
		aiderRow(SettingRow{Path: KeyPath{"code-theme"}, Type: TypeString, Default: "\"default\"", Desc: "Markdown code theme (a Pygments style)."}),
		aiderRow(SettingRow{Path: KeyPath{"show-diffs"}, Type: TypeBool, Default: "false", Desc: "Show diffs when committing."}),
		// --- git
		aiderRow(SettingRow{
			Path: KeyPath{"git"}, Type: TypeBool, Default: "true", Desc: "Look for a git repo.", Danger: DangerConfirm, Note: "false edits without git history.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"gitignore"}, Type: TypeBool, Default: "true", Desc: "Add .aider* to .gitignore."}),
		aiderRow(SettingRow{Path: KeyPath{"aiderignore"}, Type: TypeString, Default: "\".aiderignore\"", Desc: "aider ignore file."}),
		aiderRow(SettingRow{Path: KeyPath{"subtree-only"}, Type: TypeBool, Default: "false", Desc: "Only consider the current git subtree."}),
		aiderRow(SettingRow{
			Path: KeyPath{"auto-commits"}, Type: TypeBool, Default: "true", Desc: "Commit LLM changes automatically.",
			Danger: DangerConfirm, Note: "Writes git history without review.",
		}),
		aiderRow(SettingRow{
			Path: KeyPath{"dirty-commits"}, Type: TypeBool, Default: "true", Desc: "Commit when the repo is dirty.", Danger: DangerConfirm, Note: "Writes git history without review.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"attribute-co-authored-by"}, Type: TypeBool, Default: "true", Desc: "Co-authored-by trailer on aider commits."}),
		aiderRow(SettingRow{Path: KeyPath{"git-commit-verify"}, Type: TypeBool, Default: "false", Desc: "Run git pre-commit hooks (false passes --no-verify)."}),
		aiderRow(SettingRow{Path: KeyPath{"commit-prompt"}, Type: TypeString, Desc: "Custom prompt for commit messages."}),
		aiderRow(SettingRow{Path: KeyPath{"watch-files"}, Type: TypeBool, Default: "false", Desc: "Watch files for AI coding comments."}),
		// --- lint / test
		aiderRow(SettingRow{
			Path: KeyPath{"lint-cmd"}, Type: TypeStringList, Desc: "Per-language lint commands.", Danger: DangerConfirm, Note: "Runs arbitrary commands after edits.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"auto-lint"}, Type: TypeBool, Default: "true", Desc: "Lint automatically after changes."}),
		aiderRow(SettingRow{
			Path: KeyPath{"test-cmd"}, Type: TypeString, Desc: "Command that runs the tests.", Danger: DangerConfirm, Note: "Runs an arbitrary command.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"auto-test"}, Type: TypeBool, Default: "false", Desc: "Test automatically after changes."}),
		// --- analytics / upgrade
		aiderRow(SettingRow{Path: KeyPath{"analytics"}, Type: TypeBool, Desc: "Analytics for the current session (default random opt-in)."}),
		aiderRow(SettingRow{Path: KeyPath{"analytics-log"}, Type: TypeString, Desc: "File to log analytics events."}),
		aiderRow(SettingRow{Path: KeyPath{"analytics-disable"}, Type: TypeBool, Default: "false", Desc: "Permanently disable analytics."}),
		aiderRow(SettingRow{Path: KeyPath{"analytics-posthog-host"}, Type: TypeString, Desc: "Custom PostHog host for analytics."}),
		aiderRow(SettingRow{
			Path: KeyPath{"analytics-posthog-project-api-key"}, Type: TypeString, Secret: true, Desc: "Custom PostHog project key.", Note: "A credential.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"check-update"}, Type: TypeBool, Default: "true", Desc: "Check for new versions on launch (does not install)."}),
		aiderRow(SettingRow{Path: KeyPath{"show-release-notes"}, Type: TypeBool, Desc: "Show release notes on the first run of a new version."}),
		// --- other
		aiderRow(SettingRow{Path: KeyPath{"file"}, Type: TypeStringList, Desc: "Files to edit."}),
		aiderRow(SettingRow{Path: KeyPath{"read"}, Type: TypeStringList, Desc: "Read-only files (CONVENTIONS.md by convention); prompt-cached."}),
		aiderRow(SettingRow{Path: KeyPath{"vim"}, Type: TypeBool, Default: "false", Desc: "VI editing mode."}),
		aiderRow(SettingRow{Path: KeyPath{"chat-language"}, Type: TypeString, Desc: "Chat language (default system)."}),
		aiderRow(SettingRow{Path: KeyPath{"commit-language"}, Type: TypeString, Desc: "Commit message language."}),
		aiderRow(SettingRow{
			Path: KeyPath{"yes-always"}, Type: TypeBool, Default: "false", Desc: "Answer yes to every confirmation.",
			Danger: DangerConfirm, Note: "true auto-confirms everything, including shell commands.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"encoding"}, Type: TypeString, Default: "\"utf-8\"", Desc: "Input and output encoding."}),
		aiderRow(SettingRow{Path: KeyPath{"line-endings"}, Type: TypeString, Default: "\"platform\"", Desc: "Line endings when writing files."}),
		aiderRow(SettingRow{Path: KeyPath{"env-file"}, Type: TypeString, Default: "\".env\"", Desc: ".env file to load."}),
		aiderRow(SettingRow{
			Path: KeyPath{"suggest-shell-commands"}, Type: TypeBool, Default: "true", Desc: "Suggest shell commands.",
			Danger: DangerConfirm, Note: "With yes-always the suggestions run unprompted.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"notifications"}, Type: TypeBool, Default: "false", Desc: "Bell when a response is ready."}),
		aiderRow(SettingRow{
			Path: KeyPath{"notifications-command"}, Type: TypeString, Desc: "Command run for notifications.", Danger: DangerConfirm, Note: "Runs an arbitrary command.",
		}),
		aiderRow(SettingRow{Path: KeyPath{"detect-urls"}, Type: TypeBool, Default: "true", Desc: "Offer to add URLs found in chat."}),
		aiderRow(SettingRow{Path: KeyPath{"editor"}, Type: TypeString, Desc: "Editor for /editor."}),
		aiderRow(SettingRow{Path: KeyPath{"disable-playwright"}, Type: TypeBool, Default: "false", Desc: "Never install Playwright for web scraping."}),
	},
	Map: []MapRow{
		{
			Key: KeyModel, Tool: "aider", Settings: []string{"aider/model"}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: aiderRef, Note: "litellm names take provider/model.",
		},
		{
			Key: KeySmallModel, Tool: "aider", Settings: []string{"aider/weak-model"}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: aiderRef,
		},
		{
			Key: KeyReasoningEffort, Tool: "aider", Settings: []string{"aider/reasoning-effort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}}}, Source: aiderRef,
			Note: "Passed through as reasoning_effort; only low / medium / high are offered, and the model must accept the setting.",
		},
		{
			Key: KeyApprovalMode, Tool: "aider", Settings: []string{"aider/yes-always"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"auto_edits", []any{false}}, {"full_auto", []any{true}}}}, Source: aiderRef,
			Note: "aider applies edits itself and asks before commands; plan, ask and ask_edits are refused.",
		},
		{Key: KeyTelemetryOff, Tool: "aider", Settings: []string{"aider/analytics-disable"}, Transform: TransformIdentity, Source: aiderRef},
		{
			Key: KeyAutoUpdate, Tool: "aider", Settings: []string{"aider/check-update"}, Transform: TransformIdentity, Source: aiderRef,
			Note: "Checks for updates; it does not install them.",
		},
		{
			Key: KeyTheme, Tool: "aider", Settings: []string{"aider/dark-mode", "aider/light-mode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"light", []any{false, true}},
				{"dark", []any{true, false}},
				{"light", []any{nil, true}},
				{"dark", []any{true, nil}},
			}}, Source: aiderRef, Note: "Fans out to dark-mode + light-mode; a file setting only one of them reads back too.",
		},
		{Key: KeyInstructionsFiles, Tool: "aider", Settings: []string{"aider/read"}, Transform: TransformIdentity, Source: aiderRef},
		{Key: KeyBaseURL, Tool: "aider", Settings: []string{"aider/openai-api-base"}, Transform: TransformIdentity, Source: aiderRef},
	},
}
