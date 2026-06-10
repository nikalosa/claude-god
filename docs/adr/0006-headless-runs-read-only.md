# Headless runs are read-only and skill-free; the harness grades text, not actions

**Status:** accepted — **amended by [ADR-0008](0008-read-only-bash-via-pretooluse-guard.md): `Bash` is re-enabled, constrained to read-only by a PreToolUse guard** (so the model loads content terminal-style via slices, not whole-file `Read`s). The "Scope Bash to read-only — Rejected" option below was reopened: it failed via *permission rules* under `bypassPermissions`, but a PreToolUse hook is a different lever that holds under bypass. Everything else here stands.

The corpus design already forbids real-infrastructure side effects ([PRD](../PRD.md) — grade *intent*, not *outcome*). Running the validator against PAM exposed a leak: an L2 architectural probe triggered a project skill that wrote HTML and opened it in the dev's browser. The run produced a side effect, spent tokens/tool-calls on it, and the artifact was ungradeable (it never lands in the stream as assistant text).

So the harness now restricts every `claude -p` run to a read-only toolset and disables skills:

```
--disallowedTools Agent Bash Edit Write WebFetch  --disable-slash-commands
```

The graded signal is the **assistant text** (L1 recall, L2 answers) — never an artifact. The model keeps `Read/Grep/Glob` to inspect the Environment and still emits its answer as text; it loses only the ability to mutate the tree, shell out, hit the network, or open a browser. Bare-name disallows are removed from context and hold even under `bypassPermissions` (scoped denies like `Bash(open *)` would not), so this is robust against the bypass mode the harness requires.

**Skills are out of scope because they are not part of the Environment.** The Environment under test is CLAUDE.md + Claude rules (`.claude/rules/*`) + docs + memory ([CONTEXT.md](../../CONTEXT.md)); `.claude/skills/*` is not in that set. Letting a skill fire adds side effects and Numbers noise without testing any env delta, and Claude Code has no per-skill lever — only `--disable-slash-commands` (all skills) or banning the tools a skill uses. Disabling them wholesale is fair (both environments are equally skill-free) and zero-surprise.

## Considered Options

- **Defang, don't disable** (`--disallowedTools …` without `--disable-slash-commands`). Rejected: a skill whose whole point is the artifact (e.g. HTML) may error rather than answer once its Write/Bash are blocked, and skills still aren't part of the Environment. Keeping them buys noise, not signal.
- **Scope Bash to read-only** (`Bash(git log:*)` etc.). Rejected: scoped allow/deny rules are not enforced under `bypassPermissions`, which the harness needs to avoid prompt deadlock. Under bypass, Bash is all-or-nothing — so it's removed.
- **Status quo (Agent-only disallow).** Rejected: leaves Write/Bash/skills free, the exact path the PAM leak took.

## Consequences

- **Diff capture goes empty.** `captureDiff` still runs but, with no mutation tools, records no changes — correct for v1's tiers (L1/L2 answer/recall, no implementation). When L4 (full execution) lands it will need a different, write-enabled tool profile; this read-only profile is scoped to the answer/plan tiers.
- **WebFetch off** also removes a non-reproducibility source — answers can't vary with live network content.
- Extends the existing `--disallowedTools Agent` (added so all tool-calls surface as countable top-level calls); same intent — a controlled, countable, side-effect-free run.
- No unit test pins the flag string (no `claude` in unit tests, per the repo's test shape); the guarantee is verified live during a real run.
