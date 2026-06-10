# Read-only Bash via a PreToolUse guard (amends ADR-0006)

**Status:** accepted — amends [ADR-0006](0006-headless-runs-read-only.md)

[ADR-0006](0006-headless-runs-read-only.md) made runs read-only by bare-name disallowing `Bash`. Measuring context cost exposed a side effect of that choice: with no `Bash`, the model loads file content via `Read` (whole files) and `Grep` (whole matches), where a terminal session slices with `cat | head`, `sed -n 'N,Mp'`, `grep | head`, and inspects history with `git log`/`diff`/`show`. On the wallet probe the headless run ingested ~4× the tool-result content of an equivalent manual session reading the *same* files — the single `wallet-client.ts` was 3,062 chars via `head -120` but 33,248 chars via whole-file `Read` (10.9×). That inflates resident-context measurements and makes the run a poor mirror of how a developer actually inspects a repo.

So `Bash` is re-enabled, constrained to **read-only** by a `PreToolUse` hook:

```
--disallowedTools Agent Edit Write WebFetch
--settings '{"hooks":{"PreToolUse":[{"matcher":"Bash",
  "hooks":[{"type":"command","command":"<self> __bash-read-guard"}]}]}}'
--permission-mode bypassPermissions  --disable-slash-commands
```

ADR-0006 rejected scoping Bash because *permission rules* (`Bash(cat:*)`) are not enforced under `bypassPermissions`. A `PreToolUse` hook is a different lever: it fires regardless of permission mode and a hook that exits non-zero **blocks the call before permission rules are evaluated**, so it holds under bypass — the property the scoped-rule approach lacked. The hook is the benchmark binary's hidden `__bash-read-guard` subcommand (`internal/cli/bashguard.go`); the classifier is `internal/bashguard`. Wiring it via `--settings` (precedence 2) overrides the worktree's own settings without editing the tree under test.

## Threat model: cooperative model, not a sandbox

The model under test is **cooperative** — it is Claude Code being assessed for behavioral fidelity, not an attacker trying to escape. The guard is therefore not an airtight sandbox; its job is to block the actions that would (a) pollute or slow the measurement, or (b) leave durable state a run cannot reverse. Two facts shape what that requires:

- **The run executes in an ephemeral git worktree.** `captureDiff` does `git add -A; git diff --cached; git reset` and the worktree is removed with `--force`, so any **tracked-file write is captured and discarded**. The guard deliberately does *not* chase in-tool file writes (`sort -o`, `find -fprintf`, `uniq IN OUT`, `sed -i`) — the worktree reset already neutralizes them.
- **The worktree shares the target repo's `.git`.** Refs and objects are *not* tracked files, so a `git branch -d` / `git reset` / `git tag -d` would mutate the real repo and survive the worktree reset. These are *not* backstopped, so git is gated tightly (below).

What the backstop cannot undo — and what the guard exists to block — is the un-backstopped set: **command-runners, interpreters, command/process substitution, the network, and shared-`.git` mutation.** Output redirection to a file is also blocked, not because the write survives (it doesn't) but so a run cannot read back a file it just wrote and mistake it for the environment.

## The guard (`internal/bashguard.Classify`)

Allowlist-first, **fail closed**: a command is allowed only if it is *provably* read-only; anything unparseable or unresolvable is denied. It parses with `mvdan.cc/sh/v3/syntax` (a real shell AST, not regex — robust to quoting/escaping) and walks every node:

- **Command** — every simple command's name must be a bare (no `/`) name in a narrow read-only allowlist (`cat`, `head`, `tail`, `grep`, `rg`, `ls`, `find`, `wc`, `sort`, `uniq`, `cut`, `tr`, `comm`, `diff`, `sed`, `jq`, `stat`, `git`, …). Command-runners (`env`, `xargs`, `sudo`, `time`, `command`, `exec`) and interpreters (`python`, `node`, `perl`, `awk`, `bash`, `sh`) are absent — they hide the real command and are the un-backstopped exec vector. Dynamic names (`$cmd`) and path-qualified names (`./cat`) → deny.
- **No env prefix** — any inline assignment before a command (`GIT_PAGER=… git`, `LESSOPEN=… less`) → deny; this is the behaviour-injection vector.
- **Substitution** — any command substitution `$(...)` / backticks or process substitution `<(...)` → deny.
- **Redirection** — output redirection (`>`, `>>`, `>|`, `<>`, `&>`) is denied unless the target is exactly `/dev/null`. `/dev/tcp`/`/dev/udp` are denied anywhere (input redirect or as an argument, including `--flag=/dev/tcp/…`). Descriptor duplication (`>&N`) is allowed — the child is passed only fds 0/1/2, so there is no leaked writable descriptor.
- **`git`** — only read-only subcommands (`log`, `diff`, `show`, `blame`, `status`, `grep`, `ls-files`, `rev-parse`, `cat-file`, `shortlog`, …). Subcommands with a mutating form (`commit`/`add`/`checkout`/`reset`/`branch`/`tag`/`remote`/`stash`/`config`) are excluded wholesale — they can write the shared `.git`. **Every git global flag before the subcommand is denied**, which closes the config-injection RCE vectors (`-c core.pager=…`, `-c diff.external=…`, `--exec-path`) in one rule; `git grep -O` (opens a pager) is denied explicitly.
- **`sed`** — restricted to print-only scripts: a line address (`N`, `N,M`, `$`, `N~step`, `/regex/`) with an optional trailing `p`. `-i`/`-f` and any `w`/`e`/`r`/`s` verb are rejected (the file-writers are also backstopped, but `e` executes a command and is not). The common `sed -ne '5,10p'` bundle and the `$`/regex/relative ranges a developer actually slices with are all allowed.
- **`find`** — `-delete` and the `-exec`/`-execdir`/`-ok`/`-okdir` family are denied (exec / tree mutation, un-backstopped); `-fprint*` file writes are allowed (backstopped).

## Considered Options

- **PreToolUse hook (chosen).** Survives `bypassPermissions`; keeps the no-prompt headless requirement; logic lives in Go, unit-tested.
- **Drop `bypassPermissions`, use `--allowedTools "Bash(cat:*)"`.** Rejected: re-introduces prompt-deadlock risk, and Claude Code's command-prefix matching can still admit a redirection write (`cat x > y` matches the `cat` prefix). Less robust than an AST allowlist.
- **Max-hardened guard (earlier iteration).** A first pass treated the cooperative model as an attacker and added per-tool micro-rules (`sort --compress-program`, `file --compile`, `rg --pre`, `>&3` fd-leak, `sed -l` arg-desync) hardened over two adversarial red-team rounds. **Rejected on review:** those rules defend against an attacker the threat model excludes, and the file-write vectors among them are already neutralized by the ephemeral worktree. They violated KISS without adding real safety for a cooperative run, and the git/pager exclusions they implied actively degraded the terminal fidelity this ADR exists to recover. The guard was cut from ~350 to ~190 lines to the core above.
- **Status quo (Bash disallowed, ADR-0006).** Rejected: the whole point is terminal-fidelity content loading; whole-file `Read` inflates the measurement.

## Consequences

- **Terminal-fidelity reads, including git.** The model slices files (`cat|head`, `sed -n`) and inspects history (`git log/diff/show/blame/grep`) the way a developer does, so the context measurement reflects a real session instead of whole-file ingestion.
- **Confidentiality is not a goal.** The guard permits reading any file the process can read (as `cat` always could); it prevents exec/network/durable-state mutation, not disclosure. The run executes in an ephemeral worktree.
- **File writes lean on the worktree, not the guard.** A command that writes a tracked file (`sed -i`, `sort -o`) may run, but the write is captured by `captureDiff` and discarded with the worktree. `captureDiff` should stay empty for a clean run; a non-empty diff flags a misbehaving probe, not a guard failure.
- **Shared-`.git` mutation is the one git risk the guard owns** (refs/objects survive the worktree reset), handled by the read-only subcommand allowlist + global-flag ban.
- **Defense-in-depth backstops remain:** `Edit`/`Write`/`WebFetch`/`Agent` stay disallowed; the worktree is ephemeral and `captureDiff`-reset.
- **One dependency added** (`mvdan.cc/sh/v3`, pure Go) and the module's `go` directive moved 1.24 → 1.25.
- The classifier is unit-tested (`internal/bashguard`, allow + deny tables covering the read idioms, git gate, sed forms, and the exec/network/substitution/env-prefix denials) and the settings shape is unit-tested (`internal/harness`); hook firing is verified by the gated dogfood test and live smoke runs.
