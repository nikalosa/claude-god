# claude-god

> Benchmark, generate, and restructure a repo's **Claude Code context configuration** — so you can slim your `CLAUDE.md` and *prove* you didn't drop a rule Claude was relying on.

`claude-god` is an umbrella for Claude Code tooling. It ships as **one plugin with three skills**, plus a **CLI** the benchmark skill drives:

| skill | what it does | invoke |
|---|---|---|
| [**`config-bench`**](plugins/claude-god/skills/config-bench/SKILL.md) | A/B-benchmark two context configs (Before vs After) and report what got faster, cheaper, or regressed | `/claude-god:config-bench` |
| [**`quizgen`**](plugins/claude-god/skills/quizgen/SKILL.md) | Draft a benchmark **corpus** — probes plus how they're graded — from your docs and code | `/claude-god:quizgen` |
| [**`config-refactor`**](plugins/claude-god/skills/config-refactor/SKILL.md) | Reorganize `CLAUDE.md` + `.claude/rules` to shrink always-on context without losing a rule | `/claude-god:config-refactor` |

Skills are normally **invoked automatically** by Claude when they fit what you're doing — you rarely type the slash form. The CLI (`claude-benchmark`) is the engine behind `config-bench` and also runs standalone.

## Why

A project's Claude context (`CLAUDE.md`, `.claude/rules/*`, docs, memory) grows until it's bloated: critical one-line rules compete with thousands of lines of docs, so answers hedge, buried rules get silently ignored, and tokens are spent for marginal value. You want to restructure — shrink `CLAUDE.md`, split it per-area, push reference material into docs Claude loads on demand.

The blocker: no way to know whether a restructure actually helped or quietly dropped a rule. Token reduction alone is a fake win — trivially gamed by deleting rules. `claude-god` closes that gap: `config-refactor` does the restructure, `config-bench` proves the rules survived and the work got leaner.

## Install

### As a Claude Code plugin (recommended)

```sh
/plugin marketplace add nikalosa/claude-god
/plugin install claude-god@claude-god
```

This installs all three skills. On the first benchmark run, the `claude-benchmark` CLI is **auto-downloaded** (checksum-verified) — no Go toolchain required.

**Prerequisites:** an authenticated `claude` CLI and `git` on your PATH. **No API key** — the tool reuses your existing Claude login.

### CLI only

Go users:

```sh
go install github.com/nikalosa/claude-god/cmd/claude-benchmark@latest
```

Otherwise download a prebuilt binary (darwin/linux × amd64/arm64) from [Releases](https://github.com/nikalosa/claude-god/releases). Windows: use WSL — native Windows is not yet supported.

## Quick start

A full Before/After benchmark, end to end, inside the repo you want to test:

**1. Draft a corpus** — ask Claude to generate one, or invoke the skill:

```
/claude-god:quizgen
```

It drafts probes from your docs and code in three streams, you review and edit, and it freezes the set to `.benchmark/corpus/*.yaml`.

**2. Make your change** on a branch — e.g. slim `CLAUDE.md`. Let `config-refactor` plan it for you:

```
/claude-god:config-refactor
```

**3. Run the benchmark** — the skill, or the CLI directly:

```sh
claude-benchmark            # auto-detects Before/After from git
```

It prints a spend plan and waits for confirmation before spending anything:

```
Benchmark plan
  Before:      merge-base(main, HEAD)
  After:       HEAD
  Corpus:      .benchmark/corpus/self.yaml (12 probe(s))
  Samples:     1 per environment
  Concurrency: 8
  Runs:        24 claude -p calls (12 probes × 1 samples × 2 envs)
Proceed? [y/N]
```

Every probe runs in both configs — headless and read-only.

**4. Read the report.** Side by side: efficiency **Numbers** (tokens, time, cost, tool-calls), each rule PASS/FAIL in Before vs After, and head-to-head preference for open-ended and plan probes. The win is **efficiency up, rules held**. You decide — the benchmark reports, it never gates.

Override anything: `--before`/`--after` (committishes), `--corpus <file>`, `--samples N`, `--yes` (skip the prompt). The `run`, `snapshot`, and `calibrate` subcommands are for power users. Full reference: `claude-benchmark --help`.

## How it grades

Three kinds of probe, generated as independent streams ([CONTEXT.md](CONTEXT.md) is the glossary):

- **Rule-based** — questions whose answer lives in your docs (*"how are monetary amounts typed?"*). Each config graded right/wrong on its own. A PASS→FAIL flip is a **regression**.
- **Open-ended** — system/design questions with no fixed answer (*"how do the betting and ledger services communicate?"*). The two configs' answers are compared head-to-head by a **Judge**, alongside Numbers.
- **Plan** — Claude produces a step-by-step plan (no execution); Before's plan vs After's, by preference plus Numbers.

The proxy: if Claude can correctly *answer* how to wire a new service, it will likely wire it correctly — so the benchmark mostly asks, rarely executes.

## Design docs

- **[docs/PRD.md](docs/PRD.md)** — full design and locked decisions
- **[CONTEXT.md](CONTEXT.md)** — vocabulary / glossary (read first when contributing)
- **[docs/adr/](docs/adr/)** — architecture decision records

## Build from source

Requires **Go 1.24+** (earlier internal linkers omit `LC_UUID`, which recent macOS rejects at load).

```sh
go build ./...
go test ./...
go run ./cmd/claude-benchmark        # bare: auto-detect everything, then confirm
```

## Layout

```
cmd/claude-benchmark/   CLI entrypoint (thin; delegates to internal/cli)
internal/
  cli/          cobra command tree (bare benchmark + run/calibrate/snapshot)
  autodetect/   resolve Before/After committishes from git state
  harness/      isolated per-probe run: worktree, memory swap, claude -p, diff capture
  parser/       stream-json JSONL -> RunRecord
  dsl/          YAML predicate DSL -> per-rule PASS/FAIL
  judge/        LLM grading (rubric check + preference comparison) over claude -p
  aggregator/   N samples -> median stats + majority-vote outcomes
  report/       markdown (default) + JSON rendering
plugins/claude-god/     the Claude Code plugin: the three skills + the bin/ wrapper
```

## License

MIT — see [LICENSE](LICENSE).
