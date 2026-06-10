---
name: env-restructure
description: Reorganizes a repo's Claude Code context (root CLAUDE.md, .claude/rules/*, nested CLAUDE.md, docs) to shrink always-on context while keeping every instruction reachable — a verbatim reorganization, never a rewrite. Produces a self-contained migration plan a fresh session can execute. Use when the user says "slim/restructure/reorganize CLAUDE.md", "reduce always-on context", "my CLAUDE.md is too big", "split rules", "context bloat", or "too much loads every session". Skip for one-off edits to a single context file.
---

# Env Restructure

Move bloated **always-on** context into **load-on-demand** surfaces, losing nothing. This is a
**reorganization, not a rewrite** — content moves *verbatim*. The deliverable is a migration
**plan** the user reviews; once approved it can be executed here or pasted into a fresh session.

## How it behaves (read this first)

- **Conversational & minimal.** Present each finding with a recommendation; the human agrees or
  corrects. **Never** use the AskUserQuestion tool or multiple-choice. **Ask few, simple,
  goal-oriented questions** — extract the goal and the scope, infer the rest from the repo.
- **Lead with goal & scope.** Two answers drive everything: *what does the human want*, and
  *whole repo or one service/subpackage?* If a single area, restrict audit, categorize, and plan to
  that subtree (touch root only when a block must move up).
- **Adaptive depth.** Size the job, then do the *minimum adequate* number of steps. A simple repo is
  ~2 exchanges; never drag it through the full process.
- **Touch only context surfaces.** `CLAUDE.md`, `.claude/**`, nested `CLAUDE.md`, `docs/`,
  `settings.json`. **Never edit application or test code** — see [GUARDRAILS.md](GUARDRAILS.md).

## Step 0 — Audit & size the job

Run `scripts/claude-context-budget.sh` (bundled) to measure idle load and flag rules lacking
`paths:`; note root size, nested files, docs, skills. If the human scoped to one service/area,
measure and act **only within that subtree**. Then pick depth:

| Finding | Depth |
|---|---|
| Already lean (small root, 0 unscoped rules, low idle) | **Say so and stop.** Maybe one tweak. |
| Simple (single context / one service, modest bloat) | **~2 exchanges**: read-back+goal/scope, then categorize → plan. |
| Complex (monorepo, many unscoped rules) | **Full Phase A**, categorize chunked per area. |

## Phase A — Interview (keep it short)

1. **Read-back + goal & scope** (usually one exchange). State the inventory in ~2 lines (root size,
   unscoped rules, nested files, idle tokens). Then ask only the two questions that matter:
   - **What's your goal?** (shrink always-on, fix a specific pain, tidy one service…)
   - **Whole repo, or one service/subpackage?** Scope everything below to the answer.

   Infer the rest from the repo — safety/"sacred" rules via auto-detect, frequent workflows from
   what's there — and confirm them in Step 2 instead of asking now.
2. **Categorize (the heart).** Classify each block in scope by **nature/risk/relevance** — never by
   destination — into five buckets, and present for correction:
   `critical-invariant` · `always-relevant` · `conditionally-relevant` · `rarely-relevant/reference`
   · `questionable/cut-candidate`. Surface auto-detected safety invariants (see
   [GUARDRAILS.md](GUARDRAILS.md)) so the human can veto a miscategorization. **Don't expose
   rule-vs-nested-vs-doc mechanics here.**
3. **Boundaries** (state, don't ask). Source-of-truth = default branch; target = new branch; mirrors
   (`.codex/`, `AGENTS.md`) out unless told otherwise; code untouched. Proceed unless they object.

## The gate → Phase B — Generate the plan

Compute the routing ([ROUTING.md](ROUTING.md)) and write a single self-contained plan file
([PLAN-FORMAT.md](PLAN-FORMAT.md)): **summary + risks** on top, **per-target-file work orders**
with verbatim source anchors, a **separate opt-in cuts list**, and **acceptance checks**.

Present the plan — risks first. **Proceed to execute only if the human doesn't object** (or
they paste the plan into a fresh session to run it later). Cuts are *never* applied without an
explicit green-light.

## Execution

Default to **Agent-tool subagents** (map: 1/source file · adversarial loss-verify: 1/suspected-loss
· apply: 1/**target** file so writers never collide). Offer the Workflow fast-path only if ultracode
is on. Inline for tiny (<5 files). The plan embeds these instructions so a cold session can run it.
Each work order is idempotent (skip if the target already contains the block) → resumable.

## Get the facts right

The whole skill rests on **how Claude actually loads context**. Read
[LOADING-MODEL.md](LOADING-MODEL.md) before routing — it lists the real rules *and* the phantom
features you must never propose (`globs:`/`applyTo:`, stop-hook auto-propose, `#`-append, rubric skill).
