# How Claude Code loads context (verified)

Routing decisions are only correct if this model is correct. These facts are confirmed against
the official docs (`code.claude.com/docs/en/memory.md`). **Do not rely on anything not listed
under REAL.**

## REAL — rely on these

| Surface | When it loads |
|---|---|
| Root `CLAUDE.md` | **Always, at launch.** This is the always-on budget. |
| `.claude/rules/*.md` **with `paths:`** | **Lazily** — only when Claude reads a file matching one of the globs. |
| `.claude/rules/*.md` **without `paths:`** | **Unconditionally at launch.** ← this is the bloat to eliminate. |
| Nested `services/<svc>/CLAUDE.md` | When Claude reads **any** file in that subtree (not glob-scoped — the whole file loads on any touch). Siblings do not preload. |
| `docs/*.md` via **plain path / markdown link** | **Only when a pointer is followed** (read on demand). A link does **not** auto-load content. |
| `docs/*.md` via **`@path` import** | **Eagerly, always-on.** `@` pulls the content in at launch. |
| Skills | On demand (intent-triggered). |

Key consequences:

- The frontmatter key is **exactly `paths`** — a YAML array of globs. `globs:` and `applyTo:`
  **do not exist** and silently do nothing.
- A rule with a `paths:` glob that matches **no real file** never loads and throws **no error** —
  a silent dead rule. Always verify globs against actual files.
- `@import` is eager. So a "read this only when needed" note next to an `@` is a lie — either it
  loads always (`@`) or it's a pointer (plain path). **Keep `@` only for genuinely always-on content.**
- `claudeMdExcludes` in `.claude/settings.json` skips `CLAUDE.md` / rules files by glob — use it to
  drop generated/scratch context (e.g. `"**/.moon/docker/**/CLAUDE.md"`).

## Ground-truth

The bundled `scripts/claude-context-budget.sh` estimates load with a bytes/4 proxy. Where possible,
have the user run the real `/context` once at launch and once after touching a single subtree to
confirm routing matches the plan.

## PHANTOM — never propose these (they are not real features)

- ❌ A **stop-hook that auto-proposes CLAUDE.md updates**.
- ❌ The **`#` shortcut** to append learnings to CLAUDE.md.
- ❌ **`globs:` / `applyTo:`** frontmatter keys — only `paths` works.
- ❌ A **`claude-md-improver` / "6-axis rubric"** skill. Use token counts as the instrument instead.
