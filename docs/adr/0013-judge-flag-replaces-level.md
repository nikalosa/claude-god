# `--judge` boolean replaces `--level l1/l2`

**Status:** accepted (supersedes the `--level` flag in [ADR-0008](0008-one-command-evaluation-and-auto-detection.md) and [ADR-0012](0012-single-env-assess-and-kind-selector.md); the L1–L4 tier vocabulary was already retired — see [CONTEXT.md](../../CONTEXT.md))

The `--level l1/l2` flag is renamed to a boolean `--judge` (default **false**). The
tier system (L1–L4) that `--level` once selected is retired in the domain model;
the flag survived only as a judge on/off toggle, where `l1` = no judge and `l2` =
build the judge. Two opaque tokens for one boolean confused devs — `l1` read as "a
difficulty level" and as "runs only the rule probes" (both false: the level never
subsetted probes; `--kind` does that, ADR-0012). `--judge` says exactly what it does.

The Judge stays **off by default** because it is a **spend gate**, not a feature
toggle: it issues extra `claude -p` calls (real cost). A corpus that needs a judge —
comparative probes (open-ended/plan) or `judge_rubric` rules — still **errors** until
`--judge` is passed, rather than silently spending or silently skipping the check. One
boolean covers both judge jobs: **Preference comparison** (comparative probes) and
**Rubric check** (`judge_rubric` rules); `assess` needs it only for the latter, since
it never runs a Preference comparison (`dsl.NeedsRubricJudge` vs `NeedsJudge`).

The underlying gate was already boolean (`levels["l2"]`), so this is a rename/UX
cleanup, not a redesign.

## Considered Options

- **Keep `--level` as a hidden deprecated alias for one release** (`l1`→`--judge=false`,
  `l2`→`--judge=true`, with a warning). Rejected: the tool is at `v0.1.0`, days old,
  with no installed base to protect; a compatibility shim is speculative weight (YAGNI).
  A hard rename is the smaller surface.
- **Keep `--level`, accept only `l1/l2`.** Rejected: preserves the exact confusion the
  rename exists to remove.
- **Two flags / a judge "mode" enum** (separate Preference vs Rubric toggles). Rejected:
  the corpus already determines *which* judge job runs; the dev only decides whether to
  pay for a judge at all — one boolean.

## Consequences

- `--level` is removed from all four corpus-running modes (bare, `run`, `assess`,
  `calibrate`); each gains `--judge` (default false). `parseLevels` and its
  retired-tier (`l3/l4`) error path are deleted.
- The judge-gate error now names `--judge` and states the spend reason.
- The `--level l1/l2` mentions in the **Consequences** of ADR-0008 and ADR-0012 are
  superseded here. The L1–L4 tier *reasoning* in the older locked ADRs (0003/0004/0006)
  is left as historical record — it predates and is unaffected by this flag rename.
