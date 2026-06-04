# Draft prompts

One generation prompt per stream. Backend is `claude -p --output-format json`; read the
assistant text from the JSON envelope's `result` field.

## Stream 1 — Rule-based (one run per doc, docs only, empty dir)

Run once **per selected doc**, that doc's text pasted in, in an empty dir (no codebase):

> You are drafting recall and convention probes for a behavioral validator. From the SOURCE
> DOC below, extract the specific, buried, easily-dropped Rules a maintainer would want to
> survive a refactor — conventions, invariants, required procedures (e.g. "amounts are
> string-typed", "run the migration script before deploy"). For each, emit: a direct
> question; the expected answer (the hard fact); whether to grade by `text_matches` regex
> (hard tokens) or `judge_rubric` facts (prose); and a proposed severity
> (critical|high|medium). Skip anything answerable from general knowledge without this doc.
> SOURCE DOC:\n<one doc's text>

## Stream 2 — Open-ended (whole project, Before worktree)

> You are drafting system/design probes for a benchmark that compares two docs configs by
> preference. Reading this project, propose prompts asking how the system actually works
> across components — data flow, service communication, infrastructure, event shape, and
> where it uses coupling vs a facade vs direct DB access. Prompt only — no expected answer,
> no checks. Favor questions whose answer a docs restructure could make tighter or vaguer.

## Stream 3 — Plan (whole project, Before worktree)

> You are drafting planning probes for a benchmark that compares two docs configs by the
> plans Claude produces. Reading this project, propose realistic task prompts that require
> project knowledge to plan well (e.g. "wire a new service end to end", "add a field across
> the read and write models"). Ask for a step-by-step plan, NOT execution. Prompt only — no
> expected answer. Favor tasks where a docs restructure could change which steps Claude
> remembers.
