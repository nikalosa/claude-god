---
name: config-bench
description: Benchmark a Claude Code environment with the `claude-benchmark` CLI — run a repo's probe Corpus across two configs (Before/After) and report which is better on quality and efficiency, or score one config with no baseline. It reports, never edits. Use when the dev wants to know whether a change to CLAUDE.md, rules, MCP, skills, or docs helps or regresses, compare two configs (before vs after), score one config with no baseline, or mentions claude-benchmark.
---

# Benchmark a Claude environment

You **translate** the dev's request into one `claude-benchmark` run and **read the result back**. The smarts — auto-detecting Before/After, finding the corpus, the spend plan, the cache — live in the **binary**; you map words to flags, confirm, and judge the report. Glossary: [CONTEXT.md](https://github.com/nikalosa/claude-god/blob/main/CONTEXT.md).

Read-only: you may *suggest* changes in prose, but **never edit** the environment — not CLAUDE.md, rules, docs, MCP, or memory.

## Translate the request

Pull the parameters the dev actually gave; pass only those flags and let the binary default the rest. Pick the command:

| The dev wants… | Command |
|---|---|
| "check my changes / is my new env better" (A/B) | `claude-benchmark` |
| "compare to `<ref>`" ("vs main", "since I branched") | `claude-benchmark --before <ref>` |
| test an MCP config without committing it | `claude-benchmark run --before <ref> --after <ref> --after-mcp <cfg>` |
| "only my current env / only after" (no baseline) | `claude-benchmark assess` |
| "only the baseline / only before" | `claude-benchmark assess --ref <ref>` |

Add a flag only when the dev's words call for it:

| The dev says… | Flag |
|---|---|
| "ignore the cache / rerun fresh" | `--no-cache` |
| "only rule_based / plan / open_ended" (any combo) | `--kind <csv>` |
| "run it N times / be more sure" | `--samples N` |
| corpus has open_ended/plan/judge_rubric probes | `--judge` (auto-add; it adds runs) |

`claude-benchmark` (bare) auto-detects Before/After from git: uncommitted edits → Before = `HEAD`, After = the working tree; clean tree → Before = `merge-base(default, HEAD)`, After = `HEAD`. `run` does **not** auto-detect — when you use it (the MCP overlay), pass the same Before/After bare would have resolved.

## Run it

`claude-benchmark` not found ("command not found") → chain to **install-cli** first, then resume here.

1. **Corpus.** Look in `.benchmark/corpus/` for `*.yaml`. None → tell the dev and offer to launch **quizgen**; wait. One → use it. Several → ask which, pass `--corpus <file>`. Dev named one → `--corpus`.
2. **Plan (free).** Run the command **without `--yes`** — the binary prints the resolved Before/After and `N cached · M to run`, then stops without spending. Show it, and state the guessed Before in plain words so a wrong guess gets caught.
3. **Run.** On the dev's go-ahead, re-run with `--yes`. Skip the confirmation if the dev already said "just run it."

## Read it back

**A/B — give a verdict, grounded in the report's numbers:**
- Lead with the call: which env wins, or "don't adopt." A **critical Regression vetoes any efficiency win** — never recommend an env that drops a critical guardrail, however much it saves.
- **Quality** first: Regressions (Before PASS → After FAIL), then new passes.
- **Efficiency**: input/output tokens, context window (base), time, tool calls — lower is better. No dollar cost.
- A lone flipped rule on a 1-sample run may be noise; if it's critical or the dev doubts it, offer `--samples 3` or `--no-cache` to recheck.

**Single env (`assess`) — no winner:** list failing rules (critical first) and the Numbers. open_ended/plan come back "not graded" — say so, and nudge: for "did my change help?", run the A/B.

## Chain

No corpus, or the dev wants new probes → **quizgen** authors one from Before (human-reviewed, frozen). A benchmark needs a corpus first.
