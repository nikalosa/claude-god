# CLAUDE.md

`claude-god` = umbrella for Claude Code tooling experiments. First tool: `claude-validator` — A/B benchmarks CLAUDE.md context configs for behavioral-fidelity regressions.

Full design + locked decisions in @docs/PRD.md — read when needed, never restate here.

Principles:
- Be extremely concise. Sacrifice grammar for the sake of concision.
- Think first: state assumptions, ask when unclear, push back on overcomplication.
- KISS. YAGNI. Minimum code that solves the task; nothing speculative.
- Surgical edits — every changed line traces to the request.
- Define verifiable success before coding.
