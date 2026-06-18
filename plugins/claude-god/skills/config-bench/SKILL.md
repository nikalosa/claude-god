---
name: config-bench
description: Thin conversational wrapper over the `claude-benchmark` CLI — detect Before/After from git, run an A/B benchmark of a repo's Claude environment (CLAUDE.md, .claude/rules, docs, memory) across two configs, and read the report back (Regressions, efficiency Numbers, noise). Use when the dev wants to benchmark or check their Claude environment or a CLAUDE.md restructure, asks "did my restructure regress anything", "is my new context better/cheaper", "check my Claude context", or mentions claude-benchmark — or to **assess** one config absolutely against the corpus when there's no baseline ("score/assess my current config", "how does my context do on this corpus"), which routes to the `assess` command. It reports and the dev decides — it never edits the environment.
---

# Benchmark a Claude environment

Drive the `claude-benchmark` binary to run the whole **Corpus** across **Before** and
**After** (one report) and read it back with the dev. The smarts — auto-detection, corpus
discovery, the spend plan — live in the **binary**, not here
([ADR-0008](https://github.com/nikalosa/claude-god/blob/main/docs/adr/0008-one-command-evaluation-and-auto-detection.md)). You route,
confirm, and interpret. Glossary: [CONTEXT.md](https://github.com/nikalosa/claude-god/blob/main/CONTEXT.md).

## The three jobs

1. **Detect & route.**
   - Confirm cwd is the Target git repo.
   - **A/B or single-env?** Two configs to compare (a restructure, before vs after, vs a
     branch) → the A/B benchmark in jobs 2–3. One config with **no baseline**
     ("assess/score my current config") → **`assess`** (see *Assess one config*). Default to
     A/B; only route to assess when there's genuinely nothing to compare against.
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
   claude-benchmark --yes            # plus any --before/--after/--corpus/--judge/--kind the dev asked for
   ```

   Pass through dev requests verbatim. `--judge` is **off by default**; pass it to grade a
   corpus that needs the **Judge** (open-ended/plan/judge_rubric probes — it adds `claude -p`
   calls, real spend). `--samples 5` raises N. `--kind` (CSV of `rule_based,open_ended,plan`,
   default all) narrows which probe kinds run.

3. **Interpret the report.** Read the markdown back, quality first:
   - **Regressions** — Before PASS → After FAIL, "what the restructure compromised." Lead here.
   - **New passes** — FAIL → PASS, "what improved."
   - **Numbers** — tokens, cost, tool-calls (exact); Duration (advisory under parallelism).
     This is the main goal: efficiency up with quality held.
   - **Disagreement** — if a rule's Before samples split, call the flip **noise**, not a real
     regression. Pull the side-by-side traces for any rule that flipped and explain *why*.

   The tool never gates — present the picture; the dev decides.

## Assess one config (no A/B)

When there's no baseline to compare, `assess` scores one **Environment** against the corpus:

```sh
claude-benchmark assess --yes      # plus --ref/--corpus/--judge/--kind as asked
```

- **Ref.** Defaults to the current config (dirty tree → working tree; else `HEAD`);
  `--ref <branch|sha>` scores another. No Before/After detection — just one env.
- **Plan.** Runs = probes × samples (one env, **not** ×2). Surface the spend plan and get an
  explicit yes, same as the A/B path.
- **Report.** A flat **scorecard** — each rule PASS/FAIL on its own, plus single-env
  **Numbers** with no Δ column. **Open-ended/plan probes have no single-env grade** (a
  Preference comparison needs two answers): they run for Numbers and are listed *not graded*.
  To grade those, run the A/B benchmark. Skip them up front with `--kind rule_based`.
- `--judge` only if the corpus carries judge_rubric rules.

## Hard boundary

**Never edit the environment** — not CLAUDE.md, `.claude/rules/*`, docs, or memory. You may
*suggest* concrete changes in prose, but applying them ("automated restructuring driven by
results") is out of scope (PRD) until the tool is trusted. Read-only, advisory.

## Chain

- No corpus, or the dev wants new probes → **quizgen** (authors the Corpus from
  Before; human-reviewed and frozen). A benchmark needs a corpus to exist first.
