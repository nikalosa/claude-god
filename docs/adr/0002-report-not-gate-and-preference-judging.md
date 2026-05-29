# v1 is a comparison report, not a gate; open-ended probes judged by preference

**Status:** accepted (amends the PRD)

The product of v1 is a **decision-support report a human reads** — "what improved, what got compromised, what it cost" — across two Claude environments (messy *before*, restructured *after*). The developer reads it and decides; nothing is auto-rejected. The PRD framed the tool around a CI-style gate (exit non-zero to block a restructure batch); that framing is demoted. The exit code stays as a harmless optional bit for anyone wiring CI later, but it is not the point.

Consequently, open-ended architectural probes are graded by **comparative preference** — the Judge reads the *before* and *after* answers head-to-head and picks the one better for a dev to read (conciseness, exhaustiveness, directness), run in both orderings with a win counting only if it survives both. This pulls "qualitative shape," which the PRD deferred to v2, **into v1** — it is safe to include precisely because it only informs the report and never blocks.

## Considered Options

- **Keep the gate as the product** (PRD original). Rejected: the real workflow is a human restructuring an Environment and asking "is the new one better?" — a judgment call, not a pass/fail a robot should make. A fuzzy LLM preference auto-blocking work is the opposite of useful.
- **Defer qualitative preference to v2** (PRD original). Rejected once gating was dropped: with no gate, judge noise can't cause a false block, so the comparative signal is pure upside.

## Consequences

- Two probe kinds: **rule-based** (absolute per-environment grading → Regression detection, "what compromised") and **open-ended** (comparative preference → "what reads better"). Plus cost/token/time **Numbers**, always compared.
- Judge-scored rules use a 0–100 score against a pass line; to tame drift, each of the N=3 sampled answers is judged once and the **median** score is thresholded (not one answer judged repeatedly).
- The dataset (corpus) is **dev-authored in v1** (tool-assisted); auto-generation from the Environment is the planned *next* step, still deferred here because an LLM reading a bloated Environment is blind to the same buried rules the bloat hides.
