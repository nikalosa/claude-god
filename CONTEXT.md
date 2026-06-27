# claude-benchmark

The language of comparing two Claude environments (the messy "before" and the restructured "after") and reporting what improved and what changed. The product is a **decision-support report a human reads** — not an automated gate. Terms here are the project's vocabulary; general programming concepts are excluded.

**Environment** here means the whole Claude context configuration of a project: its **CLAUDE.md** (root + nested), **Claude rules** (`.claude/rules/*`), **docs**, **memory**, and the **MCP servers** it loads. These are distinct parts — "Claude rules" is *only* `.claude/rules/*`, never an umbrella for the others. The public **skill-name** surface shortens this bundle to **config** (`config-bench`, `config-refactor`) — it names the whole configuration, friendlier than "environment" (reads as env-vars) and broader than "context" (one slice).

## Language

### Subjects under test

**Target**:
The repository whose context configuration is being benchmarked. claude-benchmark runs Claude Code headlessly *inside* it.
_Avoid_: Repo-under-test, subject, SUT.

**Environment**:
One of the two configurations being compared: **Before** (baseline) and **After** (the change under test). Every probe runs in both. An Environment is **layered** — a git ref supplies the base layer (code + in-tree CLAUDE.md/rules/docs) and the memory policy, and an explicit **MCP config** sits on top — so Before and After can share a ref and differ only in MCP (e.g. an MCP server on/off). The git ref is one input, not the whole definition; the **worktree** is the per-ref cwd Runs execute in — one per distinct ref, shared by every **Run** of that ref ([ADR-0015](docs/adr/0015-shared-per-ref-worktree.md)) — never the Environment itself.
_Avoid_: Config (for a single Before/After side — that's an **Environment**; "config" is reserved for the public skill-name surface above), variant, arm, side (except `VerdictSide`, which is the per-environment half of a verdict).

**Mode**:
What a probe asks Claude to produce: **Ask** (answer a question) or **Plan** (produce a step-by-step plan, *not* execute it). Executing the task is out of scope for v1.
_Avoid_: Tier, Level, L1–L4 (retired — see Flagged ambiguities), stage, phase.

### Corpus

**Corpus**:
The per-target set of probes — the **dataset** the dev runs Claude against, before and after. Drafted by the **Generator**, then reviewed and finalized by the dev. claude-benchmark ships an example corpus only; a target owns its real corpus.
_Avoid_: Suite, test set, training set. "Dataset" is an acceptable synonym.

**Probe**:
One prompt sent to Claude, plus how its response is graded. The unit of the corpus. Three kinds, one per generation stream: **Rule-based probe**, **Open-ended probe**, **Plan probe**.
_Avoid_: Test, case, question.

**Rule-based probe**:
A probe drawn from the **docs**, where the answer is doc-stated (e.g. "how are monetary amounts typed?"). Graded *absolutely* — each environment's response is checked on its own against a fixed set of **Rules** ("did it take into account what it had to?"). The grader never reads the other environment's response; the dev compares the two PASS/FAIL outcomes by eye. **Ask** mode.
_Avoid_: Closed probe, deterministic probe.

**Open-ended probe**:
A system/design question about the **code/project** with no fixed answer (e.g. "how do the betting and ledger services communicate?"). Graded *comparatively* by the **Judge** in a head-to-head **Preference comparison** — which environment's answer reads better for a dev — alongside **Numbers**. **Ask** mode.
_Avoid_: Architectural probe (use as descriptor only), fuzzy probe.

**Plan probe**:
A task prompt where Claude produces a **step-by-step plan** (no execution). The two environments' plans are compared by **Preference comparison** plus **Numbers** (before-plan vs after-plan). Graded like an Open-ended probe, but in **Plan** mode.
_Avoid_: Plan-vs-plan probe, design probe, L3.

**Rule**:
A single *behavior* expected of Claude — the idea/principle that must survive a restructure — carrying a **Severity** (`critical | high | medium`) and one or more **Checks**. Graded by behavior, never by whether any source text survived.
_Avoid_: Assertion, requirement, expectation. **Never** use bare "rule(s)" for source files — see Flagged ambiguities.

**Claude rules**:
*Only* the `.claude/rules/*` files. One part of an **Environment**, alongside **CLAUDE.md**, **docs**, and **memory** — it does not stand for all of them. claude-benchmark does not care if any source text is moved, merged, reworded, or deleted — only whether the **Rules** (behaviors) the maintainer expressed across these still hold.
_Avoid_: "Rules" (bare — means the graded unit); using "Claude rules" for CLAUDE.md or docs.

**Check**:
One predicate evaluated against a run. Pattern-first (e.g. `text_matches`); the **Judge** is the escape hatch for prose. A rule passes only if all its checks pass.
_Avoid_: Predicate (reserve for the DSL family), matcher, validator (the tool's retired name — and it implies a gate, which a Check is not).

### Corpus generation

**Generator**:
The drafting helper that produces the **Corpus** in three independent streams: **Rule-based probes** from hand-selected **docs only**; **Open-ended probes** and **Plan probes** from the whole **project** (codebase-aware). A *drafting* aid, never an unreviewed author — the dev talks to it, edits probes, drops any rule-based probe Claude can answer without the docs, and finalizes the set. Run once against **Before**, then frozen.
_Avoid_: Corpus builder, scraper, auto-author.

**Steering config**:
The checked-in artifact that drives the **Generator**: which **docs** feed the Rule-based stream (globs), emphasis/skip notes, and proposed **Severities**. Committed to **Before** beside the frozen **Corpus** so generation stays reproducible.
_Avoid_: Prompt (bare), generation prompt.

### Grading & outcome

**Run**:
One headless `claude -p` execution of a probe in one environment, producing a **RunRecord**. Probes are sampled at an odd N per environment (default 1; raise to N≥3 for the majority-vote and **Disagreement** signals). A run is **read-only**: the model inspects the Environment with `Read/Grep/Glob` and with `Bash` constrained to read-only commands by a PreToolUse guard (so it slices files terminal-style instead of reading them whole), but is denied the mutating/network/browser tools and all **skills** ([ADR-0006](docs/adr/0006-headless-runs-read-only.md), [ADR-0009](docs/adr/0009-read-only-bash-via-pretooluse-guard.md)) — the graded signal is the assistant *text*, never an artifact. The Environment's **MCP** servers *are* loaded and their tools allowed (MCP is part of what's under test, unlike skills), pinned with `--strict-mcp-config` so only the servers the Environment declares load — never the dev's ambient user/global config ([ADR-0014](docs/adr/0014-mcp-as-environment-layer.md)).

**Regression**:
A rule whose majority-voted outcome flipped PASS (Before) → FAIL (After) — "what changed for the worse." Visible in the matrix and read by the dev; claude-benchmark does not gate, score, or count it.

**New pass**:
A rule that flipped FAIL → PASS — "what improved." Read by the dev.

**Disagreement**:
The N samples of one rule in one environment splitting (not unanimous). Surfaced as flakiness, distinct from a clean flip.

**Assessment**:
Scoring **one Environment** against the **Corpus** with no Before/After — the answer to *"assess current config with this corpus"* (the `assess` command). **Rule-based probes** grade absolutely (per-rule PASS/FAIL, the same **Rules** path); **Open-ended** and **Plan probes** have no single-env grade — a **Preference comparison** needs two answers — so they run for **Numbers** only and are listed as not graded. The report is a flat scorecard with no Δ column. Distinct from the A/B benchmark (bare / `run`) and from **calibration** (Before-vs-Before noise).
_Avoid_: single run (that is a **Run**), evaluation (overloaded — the tool's retired verb).

### Grading engines

**Judge**:
The LLM-based grader. Runs in two modes: **Rubric check** (absolute, for rule-based probes a regex cannot express) and **Preference comparison** (comparative, for open-ended and plan probes). Isolated so its run-to-run noise never touches the deterministic pattern path. Report-only — never gates.
_Avoid_: Grader (that's the whole grading step), evaluator, LLM-as-judge.

**Rubric check**:
The Judge in absolute mode: scores one response against an explicit list of discrete facts it must contain (the **Rubric**), emitting present/absent per fact. The mechanism that makes rule-based Judge grading reproducible-enough.
_Avoid_: Criteria, scoring guide.

**Preference comparison**:
The Judge in comparative mode: reads the Before and After responses head-to-head and picks the better one *for a dev to read* — across **conciseness**, **exhaustiveness**, **directness**. To neutralize ordering bias, both orderings are run and a win counts only if it survives both; otherwise tie. Report-only.
_Avoid_: Scoring, rating, A/B preference (reserve A/B for the whole benchmark).

**Numbers**:
The **efficiency signal — the main thing a restructure is trying to improve.** Captured from every run, always compared Before→After regardless of probe kind. No LLM, never gates — the dev reads it. The headline figures:
- **Context window** — `BaseContextTokens` (deterministic turn-1 resident context = the pure cost of the Environment, the clean config signal) and `PeakContextTokens` (high-water mark, exploration-noisy). Both stored and shown, surfaced *above* raw input tokens — how much window the Environment eats matters more to a dev than cumulative input.
- **Cost, tokens, tool-call counts** — **exact**: the same work bills and calls the same however runs execute, so they survive a cache hit unchanged.
- **Duration** — two numbers per run: total wall-clock (`duration_ms`) and model **generation time** (`duration_api_ms`). Wall-clock is **advisory** — it absorbs local contention under concurrency and the differing conditions of a cached-Before/fresh-After split. Generation time is the **comparable** duration signal: it tracks the model's own work, so its Before→After delta stays valid both under concurrency and across a cache hit. Each run is stamped with the concurrency it was measured under.

Whether the tool calls were the *right* ones (read the proper chunk vs grepped around blindly) is read from the run's trace, not a number.
_Avoid_: Metrics (overloaded), stats.

### Run cache

**Run cache**:
The persistence layer that stores completed **RunRecords** so an unchanged environment is never re-run. The cached unit is the raw **Run** (the `claude -p` output), never a **Verdict** or **Numbers** — grading is re-done every time from the stored record, so editing a **Rule**, **Severity**, or **Check** costs no run. Keyed by **Fingerprint**, so **Before**/**After** are labels, not cache identities — any committed side hits when its Fingerprint matches. The one exception is the **uncommitted working tree**: captured as a synthetic, ever-changing snapshot (a fresh SHA every run), it is **never cached** — no read, no write — so the cache is **baseline-only** in practice, reusing the stable committed Before across the edit-rerun loop while the changed side always runs fresh ([ADR-0016](docs/adr/0016-run-cache.md) §11).
_Avoid_: Result cache, memoization. Say **baseline-only** (committed refs cache; the uncommitted working tree never does) — not "before-only", since a committed After caches too.

**Fingerprint**:
The Run cache key: `hash(resolved commit SHA + effective MCP config + memory policy + model + effort + CLI version)` paired with the probe's `(prompt content + Mode)`. Keyed on the **resolved SHA**, never the branch name (`benchmark/before` is a moving pointer), and on the probe's prompt *content*, never its **Probe** ID (an edited prompt under the same ID must miss). **CLI version** defaults to detected `claude --version` so a bump misses and re-runs; `--cli-version <token>` overrides that one component to reuse a pool across bumps (or deliberately split it) — and every **RunRecord** also *stamps* its true CLI version, so a pinned, version-mixed **Sample pool** stays detectable in the report. Deliberately excludes everything that is re-graded — **Rules**, **Severities**, **Checks**, the **Judge**.
_Avoid_: Cache key (use as descriptor only), hash, signature.

**Sample pool**:
The ordered, append-only list of **RunRecords** a **Fingerprint** maps to. A request for odd N is served `pool[:N]`: grow by **appending** fresh runs when the pool is short, shrink by taking the deterministic **prefix** when it is long (never discarding the surplus). Order is load-bearing — `pool[0]` is the representative answer the **Preference comparison** reads, so appending never perturbs an existing verdict.
_Avoid_: Sample set, sample bag (it is ordered, not a bag).

## Flagged ambiguities

**"rule"** — overloaded, resolved:
- **Rule** (bare) = the graded behavior unit in the corpus.
- **Claude rules** = *only* the `.claude/rules/*` source files — never CLAUDE.md or docs.
A restructure freely rewrites CLAUDE.md, Claude rules, and docs; claude-benchmark checks the **Rules** (behaviors) survive. Losing a source file is fine; losing a Rule is "what changed."

**Tier / Level / L1–L4** — **retired.** Probes are classified by **Mode** (Ask / Plan) and kind (Rule-based / Open-ended / Plan probe), generated as three independent streams. The retired `--level l1/l2` flag is now the boolean **`--judge`** ([ADR-0013](docs/adr/0013-judge-flag-replaces-level.md)): it carries no conceptual weight — it selects no probes, every probe in the corpus runs — it only builds the **Judge** when the corpus needs one (pass `--judge` for a corpus with open-ended/plan or `judge_rubric` probes). Off by default, since the Judge adds `claude -p` calls (real spend).

## Example dialogue

> **Dev:** The report shows Before and After both answered `monetary_as_string` correctly.
> **Expert:** Then that rule survived the restructure — read it as "nothing changed here." It's a rule-based probe: each environment graded on its own against the rule, no head-to-head. If Before passed and After failed, that flip is a regression you'd see in the matrix — but the tool only shows it, it never blocks.
> **Dev:** And the "how do betting and ledger communicate" probe?
> **Expert:** Open-ended — no fixed answer. The Judge does a preference comparison, both orderings, and you read it next to the token and time **Numbers**. You decide if After is better.
> **Dev:** Same for the planning task?
> **Expert:** Same shape, Plan mode — Before's plan vs After's plan, preference plus Numbers. Three streams, one report, you make the call.
