# claude-god

> **Did a change to your AI assistant's environment make it better — or quietly worse?** `config-bench` measures it.

## The problem

Your Claude Code environment — `CLAUDE.md`, `.claude/rules`, docs, memory, skills, however you organize context for navigation — shapes how well the assistant works. Teams keep growing it: more rules, more docs, everything dumped into `CLAUDE.md`, until 80k+ tokens load on *every* turn. A crowded context dilutes attention: Claude starts ignoring the guidelines you wrote, hedges, slows down, and burns tokens for less (a good rule of thumb is to stay well clear of the window's degradation zone).

So you change things — slim the context, split the rules, add a skill, wire up a knowledge graph for better navigation. But you're flying blind. Is the new environment actually better than the old one? By how much? Did you cut a rule Claude was relying on? Token count alone lies: deleting rules "wins" on tokens while silently dropping your guardrails.

## The tool: `config-bench`

`config-bench` runs an A/B benchmark of two versions of your environment — **Before** vs **After** — against a corpus of probes, and reports side by side:

- **Guardrails held?** Every rule graded PASS/FAIL in each version. A Before-PASS → After-FAIL is a regression you'd otherwise ship blind.
- **How much better?** Exact token, cost, time, and tool-call deltas — *quantified*, not "feels lighter."
- **Did it degrade?** If quality drops as you trim, you see it before your team does.

You get a report a human reads. You decide — it never gates. Invoke it with `/claude-god:config-bench`, or ask Claude to benchmark your environment and it triggers automatically.

## Also included

- **`quizgen`** (`/claude-god:quizgen`) — drafts the corpus of probes `config-bench` runs, from your docs and code, so you don't hand-write them.
- **`config-refactor`** (`/claude-god:config-refactor`) — reorganizes `CLAUDE.md` + `.claude/rules` to shrink always-on context without losing a rule.

Each skill's deep doc: [config-bench](plugins/claude-god/skills/config-bench/SKILL.md) · [quizgen](plugins/claude-god/skills/quizgen/SKILL.md) · [config-refactor](plugins/claude-god/skills/config-refactor/SKILL.md).

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

It drafts probes from your docs and code in three streams; you review and edit, and it freezes the set to `.benchmark/corpus/*.yaml`.

**2. Make your environment change** on a branch — slim `CLAUDE.md`, add a skill, restructure rules, whatever you're testing. (`config-refactor` can do a restructure for you.)

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

Every probe runs in both versions — headless and read-only.

**4. Read the report.** Side by side: efficiency **Numbers** (tokens, time, cost, tool-calls), each rule PASS/FAIL in Before vs After, and head-to-head preference for open-ended and plan probes. The win is **efficiency up, rules held** — and you decide.

Override anything: `--before`/`--after` (committishes), `--corpus <file>`, `--samples N`, `--yes` (skip the prompt). The `run`, `snapshot`, and `calibrate` subcommands are for power users. Full reference: `claude-benchmark --help`.

## How it grades

Three kinds of probe, generated as independent streams ([CONTEXT.md](CONTEXT.md) is the glossary):

- **Rule-based** — questions whose answer lives in your docs (*"how are monetary amounts typed?"*). Each version graded right/wrong on its own. A PASS→FAIL flip is a **regression**.
- **Open-ended** — system/design questions with no fixed answer (*"how do the betting and ledger services communicate?"*). The two versions' answers are compared head-to-head by a **Judge**, alongside Numbers.
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
