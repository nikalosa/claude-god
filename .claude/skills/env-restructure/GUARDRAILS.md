# Guardrails & pitfalls

These are load-bearing. Violating them is how a "slim" silently breaks a repo.

## Guardrails (never break)

1. **Reorganization, not rewrite.** Move content **verbatim** (byte-for-byte). Don't paraphrase,
   "improve", or summarize while moving. Anything that *looks* redundant goes to the **cuts list**,
   never silently deleted. **When unsure, keep it.**
2. **Code is untouched.** Only edit context surfaces: `CLAUDE.md`, `.claude/**`, nested `CLAUDE.md`,
   `docs/`, `.claude/settings.json`. If moving content breaks an in-source reference (code or a doc
   points at a path you're changing), fix it with a **lazy redirect-rule stub** (a tiny path-scoped
   rule pointing to the new home), **not** by editing the code.
3. **Never demote a production-safety invariant out of always-on root.** Auto-detect candidates by:
   - markers — `CRITICAL:` / `SECURITY:` / `INVARIANT:` / `WHY:` framing,
   - phrasing — "always" / "never" / "must",
   - keywords — money/decimal/currency, idempotency, auth/token/secret, transaction/atomic,
     FK/consistency, migration/schema.
   Surface the detected set in the categorize step so the human can correct it. A demotion of any
   of these to a lazy rule or a skill is the worst failure mode — it stops loading and nothing errors.
4. **Enforce size limits** (root < 200, others < 300) and **re-check them in acceptance**.

## Pitfalls (learned the hard way)

- **Lossy slim — THE big risk.** A naive slim silently drops blocks (a first attempt on the source
  repo dropped **33**). This is exactly why the pipeline has an **audit + adversarial loss-verify**
  step: one agent maps each block to its new home and flags `MISSING`; a *separate* agent re-greps
  each suspected loss to confirm true-absence vs false-alarm. **Never skip it.**
- **Silent non-load.** A `paths:` glob that matches no real file = a rule that never loads, no error.
  Verify every glob against actual files before finalizing.
- **Stale base.** Diff and measure against the **branch base** (the commit you branched from), not
  the moving default branch — otherwise you'll see hundreds of unrelated code files.
- **Mirror drift.** `.codex/`, `AGENTS.md`, and other parallel agent surfaces diverge. Decide in the
  scope step whether they're in scope; if out, note the divergence in the plan's risks.
- **Subagent path mishaps.** A delegated writer can land a file in the wrong checkout/worktree.
  Verify the tree after any fan-out apply.
- **Content surviving only in a non-loaded file is still a LOSS.** If a block ends up only in
  `.codex/`, an un-pointed `docs/`, or a `CATALOG.md` that nothing loads, Claude can't see it.
  **Completeness must be measured against the *loadable* surface only.**

## Source of truth

"Preserve everything" is measured against the **canonical source** (the default branch / branch
base), not a local draft. Establish this in the scope step before mapping, so the corpus you're
preserving is the real one the team wrote.
