# Plan format

The plan is the single deliverable: the human's review surface, the execution work order, and the
resume journal. Write it to **`docs/context-restructure-plan.md`** (or repo root if there's no
`docs/`). It must be **self-contained** — a fresh session that reads only this file can execute the
whole restructure. Safe to delete after the restructure lands.

## Structure (four sections, in this order)

### 1. Summary & risks (human reads this first)

- One-paragraph what/why.
- **Before → after**: idle line-count and ~token estimate (from the budget script).
- Source-of-truth ref, target branch, mirror scope.
- **⚠ Needs-a-look** list — every risky move, each one line:
  - a safety invariant being moved,
  - a `paths:` glob that matches zero real files,
  - any always-on → lazy **demotion**,
  - a block the mapper couldn't confidently route,
  - mirror (`.codex/` / `AGENTS.md`) divergence.

### 2. Per-target-file work orders (the executor reads this)

One subsection **per destination file** (grouped by destination so no two apply-agents touch the
same file). For each target, a table of blocks:

| Block | Verbatim source anchor | Notes |
|---|---|---|
| short label | `path/to/source.md:L120-L148` | e.g. "keep terse invariant in root too" |

Plus, for a new/changed rule file, the exact `paths:` frontmatter to write. **Anchors are line
ranges into the source-of-truth — the executor copies from there, never paraphrases.**

### 3. Cuts list (separate, opt-in)

Blocks judged redundant/stale. Each with source anchor + one-line reason. **Nothing here is removed
unless the human explicitly green-lights that item.** Default: leave all in place.

### 4. Acceptance checks (the executor re-runs these)

A checklist the executor verifies after applying — see below.

## Execution instructions (embed verbatim into the plan)

> **To execute this plan:** Work top-down through Section 2.
> - Default to **Agent-tool subagents**: one agent per target file (they never collide because each
>   target is one file). For the loss audit, one agent per source file; verify suspected losses with
>   a *separate* agent. Use the Workflow tool only if ultracode is on. For <5 files, do it inline.
> - **Copy each block verbatim** from its source anchor. Do not rewrite.
> - **Idempotent:** before writing a block, check whether the target already contains it; if so, skip.
>   This makes the run resumable.
> - **Touch only context files.** Never edit application or test code.
> - When all work orders are done, run Section 4 and report the results.

## Acceptance checks (Section 4 contents)

- [ ] **Completeness** — every block from the source-of-truth is present in a **loadable** surface
      (root / nested loaded on touch / `paths:`-scoped rule / followed pointer). Nothing survives
      only in `.codex/` or an un-pointed file.
- [ ] **Constraints** — root < 200 lines; every other context file < 300; every `.claude/rules/*.md`
      has a `paths:` key; every `paths:` glob matches ≥1 real file; idle token budget dropped (run
      the budget script); no banned comment tokens introduced.
- [ ] **Fidelity** — moved content is verbatim, not paraphrased; safety invariants still in root;
      zero application/test files changed (`git diff --stat` shows only context surfaces).
- [ ] **Ground-truth (optional)** — `/context` at launch and after touching one subtree matches the
      plan's expected loads.

Report **before/after** idle line counts and token estimate.
