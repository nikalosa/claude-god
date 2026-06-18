---
name: quizgen
description: Drafts a claude-benchmark corpus — probes and how they're graded — from a project's Before-branch context, in three independent streams run in parallel. Rule-based probes collect the important, easily-dropped rules from hand-selected docs (one subagent per doc, that doc's text only — no codebase, no general-knowledge questions); open-ended system/design probes and plan probes are drafted from the whole project. You draft; the dev reviews and finalizes. Use when authoring or extending a benchmark corpus, generating probes from a project's docs, or when the user mentions corpus generation or a steering config.
---

# Generate a benchmark corpus

Draft a frozen **corpus** for `claude-benchmark` from a project's **Before** context.
You draft; the dev reviews, edits, and finalizes — never an unreviewed author.

The corpus has three kinds of probe, generated as three independent streams. There is no
post-generation check: the rule-based draft prompt is written to skip general-knowledge
questions in the first place, and any rule-based probe Claude can still answer *without* its
doc is dropped by the dev at review.

## The three streams (generate independently, in parallel)

| Stream | Probe kind | Context it sees | Emits |
|---|---|---|---|
| 1 | **Rule-based** | **selected docs only** — one subagent per doc, that doc's text and nothing else | `{question, expected answer, check kind, proposed severity}` |
| 2 | **Open-ended** | **whole project** (Before worktree), one subagent | prompt-only system / design questions |
| 3 | **Plan** | **whole project** (Before worktree), one subagent | prompt-only task prompts (step-by-step plan) |

## Guardrails (enforce)

- **Generate from Before, freeze.** Assemble doc source with `git show <before>:<path>`.
  Never the After / working tree.
- **Context tracks stream.** Rule-based subagents see **only their one doc — no code, no
  other docs**. Open-ended and Plan subagents see the **whole project**.
- **One subagent per doc** for the rule-based stream — each collects that doc's rules. Keeps
  generation focused, parallel, and free of cross-doc bleed.
- **Backend = `claude -p --output-format json`** in a throwaway empty dir (rule-based) or a
  Before worktree (open-ended / plan). Read the assistant text from the JSON envelope's
  `result` field. Use the `claude -p` CLI for every generation call — not a direct API SDK,
  not an agent-orchestration harness.
- **Severity proposed → dev-confirmed.** Severity (`critical | high | medium`) is the dev's
  reading priority, not a gate — the benchmark never gates. Confirmed by the dev so the
  report sorts right.
- **Implementation probes are out of scope.** Probes are **Ask** or **Plan** mode only.

## Workflow

1. **Inputs.** Get the Before ref (e.g. `benchmark/before` from `claude-benchmark snapshot`)
   and the **steering config** ([steering-config.md](references/steering-config.md)). If none
   exists, draft one with the dev: source globs + emphasis/skip notes + proposed severities.
2. **Generate — three independent streams** (run them in parallel; prompts in
   [draft-prompts.md](references/draft-prompts.md)):
   - *Rule-based:* resolve the steering `sources` globs on Before; for **each doc**, run
     `claude -p` in an **empty** dir with **only that doc's text** in the prompt → rule-based
     candidates.
   - *Open-ended:* one `claude -p` in a **Before worktree** (codebase-aware) → prompt-only
     system / design probes.
   - *Plan:* one `claude -p` in a **Before worktree** → prompt-only task prompts (the run
     is later asked for a step-by-step plan, not execution).
3. **Review + finalize.** Show the dev all three streams grouped, severities flagged for
   confirmation. The dev edits conversationally and drops any rule-based probe answerable
   *without* its doc. On confirm, write `.benchmark/corpus/<name>.yaml` and the steering
   config to `.benchmark/steering.yaml`, and commit both onto Before — only when the dev says
   so. Regeneration **appends** new probes; never rewrites the frozen set.

## Output shape

Match the corpus YAML the benchmark already loads — read an existing corpus under
`.benchmark/corpus/` (or a shipped example) before writing, don't reinvent:

- `text_matches` — hard tokens (regex over the answer).
- `judge_rubric` (`facts` + `pass_score`) — prose graded by the judge against listed facts.
- `kind: open_ended` — prompt only, no rules; preference-graded.
- `kind: plan` — prompt only, no rules; preference-graded, but the run is asked for a
  step-by-step plan. Comparative probes (open_ended, plan) need a judge — run with
  `--judge` (see `examples/corpus/plan-smoke.yaml`).

## Validate

After freezing, the dev measures the noise floor (live, costs money):

```sh
claude-benchmark calibrate --target <repo> --corpus .benchmark/corpus/<name>.yaml \
  --branch <before> --no-memory-snapshot
```

Add `--judge` when the corpus has open-ended, plan, or `judge_rubric` probes — calibrate
must build a **Judge** for them, even though only rule-based probes have a noise floor.

Before-vs-Before; tighten or drop any flaky check before trusting a real Before-vs-After
run. A malformed corpus errors at load, before any spend.
