# The Judge reuses `claude -p`, not the Anthropic API

**Status:** accepted (revises the judge-backend note in [ADR-0001](0001-go-runtime.md))

The Judge (LLM grader for rubric scoring and preference comparison) invokes `claude -p` in a throwaway **empty** working directory — not the Anthropic API via `anthropic-sdk-go`. Running in an empty dir keeps it neutral: no CLAUDE.md, no Claude rules, no tools, so the Judge cannot be swayed by the very Environment it grades. Its only context is what we pass in the prompt: the probe question, the candidate answer(s), and the rubric or preference dimensions.

The driver is auth and adoption: `claude -p` reuses the developer's existing **OAuth login** (the same one the harness uses), so the Judge needs no `ANTHROPIC_API_KEY` and no separate billing setup — an OSS adopter with only a Claude subscription can run it. This keeps the L2 work AFK (no secret to provision).

## Considered Options

- **Anthropic API (`anthropic-sdk-go` / HTTP)** — the ADR-0001 assumption. Gives temperature 0 and explicit model pinning. Rejected as the default because it requires an API key with billing, separate from the OAuth login already present, which would gate adoption and make L2 HITL.

## Consequences

- `claude -p` does not expose temperature, so the Judge is not bit-deterministic. Mitigated by the existing **median-of-3** design: each of the N=3 sampled answers is judged once and the median score is thresholded, absorbing per-call wobble.
- The product grades itself (Claude Code judging Claude Code), which is slightly less independent than a separate model. Acceptable for a report-only signal a human reads; not acceptable for an auto-gate, which we do not have.
- The `judge` package hides the backend behind one interface. Swapping to the Anthropic API later (if true temp-0 determinism is ever needed) is a localized change.
