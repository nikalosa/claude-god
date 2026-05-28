# stream-json shape notes (Issue #2 spike)

Observed from one **flat** (no-tool, single-turn) run:

```
claude -p "In one sentence, what is the purpose of claude-validator?" \
  --output-format stream-json --verbose --permission-mode bypassPermissions
```

Captured as `run-flat-01.jsonl` (golden fixture #1). `--verbose` is **required** with
`-p --output-format stream-json` (omitting it errors). `--permission-mode bypassPermissions`
ran fully headless — no prompts.

## Event stream (JSONL, one JSON object per line)

Flat run emitted 4 events, in order:

| # | `type` | `subtype` | Role |
|---|--------|-----------|------|
| 0 | `system` | `init` | Session metadata: `session_id`, `model`, `cwd`, `tools`, `memory_paths`, `permissionMode`, `claude_code_version`, … |
| 1 | `rate_limit_event` | — | Transient. **Parser must tolerate/skip unknown transient types.** |
| 2 | `assistant` | — | A streaming assistant message snapshot. |
| 3 | `result` | `success` | **Authoritative terminal event** — final text, cost, usage, timing. |

Tool-using runs will additionally emit `assistant` events whose `message.content[]` holds
`{"type":"tool_use",...}` blocks, and `user` events carrying `tool_result` blocks. Not seen
in this flat fixture — capture a tool-use + sub-agent fixture before implementing those paths.

## Where the fields the RunRecord needs live

- **Final assistant text** → `result.result` (clean string). Also reconstructable from
  `assistant.message.content[].text`, but `result.result` is canonical and simplest for L1 grading.
- **Total cost** → `result.total_cost_usd` (float; already aggregates all models used).
- **Final/authoritative usage** → `result.usage`: `input_tokens`, `output_tokens`,
  `cache_creation_input_tokens`, `cache_read_input_tokens`, nested `cache_creation`, plus an
  `iterations[]` per-message breakdown.
  - The `assistant` event also has a `message.usage`, but it is a **mid-stream snapshot**
    (showed `output_tokens: 1` vs the result's `90`). **Trust `result.usage` for totals.**
- **Per-model breakdown** → `result.modelUsage` (map keyed by model id, e.g.
  `claude-opus-4-8[1m]`). Each value has `inputTokens`/`outputTokens`/`cacheReadInputTokens`/
  `cacheCreationInputTokens`/`costUSD`/`contextWindow`/`maxOutputTokens`.
  - **Surprise:** a flat opus run also lists a `claude-haiku-4-5-*` side-call (background work).
    `total_cost_usd` already includes it; don't double-count if summing `modelUsage`.
- **Timing** → `result.duration_ms`, `result.duration_api_ms`, `result.ttft_ms`, `result.num_turns`.
- **Status** → `result.is_error` (bool), `result.stop_reason` (`end_turn`), `result.subtype`
  (`success`), `result.permission_denials`.

## Sub-agent recursion seam (deferred to a later fixture)

Every `assistant` event carries `parent_tool_use_id` (null in this flat run). A spawned
sub-agent's events are expected to carry a **non-null** `parent_tool_use_id` pointing at the
parent `Agent(...)` tool_use block — this is the key for attributing child token usage back up
the tree. Issue #3 leaves a documented seam here; recursion is implemented once a sub-agent
fixture is captured.
