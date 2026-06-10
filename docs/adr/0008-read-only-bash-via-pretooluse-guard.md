# Read-only Bash via a PreToolUse guard (amends ADR-0006)

**Status:** accepted — amends [ADR-0006](0006-headless-runs-read-only.md)

[ADR-0006](0006-headless-runs-read-only.md) made runs read-only by bare-name disallowing `Bash`. Measuring context cost exposed a side effect of that choice: with no `Bash`, the model loads file content via `Read` (whole files) and `Grep` (whole matches), where a terminal session slices with `cat | head`, `sed -n 'N,Mp'`, `grep | head`. On the wallet probe the headless run ingested ~4× the tool-result content of an equivalent manual session reading the *same* files — the single `wallet-client.ts` was 3,062 chars via `head -120` but 33,248 chars via whole-file `Read` (10.9×). That inflates resident-context measurements and makes the run a poor mirror of how a developer actually inspects a repo.

So `Bash` is re-enabled, constrained to **read-only** by a `PreToolUse` hook:

```
--disallowedTools Agent Edit Write WebFetch
--settings '{"hooks":{"PreToolUse":[{"matcher":"Bash",
  "hooks":[{"type":"command","command":"<self> __bash-read-guard"}]}]}}'
--permission-mode bypassPermissions  --disable-slash-commands
```

ADR-0006 rejected scoping Bash because *permission rules* (`Bash(cat:*)`) are not enforced under `bypassPermissions`. A `PreToolUse` hook is a different lever: it fires regardless of permission mode and a hook that exits non-zero **blocks the call before permission rules are evaluated**, so it holds under bypass — the property the scoped-rule approach lacked. The hook is the validator binary's hidden `__bash-read-guard` subcommand (`internal/cli/bashguard.go`); the classifier is `internal/bashguard`. Wiring it via `--settings` (precedence 2) overrides the worktree's own settings without editing the tree under test.

## The guard (`internal/bashguard.Classify`)

Allowlist-first, **fail closed**: a command is allowed only if it is *provably* read-only; anything unparseable or unresolvable is denied. It parses with `mvdan.cc/sh/v3/syntax` (a real shell AST, not regex — robust to quoting/escaping) and walks every node:

- **Command** — every simple command's name must be in a read-only allowlist (`cat`, `head`, `tail`, `grep`, `rg`, `ls`, `find`, `wc`, `sort`, `sed`, `git` read-subcommands, …). Command-runners (`env`, `xargs`, `sudo`, `time`, `command`, `exec`) and interpreters (`python`, `node`, `perl`, `awk`, `bash`, `sh`) are absent — they would hide the real command. Dynamic names (`$cmd`) → deny.
- **Redirection** — output redirection (`>`, `>>`, `>|`, `<>`, `&>`) is denied unless the target is `/dev/null` or an fd dup; bare `>file` is caught as a redirect even with no command. `/dev/tcp` and `/dev/udp` are denied on input too (no network).
- **Substitution** — any command substitution `$(...)` / backticks or process substitution `<(...)` → deny.
- **In-tool writers** — per-command checks: `sed -i`; `find -delete/-exec/-fprintf/…`; `git` write subcommands and `--output`; output-to-file flags on read tools (`sort -o`, `tree -o`, `yq -i`, `date -s`).

## Considered Options

- **PreToolUse hook (chosen).** Survives `bypassPermissions`; keeps the no-prompt headless requirement; logic lives in Go, unit- and adversarially-tested.
- **Drop `bypassPermissions`, use `--allowedTools "Bash(cat:*)"`.** Rejected: re-introduces prompt-deadlock risk, and Claude Code's command-prefix matching can still admit a redirection write (`cat x > y` matches the `cat` prefix). Less robust than an AST allowlist.
- **Status quo (Bash disallowed, ADR-0006).** Rejected: the whole point is terminal-fidelity content loading; whole-file `Read` inflates the measurement.

## Consequences

- **Read-only guarantee is best-effort-strong, not absolute.** It blocks every realistic file-creation/mutation/network/exec vector for the cooperative model under test. Defense-in-depth backstops remain: `Edit`/`Write`/`WebFetch`/`Agent` stay disallowed, and the worktree is ephemeral and `captureDiff`-reset, so any residual write is captured and discarded. Known accepted residuals: `sed`'s in-script `w`/`e` commands (write/exec) are not statically detected because distinguishing them from a `w`/`e` inside a regex needs a sed parser; the threat is nil for a cooperative model and bounded by the ephemeral worktree.
- **Diff capture may now be non-empty** if a read-only run shells out in a way that still touches the tree (it shouldn't); `captureDiff` continues to record and discard.
- **One dependency added** (`mvdan.cc/sh/v3`, pure Go) and the module's `go` directive moved 1.24 → 1.25.
- The guard is unit-tested (`internal/bashguard`) and adversarially red-teamed; no live `claude` in unit tests (per the repo's test shape) — the hook firing is verified by the gated dogfood test and live smoke runs.
