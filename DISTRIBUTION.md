# Distribution plan — ship `claude-benchmark` + its skills

**Status:** planned. Decisions locked in [ADR-0011](docs/adr/0011-distribution-and-public-skill-names.md). Execute on a clean branch off `main`. Repo must be **public** (required for `/plugin install`, npx fetch, and release-download URLs).

## Goal

Anyone installs a skill without a Go toolchain, grabs only the skill(s) they want, and — for the benchmark — gets the CLI with zero extra steps.

Two distribution channels, by design ([ADR-0011](docs/adr/0011-distribution-and-public-skill-names.md)):

- **Claude Code plugin marketplace** — turnkey, Claude-only. `/plugin install` carries the skill and (for `config-bench`) bootstraps the binary.
- **Vercel npx `skills`** — per-skill, agent-agnostic. `npx skills add nikalosa/claude-god@<skill>`. The channel future `codex-benchmark` / `copilot-benchmark` siblings reach users through.

## The three skills (one plugin each)

| plugin / skill | was | needs binary | contents |
|---|---|---|---|
| `config-bench` | `env-benchmark` | yes | skill + `bin/` wrapper (lazy-downloads the Go binary) |
| `quizgen` | `generate-corpus` | no | skill only |
| `config-refactor` | `env-restructure` | no | skill only (+ its `scripts/`, `references/`, sub-docs) |

Per-plugin install **is** per-skill here — grab `config-refactor` without the benchmark, or `quizgen` alone.

---

## Channel A — skills

### A1. Claude Code marketplace (repo is also the marketplace)

Do **not** use `.claude/` as a plugin root (holds `worktrees/`, settings, local state). Move skills into a clean `plugins/` tree.

- **`.claude-plugin/marketplace.json`** (repo root) — marketplace `claude-god`, lists the three plugins with `"source": "./plugins/<name>"`.
- **`plugins/<name>/.claude-plugin/plugin.json`** — name/description/version/repo/license (one per plugin).
- **`git mv .claude/skills/<old> → plugins/<new>/skills/<new>`** — sub-docs / `references/` / `scripts/` travel with each. Plugins discover skills at `<plugin-root>/skills/`.
- README install: `/plugin marketplace add nikalosa/claude-god` → `/plugin install config-bench@claude-god` (and/or `quizgen@…`, `config-refactor@…`).
- In-repo dogfooding (skills no longer auto-load from `.claude/skills`): `/plugin marketplace add ./` → install.

### A2. Vercel npx `skills`

Free once skills live under `plugins/*/skills/*/SKILL.md` — the Vercel CLI scans for `SKILL.md`. README notes the agent-agnostic path: `npx skills add nikalosa/claude-god@config-refactor`. For `config-bench` via npx the skill installs but the binary does **not** — the skill detects a missing `claude-benchmark` and points the dev at `go install` (see Channel B).

---

## Channel B — the Go binary

Canonical artifact = **GitHub Release asset**, shared by every agent and both channels. No npm.

### B1. Release pipeline
- **`.goreleaser.yaml`** — build `./cmd/claude-benchmark` for `darwin/linux × amd64/arm64`, raw binaries (`{{.Binary}}_{{.Os}}_{{.Arch}}`) + `checksums.txt`, publish a GitHub Release. Version via ldflags `-X github.com/nikalosa/claude-god/internal/cli.version={{.Version}}`.
- **`.github/workflows/release.yml`** — on tag `v*`: `goreleaser release --clean`. (No npm job, no `NPM_TOKEN`.)
- **`.github/workflows/ci.yml`** — `go build ./... && go test ./...` on PRs.

### B2. Plugin bootstrap (the turnkey path)
- **`plugins/config-bench/bin/claude-benchmark`** — committed wrapper, auto-added to the Bash PATH while the plugin is enabled. On invocation: if the real binary is absent from `${CLAUDE_PLUGIN_DATA}/`, detect os/arch → download the matching release asset + `checksums.txt` → verify sha256 → write (mode 0755); then `exec` it, propagating args + exit code. Idempotent: present → exec immediately. Prints a one-line "downloading…" only on the first run.
  - *Verify during build:* whether the wrapper receives `${CLAUDE_PLUGIN_DATA}` in its env, or must derive its store path from `$0`. Targets are darwin/linux only, so a POSIX-sh wrapper suffices.

### B3. Standalone (power users / non-Claude agents)
- `go install github.com/nikalosa/claude-god/cmd/claude-benchmark@latest` for Go users.
- Prebuilt GitHub Release binaries for everyone else (also what the wrapper downloads).
- brew tap / curl|sh — deferred (YAGNI), add if bare-terminal demand appears.

### B4. CLI `--version`
- **`internal/cli/root.go`** — `var version = "dev"`, set `rootCmd.Version`; GoReleaser overrides via ldflags. (`version` declared before `rootCmd` so the injected value is captured.)

---

## Skill rename — DONE; relocation pending

The `env-*` → `config-bench` / `quizgen` / `config-refactor` rename is **already applied in place** under `.claude/skills/`: dirs renamed, SKILL.md `name:` + cross-refs, README, `internal/cli/bare.go`+test, and the CONTEXT.md reconcile. ADRs 0007/0008/0010 left as historical record — [ADR-0011](docs/adr/0011-distribution-and-public-skill-names.md) carries the rename. Skills still auto-load for dogfooding.

Only **relocation** into the plugin tree remains, on the implementation branch (it disables `.claude/skills` auto-load, so it lands together with the marketplace manifests):

| now (in place) | → plugin tree |
|---|---|
| `.claude/skills/config-bench/` | `plugins/config-bench/skills/config-bench/` |
| `.claude/skills/quizgen/` | `plugins/quizgen/skills/quizgen/` |
| `.claude/skills/config-refactor/` | `plugins/config-refactor/skills/config-refactor/` |

---

## Files

| Action | Path |
|---|---|
| add | `.goreleaser.yaml`, `.github/workflows/release.yml`, `.github/workflows/ci.yml` |
| add | `.claude-plugin/marketplace.json` |
| add | `plugins/config-bench/.claude-plugin/plugin.json`, `plugins/config-bench/bin/claude-benchmark` |
| add | `plugins/quizgen/.claude-plugin/plugin.json`, `plugins/config-refactor/.claude-plugin/plugin.json` |
| move | `.claude/skills/{config-bench,quizgen,config-refactor}/` → `plugins/…/skills/…` (relocation; names already applied in place) |
| edit | `internal/cli/root.go` (version), `README.md`, the rename-sweep refs, `.gitignore` (`/dist/`, `plugins/config-bench/binary/`) |

---

## Manual prerequisites

1. Make repo public: `gh repo edit nikalosa/claude-god --visibility public`.
2. Tag the first release: `git tag v0.1.0 && git push --tags`.
3. (No npm account / token needed.)

---

## Verification (run before the real tag)

- `go build ./...` + `go test ./...` pass; `--version` prints the ldflags-injected value.
- All 4 `GOOS/GOARCH` targets cross-compile; `goreleaser release --snapshot --clean` (local dry-run).
- Wrapper: lazy download + checksum + arg/exit-code passthrough against a real release asset; idempotent on second run.
- All JSON manifests parse; `/plugin validate .` against `marketplace.json`; `/plugin marketplace add ./` then install all three.
- `npx skills add ./@config-refactor` (or the published repo) lands the skill in `.claude/skills`.

---

## Open

- Marketplace/repo name `claude-god` bakes in "claude" — sits awkwardly against the agent-agnostic future ([ADR-0011](docs/adr/0011-distribution-and-public-skill-names.md), [ADR-0010](docs/adr/0010-rename-to-claude-benchmark.md)). Resolve before mass adoption; renaming a GitHub repo auto-redirects, so it stays cheap pre-publish.
