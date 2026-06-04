# claude-validator

A/B-test two Claude Code context configurations of a repo — the bloated **Before** and
the restructured **After** — and read what got faster, cheaper, and tighter **without
losing the rules that matter**.

A project's Claude context (CLAUDE.md, `.claude/rules/*`, docs, memory) grows until it's
bloated: critical one-line rules compete for attention with thousands of lines of docs, so
answers hedge, buried rules get silently ignored, and tokens are spent for marginal value.
You want to restructure — shrink CLAUDE.md, split it per-area, push reference material into
docs and skills, add better references so Claude loads the right chunk on demand. The
blocker: no way to know whether a restructure actually helped or quietly dropped a rule
Claude relied on. Token reduction alone is a fake win — trivially gamed by deleting rules.

**The bet:** a leaner, better-referenced environment makes Claude faster and cheaper *while
still honoring your rules*. **The proxy:** if Claude can correctly answer "how should I wire
a new service and its infrastructure?", we bet it will wire it correctly — so the validator
mostly *asks*, rarely executes.

A drafting helper (the **Generator**) produces a **Corpus** of probes in three independent
streams, which a human then reviews and finalizes:

1. **Rule-based probes** — from your selected docs only. *"How are monetary amounts typed?"*
   The answer lives in the docs; each environment's answer is graded right/wrong.
2. **Open-ended probes** — system/design questions, generated with the whole codebase in
   view. *"How do the betting and ledger services communicate?"* No single right answer; the
   two environments' answers are compared head-to-head, alongside cost/token/time **Numbers**.
3. **Plan probes** — Claude produces a step-by-step plan (no execution); Before's plan and
   After's plan are compared, alongside **Numbers**.

Every probe runs in both environments (N=3 each, headless, read-only), and the report
measures two things:

- **Efficiency — the main goal.** The **Numbers**: tokens, time, money, and tool calls
  (fewer, and aimed at the right chunk instead of grepping around). Exact and numeric.
- **Quality — the guardrail.** Are the rules still honored, and do the answers and plans
  read *better*? Right/wrong per rule; head-to-head preference for the rest.

The win is **efficiency up with quality held or improved**. The output is a **report a human
reads** — Numbers, rule answers side by side, design/plan answers compared — and you decide.
The validator never gates.

Full design: [docs/PRD.md](docs/PRD.md). Glossary: [CONTEXT.md](CONTEXT.md). Key decisions:
[docs/adr/](docs/adr/).

## Generating the corpus

You don't hand-write every probe. The **`generate-corpus`** skill (the **Generator**) drafts
a corpus from the **Before** branch in three independent streams, then you review and finalize:

- **Rule-based** — for *each* selected doc, a separate subagent sees **only that doc** (no
  code) and extracts the important, easily-dropped rules → a question, its doc-stated answer,
  and a check (regex for hard tokens, rubric for prose).
- **Open-ended** — one pass over the **whole project** drafts system/design questions (service
  communication, data flow, infrastructure, coupling vs facade vs direct DB access).
- **Plan** — one pass over the **whole project** drafts realistic tasks to plan step-by-step.

You then talk to the skill — edit probes, drop any rule-based probe Claude can answer
*without* its doc, confirm severities (reading priority, never a gate) — and freeze the set
onto Before. Regeneration appends new probes; it never rewrites the frozen corpus. Details:
[`.claude/skills/generate-corpus`](.claude/skills/generate-corpus/SKILL.md).

## Status

Pre-v1. Scope order: **Rule-based probes → Open-ended probes → Plan probes**; executing
tasks for real is deferred. Tracked in GitHub issues #1–#7.

## Build & run

Requires **Go 1.24+** (earlier internal linkers omit `LC_UUID`, which recent macOS rejects at load).

```sh
go build ./...
go run ./cmd/claude-validator --help
go run ./cmd/claude-validator run --level l1 --target . --corpus <dir>
# Plan probes (Mode = Plan): a step-by-step plan, graded by preference like open-ended.
go run ./cmd/claude-validator run --level l3 --target . \
  --corpus examples/corpus/l3-smoke.yaml --before <before> --after <after>
```

## Layout

```
cmd/claude-validator/   CLI entrypoint (thin; delegates to internal/cli)
internal/
  cli/          cobra command tree (root + run)
  harness/      isolated per-probe run: worktree, memory swap, claude -p, diff capture  (#4)
  parser/       stream-json JSONL -> RunRecord; shape notes + golden fixtures in testdata  (#3)
  dsl/          YAML predicate DSL -> per-rule PASS/FAIL  (#5+)
  judge/        Anthropic-API grading escape hatch (L2 rubric, L3 plan preference)  (later)
  aggregator/   N=3 runs -> median stats + majority-vote outcomes  (#6)
  report/       markdown (default) + JSON rendering  (#5+)
```

Most `internal/` packages are stubs today — each holds a doc comment stating its role
and the issue that implements it.
