# Rename the tool to `claude-benchmark`; agent in the binary, skills stay agent-agnostic

**Status:** accepted (settles the naming deferred by [ADR-0008](0008-one-command-evaluation-and-auto-detection.md))

The tool carried three competing roots: **validator** (binary, `cmd/`, `.validator/`, the `env-validator` skill), **benchmark** (every user-facing sentence — the help text says "A/B-**benchmark**", and CONTEXT.md reserves A/B "for the whole benchmark"), and **evaluate** (`internal/cli/evaluate.go`, the bare-command path). The binary said one thing while the tool described itself as another. This ADR collapses the root to **`benchmark`** and fixes how the now-planned multi-agent support attaches to the name.

**Why `benchmark`, not `validator` or `evaluator`:**

- The tool scores **nothing absolutely** — every signal is Before-vs-After differential. "validate"/"evaluate" carry a gate / absolute-score connotation that [ADR-0002](0002-report-not-gate-and-preference-judging.md) already retired ("the tool reports, the dev decides").
- "benchmark" is the word the tool already uses on itself (help text, and CONTEXT.md's **Preference comparison** entry reserves A/B "for the whole benchmark"). The rename aligns the binary with the vocabulary instead of inventing a fourth term.
- "evaluator" is a flagged-*avoid* synonym for the **Judge** in CONTEXT.md; reusing it for the whole tool would re-muddy a distinction the glossary drew on purpose.
- Scope widened past pure fidelity to **cost + quality + performance** deltas — which "validate" never covered but "benchmark" does.

**Two surfaces, named on different axes.** Multi-agent is planned — Claude first, then Codex / Copilot / others:

- **The binary names the agent.** `claude-benchmark` today; future agents are *sibling binaries* `codex-benchmark`, `copilot-benchmark`. The agent belongs here because each agent's headless runner, env files (`CLAUDE.md` vs `AGENTS.md` vs `copilot-instructions.md`), and stream format differ.
- **Skills stay agent-agnostic**, in the existing `env-*` family: `env-benchmark` (was `env-validator`), `env-restructure`, `generate-corpus`. A skill expresses dev *intent* ("benchmark my env"), which is agent-independent — so adding an agent ships a new binary and **renames no skill**.

## Considered Options

- **`claude-evaluator`** (the prior front-runner, issue #23). Rejected: "eval" implies absolute scoring of one thing; collides with the **Judge** avoid-list; never a word the tool used for itself.
- **Status quo `claude-validator`.** Rejected: "validate" reads as a gate, contradicting ADR-0002, and it is the odd one out against the tool's own help text.
- **Agent-tagged skills** (`benchmark-claude`, then `benchmark-codex`). Rejected: clear per-agent, but triples the skill list the day a second agent lands, for skills whose logic is agent-independent.

## Consequences (the mechanical sweep in this change)

- Binary `claude-validator → claude-benchmark` (alias `claude-bench`); `cmd/claude-validator/ → cmd/claude-benchmark/`; cobra `Use:` + help; `.gitignore` binary entry.
- State dir `.validator/ → .benchmark/` (corpus + memory-snapshot discovery); the snapshot **branch prefix** `validator/<name> → benchmark/<name>`, including the `--before` / `--after` defaults.
- Skill dir `env-validator/ → env-benchmark/` plus its `name:` and description.
- Temp prefixes + commit-author identity in `internal/autodetect` and `internal/snapshot` (`claude-benchmark-…`, `benchmark@localhost`); dogfood env var `CLAUDE_VALIDATOR_DOGFOOD → CLAUDE_BENCHMARK_DOGFOOD`.
- Glossary + docs: CONTEXT.md title and prose (its **Check** avoid-list now flags "validator" as the *retired* name), README, PRD, example corpora.
- **Deliberately untouched:** the generic `validateSamples` / `validateConcurrency` helpers (input validation, not the tool name); the recorded `internal/parser/testdata` fixtures (opaque captured payload); the `evaluate` / "Evaluation" verb in ADR-0008 and `internal/cli/evaluate.go` — reconciling that third root is deferred, not blocked by this rename.
- The Go module path (`github.com/nikalosa/claude-god`) is unaffected, so there are no import rewrites.
