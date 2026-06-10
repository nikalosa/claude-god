# One bare command runs the A/B benchmark; Before/After auto-detected from git

**Status:** accepted (extends the PRD's CLI/UX)

The dev-facing entry point collapses to a single command. Running `claude-benchmark`
with no subcommand — or the **env-benchmark** skill from inside Claude Code — runs the
whole **A/B benchmark**: it auto-discovers the **Corpus**, auto-detects **Before** and
**After** from git, runs every probe across both environments, and prints the report. The
prior flow (`snapshot before` → switch worktree → `snapshot after` → `calibrate` →
`run --level … --target … --corpus … --before … --after …`) is demoted: every flag becomes
an optional override, and `run`/`snapshot`/`calibrate` survive as power-user-only commands.
The motivation: the *repeated* action (benchmark each restructure) must be one step; the
*per-target setup* (authoring a corpus) stays separate and human-in-the-loop.

**Auto-detection.** The harness already builds its own throwaway worktree from any ref, so
the manual `snapshot` step — the sole reason a dev juggled worktrees — is removed. Defaults:

- **After** = the working tree (HEAD + uncommitted env edits, temp-committed with
  `git add -A` so new untracked rule/doc files count).
- **Before** = `HEAD` when the tree is **dirty** (isolates exactly the uncommitted edits);
  `merge-base(default-branch, HEAD)` when the tree is **clean** (isolates the restructure
  branch's commits). On a clean default branch where merge-base == HEAD there is nothing to
  compare — it errors and asks for `--before`.

Memory is injected **live** (current project memory → both worktrees, held constant across
the A/B) instead of being committed into a snapshot branch.

**Out of scope here (since settled).** The tool rename was deferred by this ADR; it was
later ratified in [ADR-0010](0010-rename-to-claude-benchmark.md) — the root is **`benchmark`**
(`claude-benchmark` / `.benchmark/`), not the briefly-floated `claude-evaluator`. This ADR only
adds the bare command + auto-detection. The `evaluate` verb (this file's title, `evaluate.go`)
is the still-unreconciled third root and is intentionally left as-is.

## Considered Options

- **merge-base as the universal Before.** Rejected: when the tree is dirty on a feature
  branch that also holds non-env commits, merge-base drags that unrelated work into the diff
  and blames it on the restructure. Splitting on dirtiness keeps the diff surgical — dirty →
  `HEAD`, clean → merge-base.
- **Compare against `main`'s tip instead of merge-base** (clean case). Rejected: if main
  advanced with unrelated commits after the branch forked, its tip smears that drift into the
  comparison; the fork point does not.
- **Fold calibration into every benchmark.** Rejected: a Before-vs-Before pass ≈1.5×'s the
  Before-side spend for a noise number that barely moves between restructures. Calibration
  stays opt-in (the `calibrate` command); each run instead inline-flags any rule whose Before
  samples **Disagree**.
- **One command also generates the corpus.** Rejected: silently drafting a corpus from the
  bloated Before is exactly what ADR-0004's human-in-the-loop Generator forbids — an LLM
  reading the bloat is blind to the buried rules. Missing corpus → the command errors and
  points at `generate-corpus`; the skill offers to launch it.
- **Thick skill over a low-level CLI.** Rejected: the dev runs from terminal *and* Claude
  Code, so the engine itself must be convenient. Smarts live in the Go binary (deterministic,
  ADR-0001); the thin **env-benchmark** skill adds only what a skill can — reading the report
  back as a conversation — and must never auto-edit the environment ("automated restructuring
  driven by results" is out of scope until the tool is trusted).

## Consequences

- The root cobra command gets a default action; `--target/--corpus/--before/--after/--level`
  are optional. `--level` defaults to `l1` (no judge); pass `l2` for a corpus that needs the
  **Judge** (open-ended/plan/judge_rubric probes) — it selects no probes, every probe runs.
- Before spending (a 20-probe corpus at N=3 ≈ 120 `claude -p` calls), the command prints a
  plan — resolved Before/After, probe count, run count — and asks to proceed; `--yes` skips it
  for the skill / CI.
- Corpus auto-discovered from `.benchmark/corpus/`; one file is used directly, several prompt
  the dev to choose (or error listing them when stdin is not a TTY).
- The **env-benchmark** skill has three jobs — detect & route (offer to launch
  `generate-corpus` if none), run with in-chat confirmation, interpret the report — and one
  boundary: it never edits the environment.
- Reproducing a benchmark months later (a `--freeze` that pins its inputs) is deferred;
  live auto-detection is the v1 default.
