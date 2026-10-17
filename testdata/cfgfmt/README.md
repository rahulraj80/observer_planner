# testdata/cfgfmt: real vendor config fixtures for the format engines

Golden fixtures for `internal/cfgfmt` (plan
`docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md`, phase
P0). Every file is a copy (or a contiguous excerpt) of a configuration
example the VENDOR publishes, fetched 2026-10-03. None was read from a
developer machine, and none holds a credential: the fixture builder refused
any file matching a key / token / private-key shape, and the vendor samples
only ever name environment variables (`OPENAI_API_KEY`), never values.

| File | Vendor / tool | Source | License | Changes from the source |
|---|---|---|---|---|
| `json/claude-code-settings.json` | Anthropic, Claude Code | code.claude.com/docs/en/settings.md (the `~/.claude/settings.json` example) | vendor docs, short example | none |
| `json/gemini-cli-settings.json` | Google, Gemini CLI | github.com/google-gemini/gemini-cli `docs/reference/configuration.md` (nested `settings.json` example) | Apache-2.0 | none |
| `jsonc/opencode.jsonc` | OpenCode | github.com/anomalyco/opencode `packages/web/src/content/docs/config.mdx` (first `opencode.jsonc` block: trailing commas) | MIT | none |
| `jsonc/opencode-tui.jsonc` | OpenCode | same file, second `opencode.jsonc` block (line comment inside an object) | MIT | none |
| `toml/codex-config-sample.toml` | OpenAI, Codex | developers.openai.com/codex/config-sample.md | vendor docs sample config | excerpt: the head of the sample through `[history]` (573 of 1187 lines); the Markdown escaping in `"AWS\_\*"` style keys removed so the TOML is valid |
| `toml/openinterpreter-config-wsl.toml` | Open Interpreter | this repo's `testdata/openinterpreter/config-wsl.toml` (live-grounded 2026-09-03) | repo fixture | the project path anonymized to `/home/dev/example-project` |
| `yaml/aider-sample.conf.yml` | Aider | github.com/Aider-AI/aider `aider/website/assets/sample.aider.conf.yml` | Apache-2.0 | none (every key is commented out: the "insert into a comments-only file" case) |
| `yaml/hermes-cli-config.yaml` | Nous Research, Hermes Agent | github.com/NousResearch/hermes-agent `cli-config.yaml.example` | MIT | excerpt: the first 401 lines (database, runtime, attachments, plugin and model sections) |
| `yaml/goose-config.yaml` | Block, goose | github.com/block/goose `documentation/docs/guides/config-files.md` (example `config.yaml`) | Apache-2.0 | none |
| `markdown/skill-creator-SKILL.md` | Anthropic skills | github.com/anthropics/skills `skills/skill-creator/SKILL.md` | Apache-2.0 | excerpt: front matter + first 36 body lines |
| `markdown/gemini-cli-GEMINI.md` | Google, Gemini CLI | github.com/google-gemini/gemini-cli `GEMINI.md` | Apache-2.0 | none (no front matter: the "create front matter" case) |
| `markdown/opencode-AGENTS.md` | OpenCode | github.com/anomalyco/opencode `AGENTS.md` | MIT | none (marked-block append / remove round trip) |
| `markdown/claude-code-subagent.md` | Anthropic, Claude Code | code.claude.com/docs/en/sub-agents.md (`.claude/agents/code-reviewer.md` example) | vendor docs, short example | none |
| `mdc/anti-overengineering.mdc` | Cursor rule | github.com/PatrickJS/awesome-cursorrules `rules/anti-overengineering.mdc` | CC0-1.0 | none (flow-sequence `globs`) |

`golden/*.golden` are the reviewed outputs of `TestGoldenEdits`: each is the
fixture after one single-key edit. Regenerate with
`go test ./internal/cfgfmt -run TestGoldenEdits -update` and review the
diff against the fixture: an edit must change only the edited entry.
`golden/codex-array-of-tables.golden` is the one intentional exception, the
disclosed re-serialization fallback for an edit inside an array of tables.
