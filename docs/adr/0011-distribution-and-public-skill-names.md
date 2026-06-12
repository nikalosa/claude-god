# Distribution: marketplace + npx skills, plugin-bootstrapped binary; public names config-bench / quizgen / config-refactor

**Status:** accepted (supersedes the npm-centric plan in the old DISTRIBUTION.md; amends the name sweep in [ADR-0010](0010-rename-to-claude-benchmark.md))

How the tool ships. The skills distribute through **two channels at once**: the **Claude Code plugin marketplace** (`claude-god`, three *one-skill* plugins) for the turnkey Claude path, and **Vercel's npx `skills` CLI** for per-skill, agent-agnostic install. The Go binary ships as **prebuilt GitHub Releases** (GoReleaser) — auto-bootstrapped on first use by the `config-bench` plugin's `bin/` wrapper, or installed standalone (`go install`) for bare-terminal / non-Claude agents. **No npm package.** Public skill names move off the `env-*` family to **`config-bench`**, **`quizgen`**, **`config-refactor`**.

**Why:**

- **Granularity = one skill per plugin.** Claude Code installs *per plugin*, not per skill, so each skill is its own plugin — a user grabs only what they need (`config-refactor` without the benchmark, `quizgen` alone). Only `config-bench` carries the binary; the other two are pure skills.
- **Two channels mirror [ADR-0010](0010-rename-to-claude-benchmark.md)'s agent split.** The marketplace is Claude-Code-only (turnkey now); npx `skills` is explicitly multi-agent (`.claude` / `.codex` / `.agents`) and is the channel the planned `codex-benchmark` / `copilot-benchmark` siblings reach users through. Built now, agnostic-ready.
- **Plugin bootstraps the binary, deterministically.** A committed `bin/claude-benchmark` wrapper (auto-added to the Bash PATH) lazily downloads the matching prebuilt release into `${CLAUDE_PLUGIN_DATA}` on first run, checksum-verified. One install (`/plugin install`) sets up skill *and* CLI.
- **`config` names the whole bundle honestly.** The subject under test is the entire **Environment** — CLAUDE.md + `.claude/rules` + docs + memory — not just "context" (one slice) and not "environment" (reads as env-vars, the reason `env-*` was dropped). Agent-agnostic: CLAUDE.md / AGENTS.md / copilot-instructions are all "the AI config".

## Considered Options

- **A `setup-cli` skill that installs the binary.** Rejected: model-driven and probabilistic — the model must decide to call it, detect OS/arch, download, verify. A committed wrapper does the same deterministically.
- **npm/npx wrapper for the binary** (the old DISTRIBUTION.md plan, esbuild-style). Rejected: redundant once the plugin bootstraps the binary; adds a Node dependency, a second install step, and a permanent (un-renameable) npm name. `go install` already covers Go users.
- **A single multi-skill plugin.** Rejected: install is per-plugin, so it would force the binary on `config-refactor`-only users and bundle an unrelated tool under a benchmark-named plugin.
- **Keep the `env-*` names.** Rejected: "env" reads as environment variables and undersells the scope (whole config, not one file).
- **`config-optimize` for the refactor skill.** Rejected: `context-optimizer` is a crowded clone-lane (5+ repos); `config-refactor` is precise (restructure, behavior-preserved) and pairs with `config-bench` (refactor → prove no regression).

## Consequences

- Renames amend [ADR-0010](0010-rename-to-claude-benchmark.md)'s consequence list: skill dirs `env-benchmark → config-bench`, `generate-corpus → quizgen`, `env-restructure → config-refactor`. Applied to **living surfaces** only — the dirs, each SKILL.md `name:` + cross-refs, README, `internal/cli/bare.go`+test, and the CONTEXT.md reconcile. Earlier ADRs (0007/0008/0010) are left as historical record; this ADR is the authoritative current naming. ADR-0010's *core* (binary names the agent, skills stay agent-agnostic) stands.
- The binary's canonical artifact is the GitHub Release asset, shared across every agent and both channels. The plugin wrapper fetches it; the npx path needs it installed separately, so the `config-bench` skill must detect a missing binary and point the dev at `go install`.
- No `NPM_TOKEN`, no npm publish pipeline. Release pipeline = GoReleaser → GitHub Releases (binaries + `checksums.txt`) on tag `v*`.
- DISTRIBUTION.md holds the execution plan (files, manifests, pipeline, prereqs).
- **Open, deferred:** the marketplace/repo name `claude-god` still bakes in "claude", which sits awkwardly against the agent-agnostic future — left unresolved here, as in ADR-0010.
