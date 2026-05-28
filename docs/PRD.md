# claude-validator — PRD

**Status:** Design locked, pre-implementation.
**Target home:** Sibling OSS repo (`~/Desktop/claude-god/`). This document is drafted in PAM (`~/Desktop/Velitech/pam/`) only as a temporary holding location; the actual codebase will live outside PAM.
**Target consumer (v1):** the PAM repo's CLAUDE.md + `.claude/rules/*` + project memory restructure effort.

---

## Problem Statement

PAM's project context for Claude Code has grown to ~700 lines in `CLAUDE.md` plus ~15 rules files in `.claude/rules/*` plus auto-memory plus inline docs. The result is that critical, often single-line rules (monetary-as-string, use the migration script, no FK between CQRS read models, brand resolution priority, etc.) compete for the model's attention against ~5,000 lines of supporting documentation. Practical symptoms the developer observes:

- Claude Code's responses are bloated, hedging, and over-explanatory.
- Critical buried rules are sometimes silently ignored during real work, despite being "present" in context.
- Token cost per session is high, and most of that cost provides marginal value.

The developer wants to restructure: split `CLAUDE.md` into nested per-area files, move documentation-shaped content into `docs/`, prune memory, and shrink the always-loaded context. The blocker is that **there is no way to know whether a given restructure batch actually improved things, or quietly removed a rule Claude was relying on**. Token reduction alone is a misleading proxy — it's trivially "improved" by deleting critical rules, which is strictly worse, not better.

The developer needs a tool that answers, for any pair of context configurations (before vs after a restructure batch):

1. **Behavioral fidelity** — does Claude still recall and apply each critical and buried rule?
2. **Token / cost / time delta** — how much cheaper and faster is the new configuration?
3. **Qualitative shape** — are Claude's outputs tighter, more direct, less hedged?

— with results that are reproducible, gateable in CI, and not gamed by single-run stochasticity.

## Solution

A standalone tool, `claude-validator`, that runs an A/B benchmark of two Claude Code context configurations against a curated corpus of probes, captures structured execution data per run, grades rule-by-rule pass/fail, and emits a comparative report.

Core mechanics:

- **Tiered evaluators (L1 → L4)** sharing a single harness. Each tier raises the difficulty of the probe and the fidelity of the signal, but also the cost. The validator fails fast at cheap tiers before spending money on expensive ones.
- **Branch-pinned baselines.** The "before" and "after" context configurations are captured as git branches in the target repo (`validator/before`, `validator/after`). Each run spawns a fresh git worktree off the appropriate branch, swaps in the corresponding project-memory snapshot, runs Claude Code headlessly inside, captures structured output, then cleans up. This makes every benchmark reproducible months later.
- **Pattern-first grading.** Rule checks are expressed as a small YAML predicate DSL operating over the captured transcript and git diff. Judge-LLM grading is used as an escape hatch only for genuinely open-ended probes (architectural Q&A, plan-vs-plan diffs).
- **N=3 adaptive sampling.** Every probe runs three times per environment. Median is reported; runs that disagree on a critical rule trigger adaptive expansion to N=5. This is the noise floor every comparison stands on.
- **Tiered gate.** A regression on any rule tagged `severity: critical` exits non-zero (blocks the restructure batch). Non-critical regressions and improvements are reported but do not gate.

The validator is built generic from day one: harness is repo-agnostic, the corpus is per-project configuration. The PAM consumer of the tool keeps its tasks/rules/patterns under `pam/.validator/`. The OSS tool itself ships an example corpus only.

## User Stories

### Authoring & calibration

1. As a tool author, I want to express each task probe in a single YAML file (prompt, expected rules, predicate checks), so that adding a probe takes minutes and is easy to review.
2. As a tool author, I want a small predicate DSL (`bash_call_matches`, `diff_added_regex`, `wrote_file_matching`, `not(...)`, `and(...)`, `unless_path_matches`), so that I can express 95% of checks without writing code.
3. As a tool author, I want an escape hatch to register a function-based check for unusual rules, so that I am not blocked by DSL gaps for the long tail.
4. As a tool author, I want to tag each rule with a severity (`critical`, `high`, `medium`), so that the gate logic can treat them differently.
5. As a tool author, I want a calibration command that runs `before vs before` once, so that I see the validator's noise floor on my actual corpus and can tighten or drop flaky checks before the corpus is locked.
6. As a tool author, I want each rule probed at multiple tiers when applicable (the same rule appears as an L1 question, an L2 design question, an L3 plan step, and an L4 implementation check), so that the report tells me at which level a rule breaks down — recall vs design knowledge vs strategic application vs in-execution attention.

### Running the validator

7. As a developer mid-restructure, I want to snapshot the current context as `validator/before` with one command, so that I have an immutable baseline before I start editing.
8. As a developer mid-restructure, I want to run the validator with `--level l1,l2,l3` to get a fast read on each restructure batch, deferring the expensive L4 to weekly runs.
9. As a developer, I want each Claude Code session in a benchmark to run in an isolated git worktree, so that runs cannot contaminate each other or my real working tree.
10. As a developer, I want project memory snapshotted into the branch by default, so that user-level memory drift does not pollute the A/B comparison.
11. As a developer, I want a `--no-memory-snapshot` flag, so that I can opt out of memory pinning when I know it doesn't matter or want to test memory-agnostic behavior.
12. As a developer, I want runs to bypass permission prompts and run fully headless via `claude -p --output-format stream-json`, so that 60 runs per A/B do not require human babysitting.
13. As a developer, I want tasks designed and an environment that avoids real-infrastructure side effects (no actual docker/postgres mutation), so that benchmarks are safe to re-run repeatedly without corrupting dev infrastructure.
14. As a developer, I want N=3 runs per probe with adaptive expansion to N=5 on disagreement, so that single-run stochasticity does not produce false signal.

### Grading & reporting

15. As a developer, I want a markdown report listing critical regressions at the top in red, so that I see deal-breakers immediately.
16. As a developer, I want a report section showing rules that newly pass (improvements), so that I see where the restructure actually paid off.
17. As a developer, I want per-task and total deltas for total cost (`total_cost_usd`), tokens (input including cached + output), and wall-clock time, so that I can quantify "what we saved."
18. As a developer, I want every tool invocation enumerated with its associated token count, including invocations made by sub-agents the model spawns, so that I see exactly where context budget is spent.
19. As a developer, I want a rule × environment matrix (every rule × 3 runs × 2 envs) in PASS/FAIL grid form, so that I can spot flakiness vs real regression at a glance.
20. As a developer, I want side-by-side tool-call traces for any rule that flipped between environments, so that I can eyeball the actual behavior change and confirm it's real.
21. As a developer, I want JSON report output as an option, so that I can pipe results into other tooling or CI dashboards.
22. As a developer, I want the validator to exit non-zero on critical-rule regression, so that I can wire it into a pre-commit or CI hook for the restructure branch.

### Tier-specific behavior

23. As a developer, I want L1 (recall) to run direct questions (~30 prompts) and grade by regex over the final assistant text, so that I get a cheap sanity check that runs in seconds and fast-fails if a critical rule has been deleted.
24. As a developer, I want L2 (design) to probe both narrow ("how are amounts typed") and broad architectural questions ("trace bet placement from API to ledger entry; name services, schemas, gRPC calls, events"), so that I measure project-wide knowledge fidelity, not just isolated rule recall.
25. As a developer, I want L2 complex probes graded by a judge LLM against an explicit rubric (a list of facts the answer must mention), so that open-ended architectural answers can still be graded deterministically-enough at scale.
26. As a developer, I want L3 (plan) probes to have Claude generate a plan only — no implementation — and grade the plan via predicates plus an optional judge-LLM plan-vs-plan diff, so that I see whether a restructure is making Claude's planning tighter or more bloated.
27. As a developer, I want L4 (implementation) probes to reuse the L3 task prompts but executed, graded by the same predicate DSL operating on the full transcript and git diff, so that L3 and L4 form a matched pair revealing "knows what to do but loses focus during execution" failures.
28. As a developer, I want L4 to be opt-in or scheduled weekly rather than run on every batch, so that the cost of the most expensive tier is amortized over restructure work.

### Repository structure & adoption

29. As an OSS author, I want the validator code to live in a sibling repo, separate from any project being validated, so that the tool's own development is not contaminated by the very bloat it is designed to detect.
30. As an OSS author, I want the validator CLI to take `--target <repo>` and `--corpus <dir>` arguments, so that the validator is fully repo-agnostic and can be adopted by any Claude Code project without modification.
31. As an OSS author, I want the validator's own CLAUDE.md to be lean (~50 lines) and self-validating, so that the repo is its own demonstration of the methodology it advocates.
32. As a PAM developer, I want PAM to host only the corpus under `pam/.validator/corpus/*.yaml` and `pam/.validator/config.yaml`, so that PAM is a consumer of the tool, not a host, and the boundary stays clean.
33. As a future contributor, I want a sample corpus shipped with the validator (illustrative rules and tasks), so that I can adopt the tool against my own project without writing the full corpus from scratch.

### Future / extensibility (out of v1, hinted at)

34. As an OSS author, I want hooks for skills that auto-generate L1–L4 probes from a target project's CLAUDE.md + rules, so that adopting projects do not need to hand-curate their entire corpus.
35. As an OSS author, I want hooks for skills that propose CLAUDE.md restructure batches and run the validator to grade them, so that the restructure step itself can become semi-automated.

## Implementation Decisions

### Metric hierarchy

- **Primary metric: behavioral fidelity.** For each rule in the corpus, does Claude recall it (L1), explain it (L2), include it in a plan (L3), and apply it in implementation (L4)? Token reduction alone is rejected as primary because it is trivially gamed by deleting critical rules.
- **Secondary: cost + tokens + time.** Reported as median per task and totals, with input (including cached) and output broken out, wall-clock per turn, total `total_cost_usd`.
- **Tertiary: qualitative shape.** Out of scope for v1 grading; report shows a few tool-call traces for human inspection.

### Test mechanism — tiered evaluators

The validator runs four tiers, in increasing order of cost and signal fidelity. All tiers share one harness; only the prompt template and grader vary.

- **L1 — Recall.** ~30 direct one-line questions probing specific rules. Graded by regex/string match over Claude's final text. Runs in seconds per probe. Acts as a **fast-fail gate**: if a critical rule fails L1 in `after`, the validator can stop without running L2+.
- **L2 — Architectural / design Q&A.** ~15 probes ranging from narrow ("how are amounts typed") to broad ("trace a bet placement from API to ledger entry — services, schemas, gRPC calls, events"). Mixed grading: simple probes use the predicate DSL, complex probes use a judge LLM scoring against an explicit fact-rubric.
- **L3 — Plan only.** ~10 task prompts where Claude is asked to *plan* (not implement) a change. Plans are graded by predicate DSL ("plan mentions migration script") and by a judge-LLM **plan-vs-plan diff** comparing `before` and `after` plans for the same task (what steps were lost, what was gained, was the after-plan tighter or more verbose).
- **L4 — Full implementation.** Same task prompts as L3, but Claude *executes* with full tool access in a sandboxed worktree. Graded by predicate DSL operating over the tool-call log and the git diff produced. This is the most expensive tier and is opt-in / scheduled, not run on every batch.

The same rule may be probed across multiple tiers. The cross-tier pass/fail profile is the report's most actionable diagnostic.

### Grading model — pattern-first hybrid

- Predicate DSL (YAML) is the default. Primitives include `bash_call_matches`, `wrote_file_matching`, `diff_added_regex`, `diff_added_contains`, `transcript_contains`, `not(...)`, `and(...)`, `or(...)`, `unless_path_matches`.
- Judge LLM is the escape hatch for: L2 complex architectural probes (rubric-scored), L3 plan-vs-plan diffs, and any rule that is genuinely open-ended.
- Predicate-only checks for everything else, deliberately, to keep grading deterministic and the A/B comparison free of judge-LLM run-to-run noise.
- Each rule entry in YAML carries: `id`, `severity` (`critical | high | medium`), and one or more `checks` (each a predicate expression).

### Stochasticity handling

- N=3 runs per probe per environment. Median over runs for cost/tokens/time. Majority vote (≥2/3) for rule pass/fail.
- Adaptive: if a critical rule's runs disagree at N=3, the harness expands that probe to N=5 automatically. Non-critical disagreement is reported as flakiness, not expanded.
- Same-prompt repeats. Variance across runs is the signal being measured (it tells you the noise floor).

### Isolation & infrastructure side effects

- One git worktree per run, off `validator/before` or `validator/after`. Worktrees share git blobs (fast) and isolate working trees (safe). `git worktree remove --force` after grading.
- Two long-lived branches in the target repo: `validator/before` (frozen pre-restructure state, immutable baseline), `validator/after` (the restructure state under test). Historical baselines may be archived as `validator/baseline-<date>` branches.
- Project memory (`~/.claude/projects/<project-slug>/memory/`) is **snapshotted into the branch** by default. The harness `cp -r`'s the snapshot into the canonical memory location before each run and restores afterwards. A `--no-memory-snapshot` flag opts out.
- **Real infrastructure execution is forbidden by corpus design.** Tasks are written so they do not need to actually run docker, hit postgres, or push to remote services. Grading targets *intent* (the tool call was attempted) rather than *outcome* (it succeeded). This makes runs safe, fast, and parallelizable. If a task genuinely requires live infra, it is out of scope for v1.

### Headless invocation

- Each run: `claude -p "<task_prompt>" --output-format stream-json --permission-mode bypassPermissions`. Stream-JSON is the gold capture mode — it emits every tool call, message, and `usage` event as JSONL. Bypass-permissions is required so headless runs do not deadlock on permission prompts.
- No preamble is injected into the task prompt. Whatever instructions Claude needs come from the target repo's CLAUDE.md, rules, and memory — which is what the validator is testing.
- Post-run, the harness runs `git add -A && git diff --cached > diff.patch && git diff --cached --stat > diff.stat && git reset` against the worktree to capture filesystem mutations as an independent artifact alongside the tool-call log.
- The stream-JSON parser walks `Agent` (sub-agent) tool calls recursively, attributing each child session's `usage` events back up to the parent `Agent(...)` call, so the per-tool-call token report includes work done inside spawned sub-agents.

### Corpus scope (v1)

- Rules covered: **critical ∪ buried 1-liners** (~15–20 rules). "Critical" = money, security, data integrity, regulatory. "Buried" = single-line rules deep in a long file, most at risk from attention failure.
- Tasks: ~10–12 probes shared across L3 and L4; ~15 probes for L2; ~30 probes for L1.
- Soft style rules (terseness, no over-explanation, no premature abstraction) are explicitly **deferred to v2.** They are real wins, but they are noisier to grade and the system prompt already biases toward terseness, so the marginal signal is unclear. Defer until v1 grading is proven stable.

### Gate behavior

- Tiered. Critical-rule regression (majority-pass → majority-fail across N runs) → validator exits non-zero, blocking the restructure batch.
- Non-critical regressions and all improvements → report-only, no gate. Developer decides whether the cost/benefit is acceptable.
- L1 fast-fail: a critical-rule failure at L1 short-circuits the run before L2+ start, saving cost.

### Repository structure

- `claude-validator` lives in its own sibling repo (`~/Desktop/Velitech/claude-validator/`), separate from any consumer project. OSS-ready from day one. No "extract later" debt.
- Consumer projects (PAM, others) host only their own corpus and config, under `<repo>/.validator/`. CLI accepts `--target <repo>` and `--corpus <repo>/.validator/corpus` to keep the boundary explicit.
- The validator's own `CLAUDE.md` is intentionally lean (~50 lines) and is self-validated by the tool against its own corpus, as a methodology demonstration.

### Module sketch

- **Harness** — spawns worktrees, manages memory snapshot swap, invokes `claude -p`, captures stream-JSON + git diff, cleans up. Repo-agnostic.
- **Stream-JSON parser** — converts the raw event stream into a structured "run record": ordered tool calls (with per-call token attribution including sub-agent recursion), file mutations, per-turn timing, total cost.
- **Predicate DSL evaluator** — loads YAML, evaluates predicates against a run record, returns per-rule PASS/FAIL.
- **Judge LLM adapter** — wraps Anthropic API calls for L2 rubric scoring and L3 plan-vs-plan diff grading. Isolated module so the rest of the system stays deterministic.
- **Aggregator** — combines N=3 (or N=5 adaptive) run records per probe into a single result with median statistics and majority-vote rule outcomes.
- **Report renderer** — markdown (default) and JSON (`--json`) outputs. Critical regressions first, new passes second, then quantitative deltas, then matrix, then trace excerpts.
- **CLI** — `claude-validator snapshot`, `claude-validator calibrate`, `claude-validator run --level l1,l2,l3 --target <path> --corpus <path>`.

Most of these are **deep modules** (encapsulate substantial behavior behind a small interface). The harness, the parser, the DSL evaluator, and the aggregator are independently testable without touching Claude at all (replay fixtures of stream-JSON + git-diff captures suffice for tests).

## Testing Decisions

A good test for this project verifies **observable behavior** of the harness/parser/grader given a fixed input, never the implementation details (file paths chosen, internal data structure shapes, helper function decomposition). The validator's whole credibility rests on its determinism, so its own tests must be deterministic.

Modules to test:

- **Stream-JSON parser** — golden-file tests. Capture real stream-JSON fixtures from `claude -p` runs (recorded once, checked into the validator repo), feed them through the parser, assert the resulting run record matches a snapshot. Cover: simple flat runs, runs with sub-agent recursion, runs with errors, runs that hit `usage` updates mid-stream. No live Claude calls in tests.
- **Predicate DSL evaluator** — pure unit tests. Hand-crafted run records as input, hand-crafted predicate expressions, assert PASS/FAIL outcomes. Covers every primitive (`bash_call_matches`, `diff_added_regex`, `not`, `and`, `or`, `unless_path_matches`) and a few realistic compositions taken straight from the PAM corpus.
- **Aggregator** — pure unit tests over synthetic run records. Cover: N=3 all agree, N=3 disagree on critical rule (triggers expansion), N=3 disagree on non-critical (reports flaky, no expansion), median calculation under odd vs even counts, majority-vote tie behavior.
- **Report renderer** — snapshot tests on synthetic aggregated results. Confirm critical regressions appear at the top, new passes appear distinctly, the matrix renders correctly, the JSON output schema is stable.
- **Harness** — integration test against a tiny throwaway target repo (committed in `examples/`) with a known-good corpus and a known-good `claude -p` recording. Verifies worktree lifecycle, memory snapshot swap, cleanup. May be slow; gated behind an explicit env var so CI can run it less often than unit tests.

Prior art: this test shape (golden fixtures + pure unit tests + an integration smoke) is the standard pattern for any deterministic transform pipeline. The fact that the validator's runtime *consumes* an LLM does not mean its tests need to.

The validator also has a unique form of self-test: **dogfooding against its own CLAUDE.md.** Once L1 is built, run it against the validator's own (~50-line) `CLAUDE.md`. All checks should pass, demonstrating that the methodology applies to itself.

## Out of Scope

- **Soft style rules in v1 corpus** (terseness, no over-explanation, no premature abstraction). Deferred to v2 once grading infrastructure is stable.
- **Real infrastructure execution during runs.** Tasks are designed to never need live docker/postgres/network. The grader checks *intent* (tool call attempted), not *outcome* (it succeeded).
- **Live grading of model output quality beyond rule-application.** No "is the code better" judgments. Only "did Claude follow the rule."
- **Automated corpus generation from a target repo's CLAUDE.md.** Future skill; v1 is hand-curated, deliberately, because LLM-generated corpora are blind to the same buried rules a bloated CLAUDE.md hides from the model under test (self-blind).
- **Automated CLAUDE.md restructuring driven by validator results.** Future skill once the validator is trusted.
- **Validating non-Claude-Code agents** (Cursor, Cline, Aider, etc.). Architecturally possible later but not the v1 target.
- **CI dashboard / web UI.** Markdown + JSON output only. Any UI is downstream tooling.

## Further Notes

### Build sequence (recommended)

1. Initialise the sibling repo (`~/Desktop/Velitech/claude-validator/`). Write its lean ~50-line CLAUDE.md from the start.
2. Snapshot today's PAM context as `validator/before` in the PAM repo. Immutable baseline.
3. Build the harness end-to-end with **L1 only**: worktree spawning + memory snapshot swap + `claude -p` invocation + stream-JSON parser + simple regex grader + markdown report. Get one round-trip working before adding tiers.
4. Author ~30 L1 recall questions for the PAM corpus. Calibrate `before vs before`. Drop or tighten any flaky checks.
5. Add L2: same harness, add judge-LLM adapter for complex rubric questions, author ~15 L2 probes (mix of narrow + broad architectural). Recalibrate.
6. Add L3: same harness, new prompt template (plan-only), add judge-LLM plan-vs-plan diff, author ~10 plan probes.
7. (Now the developer can start restructuring CLAUDE.md and iterate with L1+L2+L3 signal after every batch.)
8. Add L4 last: full execution, predicate grading over transcript + git diff, reuse L3 task prompts. Schedule weekly or per-major-batch, not per-batch.

### Why sibling repo from day one (not "extract later")

PAM's bloated CLAUDE.md is the *exact* problem the validator is built to detect. Developing the validator *inside* PAM means every dev session is degraded by the problem the tool is fixing — eating your own bad cooking. A 30-second `mkdir` avoids: weeks of degraded development sessions, an eventual extraction PR with scrubbed history, and the awkwardness of a generic OSS tool with a private monorepo birthplace.

Nested `CLAUDE.md` does not solve this — Claude Code loads the project-root file regardless of working subdir; nested files *add* to context rather than override it. Working under `services/claude-god/` still loads PAM's full 700 lines.

### Eventual scope beyond v1

The developer wants this to grow into a broader Claude Code project hygiene toolkit: skills that auto-generate L1–L4 corpora from a target repo, skills that propose CLAUDE.md restructure batches and grade them automatically, possibly a registry of community corpora for popular framework conventions. These are explicitly v2+ and depend entirely on v1 being trusted first. v1 is the foundation; do not preempt it.

### Note on this document's location

This PRD is being written in `services/claude-god/` purely as a temporary holding location while the developer prepares the sibling repo. Once the sibling repo is created, this document moves there (likely as `PRD.md` at the repo root). The `services/claude-god/` directory in PAM should be deleted after the move — its presence in `services/` is architecturally wrong (PAM is the *target* of the validator, not its *host*).
