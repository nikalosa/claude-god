---
name: generate-corpus
description: Drafts a claude-validator Corpus — probes and their checks — from a Target's hand-selected Before-branch context (CLAUDE.md, Claude rules, docs, pasted text), routing each rule-based candidate through the Closed-book check into admit, demote-to-open-ended, or drop-as-weak, then freezing the reviewed set onto Before. Use when authoring or extending a validator corpus, generating probes from an Environment, or when the user mentions corpus generation, the Generator, the Closed-book check, or a Steering config.
---

# Generate a validator corpus

Draft a frozen **Corpus** for `claude-validator` from a **Target**'s hand-selected
**Before** context. You draft; the dev reviews and freezes — never an unreviewed author.

The decisions are in `docs/adr/0004-corpus-generation-isolated-from-before-drafting.md`,
the glossary in `CONTEXT.md`, and the backend in `docs/adr/0003-judge-backend-claude-p.md`.
Read them. **Do not restate them** — reference them. Use the glossary's terms exactly.

## Guardrails (enforce — don't re-derive)

- **Generate from Before, freeze.** Assemble source with `git show <before>:<path>`. Never
  the After/working tree — a dropped **Rule** must stay probeable.
- **Isolation tracks probe KIND, not tier.** Rule-based = doc-borne (*proven*, below).
  Open-ended = codebase-aware, report-only (can't false-pass).
- **Closed-book check is the router and the guarantee.** No probe is frozen rule-based
  until its answer is proven doc-borne. See [closed-book-check.md](references/closed-book-check.md).
- **Backend = `claude -p --output-format json`** in a throwaway dir (ADR-0003). Not the
  Anthropic SDK, not the Workflow tool.
- **Severity proposed → dev-confirmed.** `critical` sets the gate bit — never freeze one
  the dev hasn't confirmed.
- **L4 out of scope.** L1 + doc-borne L2 + architectural L2 + L3 prompts only.

## Workflow

1. **Inputs.** Get the Before ref (e.g. `validator/before` from `claude-validator snapshot`)
   and the **Steering config** ([steering-config.md](references/steering-config.md)). If none
   exists, draft one with the dev: source globs + emphasis/skip notes + proposed severities.
2. **Assemble.** Resolve the Steering `sources` globs on Before via `git show <ref>:<path>`;
   concatenate into the selected source text. This curated set is the only doc grounding.
3. **Draft.**
   - *Rule-based:* `claude -p` in an **empty** dir with the selected source text in the
     prompt → candidates `{question, expected answer, check kind, proposed severity}`. May
     additionally read a Before worktree when a rubric fact must cite real code (schema-grounded);
     admission still hinges on step 4. Prompt: [closed-book-check.md](references/closed-book-check.md).
   - *Open-ended:* `claude -p` in a **Before worktree** (codebase-aware) → prompt-only probes.
4. **Route — Closed-book check.** Per rule-based candidate, run the two-pass check →
   **admit** rule-based / **demote** to open-ended / **drop** as **Weak probe**.
   [closed-book-check.md](references/closed-book-check.md).
5. **Review + freeze.** Show the dev the probes grouped by route, severities flagged for
   confirmation. Edit conversationally. On confirm, write `.validator/corpus/<name>.yaml`
   (mirror the shape below) and the Steering config to `.validator/steering.yaml`, and commit
   both onto Before — only when the dev says so. Regeneration **appends** new probes; never
   rewrites the frozen set.

## Output shape

Mirror existing corpora exactly — read them, don't reinvent:

- Rule-based regex / rubric: `.validator/corpus/self.yaml`
- Rubric + `kind: open_ended`: `examples/corpus/l2-smoke.yaml`

`text_matches` for hard tokens; `judge_rubric` (`facts` + `pass_score`) for prose;
`kind: open_ended` (prompt only, no rules) for preference probes.

## Validate

After freezing, the dev measures the noise floor (live, costs money):

```sh
claude-validator calibrate --target <repo> --corpus .validator/corpus/<name>.yaml \
  --branch <before> --no-memory-snapshot
```

Before-vs-Before; tighten or drop any flaky **Check** before trusting a real Before-vs-After
run. A malformed corpus errors at load, before any spend.
