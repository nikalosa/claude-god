
<always_consider> 
Be extremely concise. Sacrifice grammar for the sake of concision. Speak as understandable, direct and simple as possible.
</always_consider>

`claude-god` = umbrella for Claude Code tooling experiments. First tool: `claude-benchmark` — A/B benchmarks CLAUDE.md context configs for behavioral-fidelity regressions.

Shared vocabulary in CONTEXT.md — read first, use those terms.
Full design + locked decisions in docs/PRD.md — read when doing design work.

## Principles:
- Think first: state assumptions, ask when unclear, push back on overcomplication.
- KISS. YAGNI. Minimum code that solves the task; nothing speculative.
- Surgical edits — every changed line traces to the request.
- Define verifiable success before coding; loop until it passes.

## No Code Comments
- Do NOT add comments that restate what the code does. No step-by-step narration.
