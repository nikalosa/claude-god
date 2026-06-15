# Distribution: marketplace + npx skills, plugin-bootstrapped binary; public names config-bench / quizgen / config-refactor

**Status:** accepted (supersedes the npm-centric plan in the old DISTRIBUTION.md; amends the name sweep in [ADR-0010](0010-rename-to-claude-benchmark.md))

> **Amendment (2026-06-15):** ships as **one `claude-god` plugin bundling all three skills**, not three one-skill plugins. Reason: manual invocation is namespaced `/<plugin>:<skill>`, so one plugin yields the clean `/claude-god:config-bench` instead of the doubled `/config-bench:config-bench`; and the lazy `bin/` wrapper already prevents the binary being forced on skill-only users, which was the only reason to split. Per-skill install still exists via npx. Edits below reflect this.

How the tool ships. The skills distribute through **two channels at once**: the **Claude Code plugin marketplace** (`claude-god`, a single plugin bundling all three skills) for the turnkey Claude path, and **Vercel's npx `skills` CLI** for per-skill, agent-agnostic install. The Go binary ships as **prebuilt GitHub Releases** (GoReleaser) — auto-bootstrapped on first use by the `claude-god` plugin's `bin/` wrapper, or installed standalone (`go install`) for bare-terminal / non-Claude agents. **No npm package.** Public skill names move off the `env-*` family to **`config-bench`**, **`quizgen`**, **`config-refactor`**.

**Why:**

- **One `claude-god` plugin, three skills.** Manual skill invocation is namespaced `/<plugin>:<skill>`, so a single plugin gives the clean `/claude-god:config-bench` (vs the doubled `/config-bench:config-bench` of one-plugin-per-skill). The original reason to split — not forcing the binary on skill-only users — is moot: the `bin/` wrapper downloads the CLI **lazily**, only when `config-bench` actually runs, and the other two skills are inert markdown until invoked. Per-*skill* install survives via the npx channel, which scans `SKILL.md` independent of plugin layout. Skills are normally model-invoked by `description` anyway; the slash name is rarely typed.
- **Two channels mirror [ADR-0010](0010-rename-to-claude-benchmark.md)'s agent split.** The marketplace is Claude-Code-only (turnkey now); npx `skills` is explicitly multi-agent (`.claude` / `.codex` / `.agents`) and is the channel the planned `codex-benchmark` / `copilot-benchmark` siblings reach users through. Built now, agnostic-ready.
- **Plugin bootstraps the binary, deterministically.** A committed `bin/claude-benchmark` wrapper (auto-added to the Bash PATH) lazily downloads the matching prebuilt release into `${CLAUDE_PLUGIN_DATA}` on first run, checksum-verified. One install (`/plugin install`) sets up skill *and* CLI.
- **`config` names the whole bundle honestly.** The subject under test is the entire **Environment** — CLAUDE.md + `.claude/rules` + docs + memory — not just "context" (one slice) and not "environment" (reads as env-vars, the reason `env-*` was dropped). Agent-agnostic: CLAUDE.md / AGENTS.md / copilot-instructions are all "the AI config".

## Considered Options

- **A `setup-cli` skill that installs the binary.** Rejected: model-driven and probabilistic — the model must decide to call it, detect OS/arch, download, verify. A committed wrapper does the same deterministically.
- **npm/npx wrapper for the binary** (the old DISTRIBUTION.md plan, esbuild-style). Rejected: redundant once the plugin bootstraps the binary; adds a Node dependency, a second install step, and a permanent (un-renameable) npm name. `go install` already covers Go users.
- **Three one-skill plugins** (the original decision, reversed 2026-06-15). Gave per-plugin install granularity, but namespacing forced the doubled `/config-bench:config-bench` skill name. Dropped once the lazy binary removed the only real cost of bundling — skill-only users never trigger the download — making the single `claude-god` plugin (cleaner names, simpler marketplace) the better call. npx still covers per-skill install.
- **Keep the `env-*` names.** Rejected: "env" reads as environment variables and undersells the scope (whole config, not one file).
- **`config-optimize` for the refactor skill.** Rejected: `context-optimizer` is a crowded clone-lane (5+ repos); `config-refactor` is precise (restructure, behavior-preserved) and pairs with `config-bench` (refactor → prove no regression).

## Consequences

- Renames amend [ADR-0010](0010-rename-to-claude-benchmark.md)'s consequence list: skill dirs `env-benchmark → config-bench`, `generate-corpus → quizgen`, `env-restructure → config-refactor`. Applied to **living surfaces** only — the dirs, each SKILL.md `name:` + cross-refs, README, `internal/cli/bare.go`+test, and the CONTEXT.md reconcile. Earlier ADRs (0007/0008/0010) are left as historical record; this ADR is the authoritative current naming. ADR-0010's *core* (binary names the agent, skills stay agent-agnostic) stands.
- The binary's canonical artifact is the GitHub Release asset, shared across every agent and both channels. The plugin wrapper fetches it; the npx path needs it installed separately, so the `config-bench` skill must detect a missing binary and point the dev at `go install`.
- No `NPM_TOKEN`, no npm publish pipeline. Release pipeline = GoReleaser → GitHub Releases (binaries + `checksums.txt`) on tag `v*`.
- DISTRIBUTION.md holds the execution plan (files, manifests, pipeline, prereqs).
- **Open, deferred:** the marketplace/repo name `claude-god` still bakes in "claude", which sits awkwardly against the agent-agnostic future — left unresolved here, as in ADR-0010.
- **Open, deferred — Windows.** v1 ships **darwin/linux only**: GoReleaser builds no Windows binary and the `config-bench` `bin/` wrapper is a POSIX-sh script that recognizes only Darwin/Linux. Native Windows therefore can't bootstrap the CLI; Windows devs are covered via **WSL** (runs as linux/amd64, wrapper works unchanged). The pure skills (`quizgen`, `config-refactor`) are OS-agnostic. Native support, if demand appears, = add `windows` to the GoReleaser `goarch` set + ship a `.ps1`/`.cmd` download wrapper beside the sh one. Not a blocker for v1.
- The execution checklist formerly in `DISTRIBUTION.md` is intentionally **untracked** (local-only, gitignored) — this ADR is the durable record; the implementation landed on branch `distribution-plugins`.
