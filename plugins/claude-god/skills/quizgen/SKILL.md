---
name: quizgen
description: Draft a claude-benchmark Corpus — the probes a config change is tested against — from a project's baseline docs and code. rule_based probes come from selected docs (one subagent per doc, docs-only); open_ended and plan probes come from the whole project. You draft; the dev reviews and finalizes. Use when generating or extending a benchmark corpus or probes, or when config-bench needs a corpus that doesn't exist yet.
---

# Generate a benchmark corpus

Draft a frozen **Corpus** for `claude-benchmark` — the probes a config change is tested against. You **draft**; the dev reviews, edits, and finalizes — never an unreviewed author. Glossary: [CONTEXT.md](https://github.com/nikalosa/claude-god/blob/main/CONTEXT.md).

**Always generate from Before** — the config *without* the change under test. If the dev has already edited CLAUDE.md, rules, or docs, the corpus must come from **before** those edits, never the current tree — else the questions leak the change and grade the answer they were built from. Then freeze.

## What the corpus needs (infer from the request; ask only if missing)

- **Kinds** — default **all three**: rule_based, open_ended, plan.
- **How many** — default ≈ **20 rule_based, 5 open_ended, 5 plan**. The dev can set any count per kind.
- **Docs** (rule_based source) — default `CLAUDE.md`, nested `**/CLAUDE.md`, and `.claude/rules/**/*.md`. The dev can add or narrow.
- **Before ref** — the config without the change. Resolve as config-bench does: uncommitted edits → `HEAD`; changes committed on a branch → `merge-base(<default-branch>, HEAD)`. The dev can name it. **Never the working tree** when it holds the change.

## Generate — three streams, in parallel, read-only

Backend: `claude -p --output-format json --allowedTools Read Grep Glob` — **read-only**. Never `--permission-mode bypassPermissions` (it trips the safety classifier and isn't needed). Read the draft from the JSON `.result` field.

Feed **Before's** text only: rule_based gets each doc via `git show <before>:<path>`; open_ended/plan run inside a throwaway **Before worktree** (`git worktree add <tmp> <before>`), never the dev's current checkout.

**rule_based** — one subagent per doc, **that doc's text only, in an empty throwaway dir outside the project** (so the project's own CLAUDE.md doesn't auto-load and answer for free). Extract the specific, buried, easily-dropped rules a maintainer wants to survive a refactor — conventions, invariants, required procedures (e.g. "amounts are string-typed", "run the migration script before deploy"). Per rule emit: a direct question; the expected answer; the check kind (`text_matches` regex / `judge_rubric` facts); a proposed **severity** (critical|high|medium). Skip anything answerable from general knowledge without the doc.

**open_ended** — one subagent, the **Before worktree** (code + docs at Before). Real architectural / design / domain questions a developer actually asks — how data flows across services, where coupling sits vs a facade vs direct DB access, the shape of the event bus, why a boundary exists. These mirror real-world scenarios, because that's what a config change must keep Claude able to answer. **Not** trivial one-paragraph definitions. Prompt only — no expected answer, no checks. Favor questions a docs restructure could make sharper or vaguer.

**plan** — one subagent, the **Before worktree** (code + docs at Before). Real task prompts that need project knowledge to plan well — "wire a new service end to end", "add a field across the read and write models". Ask for a step-by-step plan, **not** execution. Prompt only. Favor tasks where a restructure could change which steps Claude remembers.

## Review + write

1. Show the dev all three streams grouped. Flag every **severity** for confirmation — **criticals matter most** (they sort the report). The dev edits conversationally and drops any rule_based probe Claude can answer *without* its doc.
2. On confirm, write `.benchmark/corpus/<name>.yaml` ([corpus-schema.md](references/corpus-schema.md) — read an existing corpus first if one exists) and the **steering config** to `.benchmark/steering.yaml` ([steering-config.md](references/steering-config.md): sources + emphasis + confirmed severities, so regeneration is a reviewed, additive diff).
3. Regeneration **appends** new probes; never rewrites the frozen set. Commit both onto the baseline — only when the dev says so.

## Validate

A malformed corpus errors at **load**, before any spend — so the next `config-bench` run is the check. No separate validate step.
