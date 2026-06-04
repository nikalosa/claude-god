# Drop regression-detection; the Generator drafts three independent corpus streams

**Status:** accepted (supersedes [ADR-0004](0004-corpus-generation-isolated-from-before-drafting.md), extends [ADR-0002](0002-report-not-gate-and-preference-judging.md))

We stop trying to *guarantee* that a probe can catch a deliberately dropped rule. ADR-0004 built the **Closed-book check** to prove each rule-based probe was doc-borne — so a PASS→FAIL flip was provably a real regression. With regression-catching no longer a goal, that machinery (Closed-book check, Weak probe, doc-borne proof) is removed.

Instead the **Generator** is a drafting helper that produces the **Corpus** in three independent streams:

1. **Rule-based probes** — generated from hand-selected **docs only** (one subagent per doc), collecting the important rules.
2. **Open-ended probes** — system / design / architecture questions, generated with the **whole project** in context.
3. **Plan probes** — task prompts for plan-vs-plan, generated with the whole project in context.

The three generations are independent. The dev then talks to the skill, edits probes, and finalizes the corpus. It is a helper tool, not an authority.

## Why

- The real workflow is a human asking "is the restructured environment better?" — answered by reading Before/After answers, plans, and **Numbers** side by side. A formal regression *guarantee* was machinery the human did not need.
- Dropping it removes the second `claude -p` per candidate (the Closed-book check) and two glossary concepts.
- A rule-based probe Claude can answer without the docs is now filtered by the **dev at finalization**, not by an automated check.

## Consequences

- **Retired:** Closed-book check, Weak probe, doc-borne proof, and the "has it been shown to Detect a removed rule" worry.
- **Demoted:** Regression / New pass are plain observations the dev reads in the matrix — never gated, scored, or counted (extends ADR-0002's report-not-gate to its conclusion).
- Generation context is split by **stream, not tier**: docs-only for rule-based; whole-project for open-ended and plan.
- The `generate-corpus` skill must be rewritten to this three-stream model; its Closed-book / router steps are obsolete.
- **Implement (L4)** stays out of scope; v1 **Modes** are **Ask** and **Plan** only.
