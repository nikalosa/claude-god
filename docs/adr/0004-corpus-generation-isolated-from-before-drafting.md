# Corpus generation is an isolated, from-Before *drafting* skill

**Status:** accepted (amends the corpus-generation deferral in [ADR-0002](0002-report-not-gate-and-preference-judging.md))

ADR-0002 deferred auto-generating the corpus because "an LLM reading a bloated Environment is blind to the same buried rules the bloat hides." We reintroduce generation — but as a human-backstopped **drafting** skill, not unattended auto-generation, which sidesteps that objection. A Claude Code skill runs `claude -p` against the **Before** branch over hand-selected source text (CLAUDE.md, Claude rules, docs, pasted text) — doc-isolated for rule-based probes, codebase-aware for open-ended ones — drafts probes, and the dev reviews/edits them conversationally before freezing the result as the repo's behavioral source-of-truth corpus.

Why this beats the self-blind objection:

- **The dev hand-selects the input.** Curation is the human backstop; the LLM never scrapes a bloated Environment blind. This is the "tool-assisted" authoring CONTEXT.md already sanctions, made concrete.
- **Generated from Before, frozen.** Before is the comprehensive (if bloated) statement of intent. Generating from the suspect After would make a dropped rule structurally undetectable — the dropped line isn't there to generate a probe from. The frozen corpus is the shared yardstick for both calibration (Before-vs-Before) and the real Before-vs-After run.
- **Rule-based probes are doc-borne by construction**, proven by the Closed-book check (below) rather than assumed from a tier label — the guarantee a regression is detectable at all.
- **Closed-book check** doubles as the router: run with the codebase present and the docs stripped, a candidate is admitted as rule-based only if its answer breaks without the docs; otherwise it is demoted to open-ended preference (code carries it) or dropped as a weak probe (priors carry it).

## Scope by probe kind (not tier)

Isolation tracks the **probe kind**, not the tier label — L2 in particular spans both kinds.

- **Rule-based** (L1 recall; L2 doc-borne conventions, procedures, invariants — "what commands before deploy," "how amounts are typed"). Generator emits question + expected answer → a `text_matches` regex (hard tokens) **or** a `judge_rubric` fact-list (prose), tagged per probe. The generator may read the codebase when a question references it (schema-grounded rubric facts), but a candidate is admitted as rule-based **only if it passes the Closed-book check** — the direct test that its answer is doc-borne. Severity is **proposed** from doc cues + the Steering config, then **dev-confirmed** — never finalized unreviewed, since `critical` sets the gate bit.
- **Open-ended** (L2 architectural — "how do services A and B communicate," event-sourcing shape; L3 task prompts). Generator is codebase-aware and emits a **prompt only** — no expected answer, no checks. Graded by Preference comparison, report-only. Safe to read the codebase precisely because open-ended probes cannot produce a false "nothing compromised." Architectural L2 questions **default here**, promoted to rule-based only if the Closed-book check proves them doc-borne.
- **L4 — out of scope.** Hand-authored if ever; an isolated generator cannot write reliable diff/tool-call checks for files it has not seen.

## Considered Options

- **Full unattended auto-generation** (ADR-0002's deferred form). Rejected: self-blind to buried rules, and an LLM finalizing severity unreviewed would drive the gate bit on a guess.
- **Generate from current / After docs** ("up-to-date"). Rejected: a rule dropped by the restructure is gone from After's docs, so no probe is ever written for it — the tool reports green on the exact regression it exists to catch.
- **Hand-curation only** (status quo). Rejected for L1/L2 (~45 probes of authoring toil); retained for L4.

## Consequences

- **Two generation modes, split by probe kind not tier:** doc-grounded for rule-based probes (a regression is detectable only if the answer is doc-borne — enforced by the Closed-book check), codebase-aware for open-ended probes (report-only, can't false-pass). L2 spans both.
- Reuses the ADR-0003 `claude -p` backend and empty-dir isolation — one LLM backend across judge and generator, no Anthropic API key.
- The Closed-book check costs a second `claude -p` per candidate (codebase present, docs stripped) — cheap relative to a full N=3 environment run.
- **Steering config** (selected-doc globs + emphasis/skip notes + proposed severities) is checked into the Before branch beside the frozen corpus. Regeneration is a **reviewed, additive diff** — new probes appended — never an in-place rewrite of the frozen set.
- New CONTEXT.md terms: **Generator**, **Closed-book check**, **Weak probe**, **Steering config**.
