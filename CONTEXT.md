# claude-validator

The language of comparing two Claude environments (the messy "before" and the restructured "after") and reporting what improved and what quietly broke. The product is a **decision-support report a human reads** — not an automated gate. Terms here are the project's vocabulary; general programming concepts are excluded.

**Environment** here means the whole Claude context configuration of a project: its **CLAUDE.md** (root + nested), **Claude rules** (`.claude/rules/*`), **docs**, and **memory**. These are distinct parts — "Claude rules" is *only* `.claude/rules/*`, never an umbrella for the others.

## Language

### Subjects under test

**Target**:
The repository whose context configuration is being benchmarked. The validator runs Claude Code headlessly *inside* it.
_Avoid_: Repo-under-test, subject, SUT.

**Environment**:
One of the two configurations being compared, pinned to a git branch: **Before** (baseline) and **After** (the change under test). Every probe runs in both.
_Avoid_: Config, variant, arm, side (except `VerdictSide`, which is the per-environment half of a verdict).

**Tier**:
A difficulty level of probing, L1–L4: recall (L1), design Q&A (L2), plan-only (L3), full implementation (L4). Higher tiers cost more and give higher-fidelity signal.
_Avoid_: Level in prose (the `--level` flag keeps the name), stage, phase.

### Corpus

**Corpus**:
The per-target set of probes — the **dataset** the dev runs Claude against, before and after. Dev-authored in v1 (tool-assisted); auto-generation from the Environment is the next step. The validator ships an example corpus only; a target owns its real corpus.
_Avoid_: Suite, test set. "Dataset" is an acceptable synonym.

**Probe**:
One prompt sent to Claude, plus how its response is graded. The unit of the corpus. Two kinds: **Rule-based probe** and **Open-ended probe**.
_Avoid_: Test, case, question.

**Rule-based probe**:
A probe graded *absolutely* — each environment's response is checked on its own against a fixed set of **Rules** ("did it take into account what it had to?"). The grader never reads the other environment's response; a **Regression** is found by comparing the two PASS/FAIL outcomes. This is where "what compromised" comes from.
_Avoid_: Closed probe, deterministic probe.

**Open-ended probe**:
An architectural / design question with no fixed rule list (e.g. "how is RBAC implemented across these services?"). Graded *comparatively* by the **Judge** in a head-to-head **Preference comparison** — which environment's answer is better for a dev to read.
_Avoid_: Architectural probe (use as descriptor only), fuzzy probe.

**Rule**:
A single *behavior* expected of Claude — the idea/principle that must survive a restructure — carrying a **Severity** (`critical | high | medium`) and one or more **Checks**. Graded by behavior, never by whether any source text survived.
_Avoid_: Assertion, requirement, expectation. **Never** use bare "rule(s)" for source files — see Flagged ambiguities.

**Claude rules**:
*Only* the `.claude/rules/*` files. One part of an **Environment**, alongside **CLAUDE.md**, **docs**, and **memory** — it does not stand for all of them. The validator does not care if any source text is moved, merged, reworded, or deleted — only whether the **Rules** (behaviors) the maintainer expressed across these still hold.
_Avoid_: "Rules" (bare — means the graded unit); using "Claude rules" for CLAUDE.md or docs.

**Check**:
One predicate evaluated against a run. Pattern-first (e.g. `text_matches`); the **Judge** is the escape hatch for open-ended rules. A rule passes only if all its checks pass.
_Avoid_: Predicate (reserve for the DSL family), matcher, validator (overloaded with the tool name).

### Corpus generation

**Generator**:
The drafting step that turns hand-selected source text — **CLAUDE.md**, **Claude rules**, **docs**, and any pasted-in text — into a draft **Corpus**, run once against the **Before** environment and then frozen. Access tracks **probe kind, not tier**: **rule-based probes** must be doc-borne, proven by the **Closed-book check** (the generator may still read code for schema-grounded rubric facts); **open-ended probes** (incl. architectural L2) may read the codebase, being report-only and unable to false-pass. The empty-dir isolation it leans on is the Judge's ([ADR-0003](docs/adr/0003-judge-backend-claude-p.md)). A **drafting** aid, never an unreviewed author — the dev reviews and edits conversationally before freezing.
_Avoid_: Corpus builder, scraper, auto-author.

**Closed-book check**:
The **Generator**'s router and quality gate, run with the codebase present but the **docs stripped**. Three outcomes: answer **breaks** without docs → doc-borne → admit as **rule-based probe** (a regression is detectable); answer **survives** on the code → not doc-borne → demote to **open-ended probe** (preference, report-only); answer survives even with no code → prior-borne → **Weak probe**. For doc-only convention rules the codebase is irrelevant, so the check reduces to full isolation — the cheap form of the planted-fact (`zilworld`) trick the detection harness uses.
_Avoid_: Open-book test, ablation (reserve for the detection harness).

**Weak probe**:
A candidate whose answer **survives** its **Closed-book check** with neither docs nor code carrying it — answerable from training priors, so a dropped **Rule** would never flip it. Flagged for the dev's review, never frozen silently.
_Avoid_: Trivial probe, redundant probe.

**Steering config**:
The checked-in artifact (selected-doc globs + emphasis/skip notes + proposed **Severities**) that drives the **Generator**, committed to the **Before** branch beside the frozen **Corpus** so generation stays reproducible and auditable.
_Avoid_: Prompt (bare), generation prompt.

### Grading & outcome

**Run**:
One headless `claude -p` execution of a probe in one environment, producing a **RunRecord**. Probes are sampled N=3 per environment.

**Detect**:
The report correctly showing a real change — a genuinely dropped rule appearing as a **Regression**. The open worry: the tool has only ever been run Before-vs-identical-Before (correctly showing "nothing changed"); it has not yet been shown to *detect* a deliberately removed rule. Distinct from a **false positive** (noise shown as a regression) and the **noise floor** (the false-positive rate measured by a Before-vs-Before calibration run).
_Avoid_: Fire, trigger, trip, alarm.

**Regression**:
A rule whose majority-voted outcome flipped PASS (Before) → FAIL (After). This is "what compromised." A `critical`-severity regression also sets a non-zero exit code — a harmless optional bit for anyone wiring CI later, not the product.

**New pass**:
A rule that flipped FAIL→PASS — "what improved."

**Disagreement**:
The N samples of one rule in one environment splitting (not unanimous). Surfaced as flakiness, distinct from a clean Regression.

### Grading engines

**Judge**:
The LLM-based grader. Runs in two modes: **Rubric check** (absolute, for rule-based probes a regex cannot express — gates) and **Preference comparison** (comparative, for open-ended probes — report-only). Isolated so its run-to-run noise never touches the deterministic pattern path.
_Avoid_: Grader (that's the whole grading step), evaluator, LLM-as-judge.

**Rubric check**:
The Judge in absolute mode: scores one response against an explicit list of discrete facts it must contain (the **Rubric**), emitting present/absent per fact. The mechanism that makes rule-based Judge grading reproducible-enough.
_Avoid_: Criteria, scoring guide.

**Preference comparison**:
The Judge in comparative mode: reads the Before and After responses head-to-head and picks the better one *for a dev to read* — across **conciseness**, **exhaustiveness**, **directness**. To neutralize ordering bias, both orderings are run and a win counts only if it survives both; otherwise tie. Report-only.
_Avoid_: Scoring, rating, A/B preference (reserve A/B for the whole benchmark).

**Numbers**:
The cost / token / time deltas, captured numerically from every run and always compared Before→After regardless of probe kind. No LLM. Report-only. Within Numbers, **cost and tokens are exact** — the same work bills the same however the runs are executed — while **time (Duration) is advisory**: it inflates when runs share resources, so the report marks it not-comparable unless the runs were unparallelized. The exact Numbers are the resource north-star; Duration is a rough read.
_Avoid_: Metrics (overloaded), stats.

## Flagged ambiguities

**"rule"** — overloaded, now resolved:
- **Rule** (bare) = the graded behavior unit in the corpus.
- **Claude rules** = *only* the `.claude/rules/*` source files — never CLAUDE.md or docs.
A restructure freely rewrites CLAUDE.md, Claude rules, and docs; the validator checks the **Rules** (behaviors) survive. Losing a source file is fine; losing a Rule is "what compromised."

## Example dialogue

> **Dev:** The report says the After environment regressed `monetary_as_string`.
> **Expert:** That's a "what compromised" finding — you dropped a line that mattered. It only counts if the majority of the three Before runs passed and the majority of the After runs failed. If the three split, that's disagreement, not a regression.
> **Dev:** It's a rule-based design probe graded by the Judge against a rubric — same treatment?
> **Expert:** Same. The Judge's rubric check just turns the answer into per-fact pass/fail; once it's a rule outcome it reads like any other. What we never want is the Judge's own noise to *invent* a regression on a clean run — that's why we measure the noise floor with a Before-vs-Before pass first.
> **Dev:** And the RBAC architecture probe?
> **Expert:** That's open-ended — no rubric. The Judge does a preference comparison: which answer reads better, both orderings. That's a "what improved/worsened" signal you read by eye, alongside the token and time numbers. You decide if the restructure was worth it.
