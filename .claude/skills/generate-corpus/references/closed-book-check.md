# Closed-book check

The **Generator**'s router and guarantee (ADR-0004, CONTEXT.md). It proves a rule-based
candidate's answer is **doc-borne** — the only thing that makes a dropped **Rule** show up
as a detectable **Regression**. Run it per rule-based candidate, after drafting.

"Source docs" = the files matched by the Steering `sources` globs (plus `pasted`).

## The two passes

**Pass 1 — code present, docs stripped.**

```sh
repo=<target repo>; ref=validator/before
cb="$(mktemp -d)"
git -C "$repo" archive "$ref" | tar -x -C "$cb"    # tracked files at <ref>, NO .git to recover from
rm -f "$cb"/CLAUDE.md "$cb"/.claude/rules/*.md      # remove EVERY file matching Steering `sources`
( cd "$cb" && claude -p "<candidate question>" \
    --output-format json --permission-mode bypassPermissions )   # tools on — it must read the code
rm -rf "$cb"
```

> **Isolate from git, not just the working tree.** A `git worktree` shares the repo's object
> store, so an agent with tools recovers the "stripped" docs via `git show <ref>:<path>` — the
> strip becomes a no-op and every candidate wrongly survives. `git archive | tar` gives a
> history-less copy. Likewise strip any non-source file that *encodes the answer* — an existing
> validator corpus, golden test fixtures — or the codebase leaks the **Rule** and the probe
> wrongly demotes.

**Pass 2 — empty dir (no code, no docs).** Run only if Pass 1 still conveys the fact.

```sh
d=$(mktemp -d); ( cd "$d" && claude -p "<candidate question>" --output-format json ); rm -rf "$d"
```

Read the assistant text from the json envelope's `result` field (mirror
`internal/judge/backend.go`). **Survives** = the text still conveys the candidate's
**expected answer**; **breaks** = it doesn't (says it can't tell, or answers wrong/generic).

## Decision

| Pass 1 (code, no docs) | Pass 2 (empty) | Route | Why |
|---|---|---|---|
| **breaks** | — | **admit rule_based** | doc-borne → a dropped Rule flips it. Detectable. |
| survives | **breaks** | **demote open_ended** | code carried it → can't false-pass; report-only preference. |
| survives | survives | **drop — Weak probe** | priors carried it → a dropped Rule never flips it. Flag for the dev, never freeze silently. |

A doc-only convention rule (e.g. "amounts are string-typed", stated only in CLAUDE.md)
breaks at Pass 1 because stripping the source docs removes its only carrier — the check
reduces to empty-dir isolation, the planted-fact (`zilworld`) trick in
`internal/detection/testdata`. No special-casing needed.

## Draft prompts

**Rule-based draft** — empty dir, selected source text in the prompt:

> You are drafting recall and convention probes for a behavioral validator. From the SOURCE
> TEXT below, extract the specific, buried, easily-dropped Rules a maintainer would want to
> survive a refactor — conventions, invariants, required procedures (e.g. "amounts are
> string-typed", "run the migration script before deploy"). For each, emit: a direct
> question; the expected answer (the hard fact); whether to grade by `text_matches` regex
> (hard tokens) or `judge_rubric` facts (prose); and a proposed severity
> (critical|high|medium). Skip anything answerable from general knowledge without the text.
> SOURCE TEXT:\n<selected source text>

**Open-ended draft** — Before worktree, codebase-aware:

> You are drafting architectural design probes for a benchmark that compares two docs configs
> by preference. Propose prompts asking how the system actually works across components — data
> flow, service communication, event shape. Prompt only — no expected answer, no checks. Favor
> questions whose answer a docs restructure could make tighter or vaguer.
