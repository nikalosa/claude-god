# MCP servers as a controlled Environment layer

**Status:** accepted (extends the **Environment** definition in [CONTEXT.md](../../CONTEXT.md); builds on the headless read-only profile in [ADR-0006](0006-headless-runs-read-only.md))

An **Environment** includes the **MCP servers** Claude loads, not just its in-tree
context (CLAUDE.md / rules / docs / memory). A change like "add a code-intelligence
MCP server" is an environment change a dev wants benchmarked — does it actually make
Claude cheaper/faster at the same answer quality? — so the tool must be able to vary
MCP across Before and After.

The blocker was that the harness tied "Environment" to a git branch: a **Run** spawns
a worktree from a ref and runs `claude -p` inside it. MCP config and any index a server
needs live largely *outside* committed content (untracked `.mcp.json`, local settings,
a gitignored on-disk index), so they never reached the worktree — and the run silently
inherited whatever MCP servers the dev had in their *global* `~/.claude` config, an
uncontrolled variable.

## Decision

1. **MCP is a per-Environment layer, independent of the ref.** The run pool's per-side
   unit becomes `Env{Ref, MCPConfig}` (was a bare branch string), so **Before and After
   can share a ref and differ only in MCP** (e.g. a server on/off).
2. **MCP is always a controlled variable.** `invokeClaude` always passes
   `--strict-mcp-config`, so *only* the servers the Environment declares load — never the
   dev's ambient user/global config. (This also fixes a latent nondeterminism that
   predated the feature.)
3. **Per-side resolution** (`harness.mcpConfig`): an explicit config wins (a
   `--mcp-config` file path or inline JSON, from `--before-mcp`/`--after-mcp`, or `--mcp`
   for `assess`/`calibrate`); else the ref's committed `.mcp.json`; else none. So a
   committed `.mcp.json` travels with the ref for free, and an untracked/machine-specific
   config is supplied by flag.
4. **Path-bound index servers are the user's config, not the tool's concern.** A server
   whose index is keyed to the repo path (e.g. CodeGraph, which in MCP mode resolves the
   project from the client's rootUri = the worktree, where no index exists) is pinned via
   the server's *own* flags in the user-authored config — `codegraph serve --mcp --path
   <abs-repo> --no-watch` — so it serves the real index regardless of the worktree cwd.
   The tool stays MCP-agnostic; no server gets special-cased.

## Considered Options

- **Carry MCP only via a committed `.mcp.json` on two branches** (keep the ref as the
  sole axis). Rejected as the *only* mechanism: can't express a same-ref on/off; forces
  committing machine-specific absolute paths; and the common real setup (untracked/local
  MCP) stays invisible. Kept as the default *fallback* when no explicit config is given.
- **Copy or symlink the server's index into each worktree.** Rejected: the motivating
  index is ~300 MB, per run; the server's own `--path` resolves it with zero copy
  (verified — `codegraph status <abs>` from a foreign cwd returns the full index).
- **Leave MCP to default discovery (no `--strict-mcp-config`).** Rejected: the run would
  inherit the dev's personal global servers, so the benchmark would measure a different
  environment than the one declared — nondeterministic and unfair.

## Consequences

- `harness.Opts` gains `MCPConfig`; `invokeClaude` always emits `--strict-mcp-config`
  and adds `--mcp-config <cfg>` when set; `claudeArgs`/`mcpConfig` are split out and
  unit-tested (no `claude` needed).
- cli: `Env{Ref, MCPConfig}` threads through `runBenchmark`/`runSingleEnv`; `run` gains
  `--before-mcp`/`--after-mcp`, `assess` and `calibrate` gain `--mcp`. The bare command
  stays auto-detect-only (no MCP flag — MCP has no git signal; it picks up a committed
  `.mcp.json` if present).
- **Behavior change:** runs no longer inherit ambient/global MCP servers. A repo that
  commits `.mcp.json` still loads it, now deterministically.
- **Numbers** (tool-call counts, tokens) now capture MCP tool usage, so "did this MCP
  server actually cut retrieval cost?" is a first-class readout. The server's own
  out-of-band compute (e.g. building its index) is not in the `claude -p` stream — the
  comparison measures *session* cost, not the server's amortized index build.
- **Limitation:** a path-pinned index server serves one real-repo state, so the
  comparison is valid when Before and After share that ref (the MCP on/off case). If they
  differ in code, the single index matches only one side — out of scope here; author
  per-side configs or re-index.

## Addendum — `--strict-mcp-config` is not full MCP control

`--strict-mcp-config` fixes *discovery* (only declared servers load, never ambient
ones). It does **not** guarantee a declared server is actually *usable* in a run. Two
ways a declared server silently goes missing — both grade as a false "no difference":

1. **Deny-side gap (by name).** A server disabled by name in the dev's ambient
   `~/.claude.json` `disabledMcpServers` still loads `status:"disabled"` —
   `--strict-mcp-config` does not re-enable it. Worktrees resolve to the same project
   via the shared `.git`, so the ambient deny applies. Workaround: rename the server in
   the MCP config, or drop it from the disabled list.
2. **Startup race under concurrency.** Headless `claude -p` does **not** block on the
   MCP handshake before turn 1, and no flag forces it. Each run cold-starts its own
   stdio server; under CPU contention from many concurrent runs the handshake loses the
   race, the turn starts with no MCP tools, init status stays `"pending"`, and zero
   `mcp__*` calls are made. (Measured: 2 concurrent wins, 6 loses; the *server* boots in
   ~0.2s even 6× concurrent — the starved *client* is the bottleneck, not the server.)

**Detection (the only ground truth).** The stream has no post-init "connected" event;
`mcp_servers` status appears once, in `system/init`. `"pending"` is ambiguous (it can
still connect-and-be-used). So the only positive proof a declared server was usable is
an actual `mcp__<server>__*` tool call. `harness.checkMCPHealth` fails a run unless each
**declared** server (parsed from the `--mcp-config`) is either `connected` at init or
made ≥1 call — catching both gaps above.

**Prevention.** When an Environment declares MCP (explicit `--before-mcp` /
`--after-mcp` / `--mcp`), the run pool caps first-pass concurrency
(`mcpRunConcurrencyCap`) and serializes retries through a gate, so the rare miss is
re-rolled under low contention where the handshake wins. Correctness over speed —
Duration is already advisory above concurrency 1. A ref's *committed* `.mcp.json` is
resolved later in the harness and is not visible to the pool, so the cap does not engage
for that case (the detector still fails a missed run; supply MCP by flag for the
capped-and-retried path). A shared, pre-warmed server (one boot, HTTP transport) would
remove the race at full concurrency — deferred until the cap proves too slow.
