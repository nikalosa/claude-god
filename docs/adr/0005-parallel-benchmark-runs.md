# Benchmark runs execute in a bounded parallel pool; cost/tokens stay exact, Duration degrades to advisory

**Status:** accepted

The benchmark was strictly sequential — for P probes it runs P × N samples × 2 environments `claude -p` invocations back-to-back, dominating wall-clock. The runs are independent (each `harness.Run` is a pure function of `(branch, prompt)`; aggregation is a join after all runs finish), so they are flattened into one bounded `errgroup` pool (`--concurrency`, default 8) and collected into a result matrix indexed by `(probe, environment, sample)`; grading runs serially afterwards. This is safe for a tool whose credibility rests on determinism because the **verdict is preserved** — indexed collection has no shared mutable state, and the one real race (concurrent `git worktree add`/`remove` on the shared `.git`) is serialized by a package-level mutex around only those git calls.

## Considered Options

- **Stay sequential.** Rejected: the independence is real and the speedup is ~the concurrency factor; the only thing lost is one metric's precision (below), which the report flags honestly.
- **Hand-rolled semaphore + WaitGroup + sync.Once.** Rejected: it re-implements `errgroup` (bounded fan-out + first-error-cancel) inline, where concurrency bugs hide. `golang.org/x/sync` is a Go-team module with zero transitive deps — adding it removes code, it doesn't add weight.
- **Per-probe barrier (parallelize one probe's runs, grade, repeat).** Rejected: drains the pool between probes, never filling the cap; worse utilization than a flat pool.

## Consequences

- **Cost and tokens stay exact** — they come from the model's `modelUsage` / `total_cost_usd`, which bill the same work identically regardless of scheduling. They remain the authoritative resource Numbers.
- **Duration degrades to advisory.** `duration_ms` is each run's own wall-clock; under concurrency it inflates from shared CPU, a shared account rate limit, and 429 backoff. The report still prints it but annotates it not-comparable unless `--concurrency 1`. See the Numbers entry in [CONTEXT.md](../../CONTEXT.md).
- **First error aborts the batch**, cancelling in-flight siblings via the errgroup context; `claude -p` is killed through `exec.CommandContext`, and each run's worktree/memory cleanup still fires because those defers use `context.Background()`. Matches the prior sequential "first failure aborts" semantics.
- **No orchestrator-level backoff.** `claude -p` retries 429s internally, so a rate-limited worker blocks rather than failing; the pool cap is the politeness knob, not a correctness guard.
- **Grading and the Judge stay serial.** The Judge also shells `claude -p`; parallelizing grading is a separate future step, deliberately out of scope here.
- The pool takes an injected `runFunc`, so its determinism (identical verdicts/medians at `--concurrency 1` vs 8) is unit-tested with a fake — no `claude` in unit tests.
