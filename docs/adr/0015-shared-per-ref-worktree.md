# Benchmark runs share one worktree per ref instead of one per run

**Status:** accepted (extends [ADR-0005](0005-parallel-benchmark-runs.md); reverses the per-run-worktree note in `internal/harness/harness.go`; rests on read-only runs [ADR-0006](0006-headless-runs-read-only.md) and the per-Environment MCP layer [ADR-0014](0014-mcp-as-environment-layer.md))

Each `harness.Run` created **and destroyed its own** git worktree — one checkout per run, so a batch did `probes × samples × 2` checkouts. On a large target (PAM, ~13k files) a checkout is ~3.5s and they serialize on disk (the slow `reset --hard` saturates I/O even when issued in parallel), so checkout cost is linear in run count and a large share of wall-clock.

Because a **Run is read-only** ([ADR-0006](0006-headless-runs-read-only.md): the model inspects the tree with `Read/Grep/Glob` and read-only `Bash`, but `Edit/Write/WebFetch` and Bash-writes are denied), a worktree is never mutated by a run and is safe to reuse. The worktree lifecycle is therefore hoisted out of the per-run path: one worktree is created **per distinct ref** and shared by every Run of that ref.

## Decision

1. **Key worktrees by ref, not by run.** The distinct refs in `{before, after}` get one worktree each — ≤2 total, and **1** when Before and After share a ref (the MCP on/off case, [ADR-0014](0014-mcp-as-environment-layer.md)): they share the worktree and differ only in the `--mcp-config` flag, never in tree content.
2. **The lifecycle lives in the command funcs** (`bare`/`run`/`assess`), not in the run pool — preserving the injected-`runFunc` seam whose determinism is unit-tested with a fake ([ADR-0005](0005-parallel-benchmark-runs.md)). The pool still never sees a worktree; the production `runFunc` closes over a `map[ref]*Worktree` and calls `harness.RunIn`.
3. **Create worktrees eagerly, one per distinct ref, before the run pool starts.** The **Run cache** is deferred ([ADR-0016](0016-run-cache.md)); when it lands, creation moves to lazy-on-first-cache-miss behind a per-ref create-once guard, so a fully-cached ref makes zero checkouts.
4. **Inject memory once at creation; remove the `~/.claude/projects/<slug>` scratch once after the pool drains — never per-run.** A per-run remove would `RemoveAll` the shared scratch while sibling runs are still writing their session transcripts into it. The pool waits for all runs to return before the deferred teardown fires, so nothing is mid-run at removal.
5. **Drop `captureDiff`.** Under the read-only profile it only ever produced an empty, unread diff, and its `git add -A` / `reset` were the only writes into the shared worktree's index — a race under concurrency. Its `DiffPath`/`DiffStatPath`/`WorktreePath` result fields, unread by any caller, are removed with it.

## Considered Options

- **One worktree per concurrency slot (Option 1).** Pool-sized trees, reused within a slot. Zero new assumptions (no shared cwd) but still ≈`concurrency` checkouts and no collapse to 2. **Kept as the fallback** if a shared cwd ever proves flaky — it uses the same `Prepare`/`RunIn`/`Close` split.
- **Keep one worktree per run (status quo).** Rejected: checkout cost is linear in run count and dominates wall-clock on large targets.

## Consequences

- **Shared cwd is safe because runs are read-only — verified.** 6 concurrent `claude -p` in one shared cwd returned 6/6 correct, isolated answers with zero cross-contamination; `~/.claude.json` (the single global file every run writes regardless of cwd) stayed valid; session transcripts are keyed by per-run **session UUID**, so concurrent runs sharing one project scratch never collide. This is the evidence that overturns the old harness.go assumption that a shared cwd would collide under concurrency.
- **Checkout cost becomes fixed (~2 checkouts), not linear.** Projected checkout time: 20-run batch ~70s→~7s; 60-run ~210s→~7s; 180-run ~630s→~7s. Wall-clock saving is ≤ that (checkout partly overlaps claude latency) but mostly additive, since checkouts serialize on disk while claude is network-bound.
- **The MCP startup-race cap is unchanged** ([ADR-0014](0014-mcp-as-environment-layer.md)): it bounds concurrent `claude -p` invocations, not worktrees. A retry just re-invokes claude in the same shared cwd — fine, because runs are read-only and concurrent-safe.
- `worktreeMu` still serializes the (now ≤2) `worktree add` / `remove` admin ops against the shared `.git`.
