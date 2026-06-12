# Routing — categories → destinations

The interview produces **categories** (by nature/risk). This file turns each category into a
**destination** mechanically, during plan generation. The human sees categories; the plan shows
destinations. Read [LOADING-MODEL.md](LOADING-MODEL.md) first — routing is meaningless without it.

## The four destinations

| Destination | Loads | Holds |
|---|---|---|
| **Root `CLAUDE.md`** | always | repo-wide, every-session, *short* facts + critical always/never rules. Target **< 200 lines**. |
| **Nested `<area>/CLAUDE.md`** | on subtree touch | orientation + specifics for **one** service/area (any file type). |
| **Path-scoped `.claude/rules/*.md`** (`paths:`) | on matching-file touch | a **cross-service, per-file-type** concern (conventions for all `*.go`, all migrations, etc.). |
| **`docs/*.md`** (plain-path pointer) | only when a pointer is followed | long reference needed only while doing one task type. |
| **Skill** | on demand (intent) | a multi-step **procedure / workflow / reusable expertise**. |

## Category → destination

| Interview category | Goes to | Rule |
|---|---|---|
| **critical-invariant** | **Root**, always-on, short | Never demote. Keep the terse invariant in root; the *detailed* version may live in a nested/rule home — but the invariant itself stays in root. |
| **always-relevant** | **Root** if short & repo-wide | If it's actually bulky, keep a one-line pointer in root and move the body to a rule/doc. |
| **conditionally-relevant** | nested vs rule vs skill — see decision tree below | The bulk of the win lives here. |
| **rarely-relevant / reference** | **`docs/*.md`** pointer, *or* inline in a path-scoped rule | See "rule-inline vs doc-pointer" below — the critical subtlety. |
| **questionable / cut-candidate** | **Cuts list** (separate, opt-in) | Never routed, never deleted silently. |

## Decision tree for conditionally-relevant

1. **Is it a multi-step procedure / workflow / intent-triggered expertise?** → **skill**.
   *Caveat:* never make a production-safety rule a skill — skills under-trigger; a missed safety
   rule is an incident. Skills are for convenience/workflow content only.
2. **Else, is it tied to a file-type/glob spanning multiple services** (all `*.ts`, all migrations,
   all `Dockerfile`s)? → **path-scoped rule** with a `paths:` glob.
3. **Else, is it tied to one service/area directory** (any file type)? → that **nested `CLAUDE.md`**.

## rule-inline vs doc-pointer (the subtlety that bites)

For long reference content, the question is **must it auto-load when a file is touched?**

- **Yes** (e.g. infra/deploy rules that must fire whenever someone touches `infra/**`) → a
  **path-scoped rule with the content inline**. A passive `docs/*.md` link will **not** auto-load.
- **No** (only read when a human goes digging) → **`docs/*.md`** with a **plain-path** pointer from
  the relevant rule/root. Never use `@import` for this — `@` loads eagerly and defeats the purpose.

## Size targets (re-checked in acceptance)

- Root `CLAUDE.md` **< 200 lines** (parameterizable; tighter is better).
- Any nested `CLAUDE.md` / rule **< 300 lines** (split by file-type or responsibility if over).
- After routing, **zero** `.claude/rules/*.md` should remain without `paths:` (unless something is
  genuinely always-on and cross-cutting — then it belongs in root, not an unscoped rule).
