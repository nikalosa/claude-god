# claude-validator

A/B-benchmarks two CLAUDE.md context configurations (before vs after a restructure)
for behavioral-fidelity regressions, while quantifying token/cost/time deltas.

Full design: [docs/PRD.md](docs/PRD.md). Key decisions: [docs/adr/](docs/adr/).

## Status

Pre-v1, building the L1 slice. Tracked in GitHub issues #1–#7.

## Build & run

Requires **Go 1.24+** (earlier internal linkers omit `LC_UUID`, which recent macOS rejects at load).

```sh
go build ./...
go run ./cmd/claude-validator --help
go run ./cmd/claude-validator run --level l1 --target . --corpus <dir>
```

## Layout

```
cmd/claude-validator/   CLI entrypoint (thin; delegates to internal/cli)
internal/
  cli/          cobra command tree (root + run)
  harness/      isolated per-probe run: worktree, memory swap, claude -p, diff capture  (#4)
  parser/       stream-json JSONL -> RunRecord; shape notes + golden fixtures in testdata  (#3)
  dsl/          YAML predicate DSL -> per-rule PASS/FAIL  (#5+)
  judge/        Anthropic-API grading escape hatch (L2 rubric, L3 plan diff)  (later)
  aggregator/   N=3 runs -> median stats + majority-vote outcomes  (#6)
  report/       markdown (default) + JSON rendering  (#5+)
```

Most `internal/` packages are stubs today — each holds a doc comment stating its role
and the issue that implements it.
