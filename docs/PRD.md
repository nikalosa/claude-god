# claude-validator — PRD

**Status:** Pre-v1. L1 (rule-based recall) shipped and dogfooded against the validator's own CLAUDE.md; L2 (open-ended) and L3 (plan) in progress. Real execution (L4) deferred.
Glossary: [CONTEXT.md](../CONTEXT.md). Decisions: [docs/adr/](adr/). Work tracked in GitHub issues.

> This PRD is the long-form of the [README](../README.md). Where they differ, the README and the ADRs win — this document is kept in sync with them, not the reverse.

## Problem

A project's Claude context — `CLAUDE.md` (root + nested), `.claude/rules/*`, `docs/`, and auto-memory — grows until it's bloated. Critical one-line rules (monetary-as-string, use the migration script, no FK between CQRS read models, brand-resolution priority) compete for the model's attention against thousands of lines of supporting documentation. The symptoms:

- Responses are bloated, hedging, over-explanatory.
- Critical buried rules are silently ignored during real work, despite being "present."
- Token cost per session is high; most of it buys marginal value.

The fix is to restructure: shrink the always-loaded context, split `CLAUDE.md` per area, push reference material into `docs/` and skills, add better references so Claude loads the right chunk on demand. The blocker: **no way to know whether a restructure actually helped or quietly dropped a rule Claude relied on.** Token reduction alone is a fake win — trivially gamed by deleting the rules that mattered.

PAM (a ~700-line `CLAUDE.md` plus ~15 rules files) is the motivating case, but the tool is repo-agnostic: it takes any `--target` repo and its `--corpus`.

## The bet, and the proxy

**The bet:** a leaner, better-referenced environment makes Claude faster and cheaper *while still honoring the rules.*

**The proxy:** if Claude can correctly answer "how should I wire a new service and its infrastructure?", it will wire it correctly. So the validator mostly **asks**, and only rarely **executes** — questions are cheap, safe, parallelizable, and graded on the assistant's *text*.

## What the report measures

Every probe runs in both environments — the messy **Before** and the restructured **After** — N=3 each, headless and read-only. The report measures two things:

- **Efficiency — the main goal.** The **Numbers**: tokens (input / output / cached), wall-clock, `total_cost_usd`, and tool-call counts — fewer calls, aimed at the right chunk instead of grepping around. Exact and numeric. A restructure earns its keep by making Claude cheaper and faster.
- **Quality — the guardrail.** What stops efficiency being gamed by deleting rules:
  - **Rules still honored** — rule-based probes graded right/wrong per environment. A Before-PASS → After-FAIL flip is a **Regression** ("what you compromised").
  - **Answers and plans read better** — open-ended and plan probes compared head-to-head by the **Judge** across conciseness, exhaustiveness, directness.

**The win is efficiency up with quality held or improved.** The output is a **report a human reads** — Numbers, rule answers side by side, design/plan answers compared — and the developer decides. **The validator never gates.** A non-zero exit on a `critical` Regression survives as a harmless optional bit for anyone wiring CI later — not the point. See [ADR-0002](adr/0002-report-not-gate-and-preference-judging.md).

## Probes — three streams

A **Probe** is one prompt plus how its response is graded. Three kinds, built in this order:

1. **Rule-based probe** — drawn from selected **docs only**. *"How are monetary amounts typed?"* The answer lives in the docs; each environment's answer is graded right/wrong against a fixed set of **Rules**, each carrying a **Severity** (`critical | high | medium`) and one or more **Checks**. This stream is the quality guardrail and the only place a **Regression** is detected.
2. **Open-ended probe** — system/design questions, generated with the whole codebase in view. *"How do the betting and ledger services communicate?"* No single right answer; the two environments' answers are compared head-to-head by the Judge, alongside Numbers. Report-only — it cannot produce a false "nothing compromised."
3. **Plan probe** — Claude produces a step-by-step plan, no execution. Before's plan and After's plan are compared (tighter? steps lost or gained?), alongside Numbers.

**Tier** (L1–L4) is the orthogonal difficulty axis kept by the `--level` flag: L1 recall, L2 design Q&A, L3 plan-only, L4 full implementation. L4 — running tasks for real — is **deferred**; the proxy above is why the tool is useful without it.

## How a run works

- **Before / After** are git branches in the Target. Each **Run** spawns a fresh `git worktree` off the right branch, runs Claude headlessly inside, captures the stream, and cleans up (`git worktree remove --force`). Reproducible months later.
- **Project memory** (`~/.claude/projects/<slug>/memory/`) is snapshotted into the run by default so user-level memory drift doesn't pollute the A/B; `--no-memory-snapshot` opts out.
- **Headless and read-only.** `claude -p "<prompt>" --output-format stream-json --permission-mode bypassPermissions --disallowedTools Agent Bash Edit Write WebFetch --disable-slash-commands`. The graded signal is the assistant **text**; the model keeps `Read/Grep/Glob` to inspect the Environment but cannot mutate the tree, shell out, hit the network, or fire a skill. Skills aren't part of the Environment, so disabling them removes noise, not signal ([ADR-0006](adr/0006-headless-runs-read-only.md)). Diff capture still runs but records nothing until L4 lands.
- **No preamble** is injected — whatever Claude needs comes from the Target's CLAUDE.md, rules, and memory, which is exactly what's under test.
- **N=3** samples per environment: median for Numbers, majority vote for rule outcomes, adaptive expansion to N=5 when a `critical` rule's samples disagree. Same-prompt repeats — the variance *is* the noise floor.
- **Bounded parallelism.** Runs are independent, so they execute in one `errgroup` pool (`--concurrency`, default 8). Cost and tokens stay **exact** (the same work bills identically however it's scheduled); **Duration degrades to advisory** and the report marks it not-comparable unless `--concurrency 1` ([ADR-0005](adr/0005-parallel-benchmark-runs.md)).

## Grading

- **Pattern-first.** Rule-based **Checks** are a small YAML predicate DSL over the run record — `text_matches`, `bash_call_matches`, `diff_added_regex`, `not(...)`, `and(...)`, `or(...)`, `unless_path_matches`, with a function escape hatch for the long tail. Deterministic, so the A/B is free of judge noise. A rule passes only if all its checks pass.
- **Judge** — the LLM grader, for what a regex can't express. It runs `claude -p` in a throwaway **empty** directory (no CLAUDE.md, no rules, no tools) so the Environment it grades can't sway it, and reuses the developer's existing OAuth login — no `ANTHROPIC_API_KEY`, no separate billing ([ADR-0003](adr/0003-judge-backend-claude-p.md)). Two modes: **Rubric check** (absolute — scores an answer against an explicit fact-list, for rule-based prose a regex can't catch) and **Preference comparison** (comparative — picks the better of Before/After for a dev to read, both orderings, a win counting only if it survives both; report-only). `claude -p` exposes no temperature, so the Judge isn't bit-deterministic; the median-of-3 design absorbs the wobble.

## Corpus and the Generator

- A **Corpus** is the per-Target set of probes — the dataset run Before and After. The Target owns its real corpus (under `<target>/.validator/`); the tool ships an example corpus only.
- The **Generator** is a human-backstopped **drafting** skill, not unattended auto-generation. It runs `claude -p` against the **Before** branch over hand-selected source text (CLAUDE.md, rules, docs, pasted text) and drafts the three streams — rule-based from the docs in isolation, open-ended and plan probes codebase-aware. The dev reviews and edits conversationally, then **freezes** the result onto Before. Generating from Before (not the suspect After) is what keeps a dropped rule detectable — the dropped line is still there to write a probe from ([ADR-0004](adr/0004-corpus-generation-isolated-from-before-drafting.md)).
- A **Steering config** (selected-doc globs + emphasis/skip notes + proposed severities) is checked in beside the frozen corpus so generation is reproducible; regeneration is a reviewed, additive diff, never an in-place rewrite.

## Report

Markdown by default, `--json` for tooling. Order: quality findings first (Regressions — "what compromised"; New passes — "what improved"), then the **Numbers** deltas (per-probe and totals; input / cached / output tokens, time, `total_cost_usd`; per-tool-call token attribution, recursing into sub-agent calls), then the rule × environment × sample PASS/FAIL matrix for spotting flakiness vs real regression, then side-by-side tool-call traces for any rule that flipped.

## Build it in Go

The tool is subprocess orchestration (`claude -p`, `git worktree`, `git diff`) plus fan-out parallelism over a deterministic data pipeline (stream-json → run record → DSL grade → aggregate → report). `os/exec`, goroutines + `errgroup`, and a single static binary fit all three; Go's forced-explicit struct schemas align with the determinism the tool's credibility rests on ([ADR-0001](adr/0001-go-runtime.md)). Requires Go 1.24+ (earlier internal linkers omit `LC_UUID`, which recent macOS rejects at load).

## Module sketch

Packages under `internal/`:

- **harness** — worktree lifecycle, memory snapshot swap, `claude -p` invocation, stream + diff capture, cleanup. Repo-agnostic.
- **parser** — stream-json JSONL → **RunRecord**: ordered tool calls with per-call token attribution (recursing into `Agent` sub-agent calls), file mutations, per-turn timing, total cost.
- **dsl** — loads YAML, evaluates Checks against a RunRecord → per-rule PASS/FAIL.
- **judge** — `claude -p` empty-dir backend for Rubric check and Preference comparison, behind one interface (swappable to the Anthropic API later if true temp-0 is ever needed).
- **aggregator** — N=3 / N=5 run records → median Numbers + majority-vote outcomes.
- **report** — markdown + JSON.
- **cli** — cobra tree: `run --level … --target … --corpus …`, plus `snapshot` (pin an Environment to a branch) and `calibrate` (Before-vs-Before noise floor).

harness, parser, dsl, and aggregator are testable with no live Claude — replay fixtures of stream-json + git-diff suffice.

## Testing

Tests verify **observable behavior** given fixed input, never implementation details — the validator's credibility rests on determinism, so its own tests are deterministic. Parser: golden-file tests over recorded stream-json fixtures (flat runs, sub-agent recursion, errors, mid-stream `usage`). DSL: pure unit tests over every primitive plus realistic compositions. Aggregator: synthetic run records — all agree, critical-disagree → expand, non-critical-disagree → flaky, median odd vs even, majority-vote ties. Report: snapshot tests. Harness: one integration smoke against a tiny committed example repo, gated behind an env var. Plus the self-test: dogfood L1 against the validator's own lean CLAUDE.md — all checks should pass.

## Out of scope (v1)

- **Real execution during runs (L4).** Tasks are designed never to need live docker/postgres/network; the grader checks intent, not outcome. The read-only profile is scoped to the answer/plan tiers; L4 will need a write-enabled profile when it lands.
- **Soft style rules in the corpus** (terseness, no over-explanation). Real wins, but noisier to grade and the system prompt already biases terse — defer until grading is proven stable.
- **Unattended corpus auto-generation.** The Generator is a *drafting* aid with a human in the loop, deliberately — an LLM reading a bloated Environment is blind to the same buried rules the bloat hides.
- **Automated restructuring driven by results.** Future skill, once the validator is trusted.
- **Non-Claude-Code agents** (Cursor, Cline, Aider). Possible later; not the v1 target.
- **CI dashboard / web UI.** Markdown + JSON only.

## Repository structure

`claude-validator` lives in its own sibling repo (`~/Desktop/claude-god/`), separate from any project it validates — developing it inside a bloated target would degrade every dev session with the exact problem the tool exists to detect. Consumer projects host only their own corpus + config under `<repo>/.validator/`; the CLI's `--target` / `--corpus` keep the boundary explicit. The validator's own `CLAUDE.md` is intentionally lean and self-validated against its own corpus, as a demonstration of the methodology.

## Eventual scope (beyond v1)

A broader Claude Code hygiene toolkit: skills that auto-generate corpora from a target, skills that propose restructure batches and grade them, possibly a registry of community corpora for popular framework conventions. All depend on v1 being trusted first — v1 is the foundation; don't preempt it.
