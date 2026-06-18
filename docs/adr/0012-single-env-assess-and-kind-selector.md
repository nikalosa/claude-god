# Single-environment `assess` and the `--kind` probe selector

**Status:** accepted (extends ADR-0008's CLI/UX)

Two independent additions to the CLI.

**`--kind` selector.** A CSV flag over the three probe kinds (`rule_based`,
`open_ended`, `plan`), default all, on every corpus-running mode (bare, `run`,
`calibrate`, `assess`). It subsets the **Corpus** right after load; an empty
selection errors. It carries no conceptual weight — it subsets which probes run,
it does not change grading — so the dev can run "just the rules" or "just the
design probes" without editing the corpus. The judge is built from the
*filtered* set, so `--kind rule_based` on a judge-needing corpus runs without the
Judge when the surviving rules carry no `judge_rubric` (no `--judge` needed; the
flag was `--level l1` before [ADR-0013](0013-judge-flag-replaces-level.md)).

**`assess` — score one Environment, no A/B.** Runs the corpus against a single
ref (default: the working tree, temp-committed when dirty — the same "current
config" ADR-0008 calls After) and prints an absolute scorecard. It exists for the
dev-ask *"assess current config with this corpus"* — there is no second config to
prefer against, so the report is a flat per-rule PASS/FAIL plus single-env
**Numbers** with **no Δ column**. The three comparison-shaped asks (this-vs-that,
dirty HEAD-vs-tree, clean merge-base-vs-HEAD) stay on the A/B path (bare/`run`).

**Grading reuse.** `rule_based` grading is already absolute (`dsl.Grade` →
per-rule PASS/FAIL); `runner.GradeProbe` called with an empty After side grades
the one env's rules and skips the **Preference comparison** (it needs two
answers). So single-env grading is the existing path with one side empty — no new
grader. `aggregator.ComputeDeltas`' Before columns give the single-env Numbers
sum directly.

**Comparative probes single-env: Numbers only, not graded.** `open_ended`/`plan`
probes are graded *comparatively* — one answer has nothing to prefer against — so
`assess` runs them for their **Numbers** (base/peak context, cost, tokens are a
real single-env signal) but emits no pass/fail for them, listing them as
*not graded (needs A/B)*. Because no **Preference comparison** runs, `assess`
needs the **Judge** only for `judge_rubric` rules, never for comparative probes
(`dsl.NeedsRubricJudge`, distinct from `NeedsJudge`).

## Considered Options

- **Reuse `runBenchmark(ref, ref)` and render one side** (as `calibrate` does for
  its noise floor). Rejected: that runs every probe **twice** — single-env's whole
  point is half the spend. `assess` dispatches one env only.
- **Absolutely score comparative probes against a rubric (option B).** Deferred.
  It needs a corpus-schema change — today the loader *forbids* rules/facts on
  `open_ended`/`plan` probes — so it is a corpus redesign, not a render addition.
  `JudgeRubric` already scores one answer absolutely, so the judge mechanism
  exists; what is missing is per-probe expected facts + a threshold on those
  kinds. Punted until a dev actually wants single-env grades for design/plan
  probes; `assess` ships useful now for the common rule-based ask.
- **Make `assess` rule-based only (auto-drop comparative).** Rejected: their
  single-env Numbers (especially turn-1 base context, the config's clean signal)
  are worth surfacing; silently dropping them hides cost. `--kind rule_based`
  remains the explicit way to exclude them.

## Consequences

- New `assess` command (ergonomic like bare: auto-discovers the corpus, resolves
  the ref, prints a spend plan, confirms, `--yes` to skip). `run`/`snapshot`/
  `calibrate` are unchanged.
- `autodetect.ResolveOne` resolves a single ref (override, else working tree),
  mirroring the After half of `Resolve` without its A/B "nothing to compare" error.
- `report.RenderAssessment` renders the single-env shape: scorecard (Probe · Rule ·
  Severity · Result), single-env Numbers (no Δ), per-probe Numbers (no Δ), and the
  not-graded comparative list.
- `--kind` is wired into all four corpus-running modes; `snapshot` (no corpus) is
  untouched.
