---
name: config-bench
description: Thin conversational wrapper over the `claude-benchmark` CLI — detect Before/After from git, run an A/B benchmark of a repo's Claude environment (CLAUDE.md, .claude/rules, docs, memory) across two configs, and read the report back (Regressions, efficiency Numbers, noise). Use when the dev wants to benchmark or check their Claude environment or a CLAUDE.md restructure, asks "did my restructure regress anything", "is my new context better/cheaper", "check my Claude context", or mentions claude-benchmark. It reports and the dev decides — it never edits the environment.
---

# Benchmark a Claude environment

Drive the `claude-benchmark` binary to run the whole **Corpus** across **Before** and
**After** (one report) and read it back with the dev. The smarts — auto-detection, corpus
discovery, the spend plan — live in the **binary**, not here
([ADR-0008](../../../docs/adr/0008-one-command-evaluation-and-auto-detection.md)). You route,
confirm, and interpret. Glossary: [CONTEXT.md](../../../CONTEXT.md).

## The three jobs

1. **Detect & route.**
   - Confirm cwd is the Target git repo.
   - State the resolved **Before**/**After** so the dev can confirm: dirty tree → Before =
     `HEAD`, After = working tree; clean tree → Before = `merge-base(default-branch, HEAD)`,
     After = `HEAD`. If the dev names a baseline in chat ("vs the release branch"), pass
     `--before <ref>`.
   - Check `.benchmark/corpus/` holds ≥1 `*.yaml`. **None** → tell the dev there's no corpus
     and offer to launch the **quizgen** skill. **Several** and the dev didn't pick →
     ask which, pass `--corpus <file>`.

2. **Run with confirmation.** The binary prints a spend plan — resolved Before/After, probe
   count, total `claude -p` runs (≈ probes × samples × 2 envs). Surface it in chat, get an
   explicit yes, then invoke with `--yes`:

   ```sh
   claude-benchmark --yes            # plus any --before/--after/--corpus/--level the dev asked for
   ```

   Pass through dev requests verbatim. `--level` defaults to `l1` (no judge); pass `l2` to
   grade a corpus that needs the **Judge** (open-ended/plan/judge_rubric probes). `--samples 5`
   raises N.

3. **Interpret the report.** Read the markdown back, quality first:
   - **Regressions** — Before PASS → After FAIL, "what the restructure compromised." Lead here.
   - **New passes** — FAIL → PASS, "what improved."
   - **Numbers** — tokens, cost, tool-calls (exact); Duration (advisory under parallelism).
     This is the main goal: efficiency up with quality held.
   - **Disagreement** — if a rule's Before samples split, call the flip **noise**, not a real
     regression. Pull the side-by-side traces for any rule that flipped and explain *why*.

   The tool never gates — present the picture; the dev decides.

## Hard boundary

**Never edit the environment** — not CLAUDE.md, `.claude/rules/*`, docs, or memory. You may
*suggest* concrete changes in prose, but applying them ("automated restructuring driven by
results") is out of scope (PRD) until the tool is trusted. Read-only, advisory.

## Chain

- No corpus, or the dev wants new probes → **quizgen** (authors the Corpus from
  Before; human-reviewed and frozen). A benchmark needs a corpus to exist first.
