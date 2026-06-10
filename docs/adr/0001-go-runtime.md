# Build claude-benchmark in Go

**Status:** accepted

We're writing claude-benchmark in Go rather than TypeScript or Python. The tool is fundamentally subprocess orchestration (`claude -p`, `git worktree`, `git diff`) with fan-out parallelism (N=3/N=5 runs per probe) over a deterministic data pipeline (stream-json parse → run record → DSL grade → aggregate → report). Go fits all three: `os/exec` is its home turf, goroutines + `errgroup` are the cleanest model for the parallel runs, and a single static binary is the best distribution story for an OSS CLI (no Node/Python runtime on the user's machine). Its forced-explicit struct schemas are *aligned* with the project's core goal — credibility through deterministic, golden-file-tested data shapes.

## Considered Options

- **TypeScript/Node** — best ecosystem fit (Claude Code is Node, audience runs Node, `npx` distribution, first-party Anthropic SDK). Rejected because the distribution + concurrency + determinism advantages of Go outweighed ecosystem familiarity for a no-deadline tool.
- **Python** — author's strongest language; terse data-shaping for the aggregator/report. Rejected for weaker distribution (pip/venv friction) and an ecosystem mismatch with the Claude Code audience.

## Consequences

- The one place Go costs more than the alternatives is the YAML predicate DSL evaluator (`not()`/`and()`/`or()`/`bash_call_matches`) — a recursive tree over heterogeneous YAML, which needs custom unmarshaling or a `map[string]any` walk. This is bounded, one-time, and not future pain.
- The judge-LLM adapter uses `anthropic-sdk-go` (or a thin HTTP client); it is an isolated module either way, so SDK maturity is not a blocker.
