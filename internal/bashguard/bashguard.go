// Package bashguard classifies a shell command as read-only or not. It is the
// enforcement core behind the harness's PreToolUse hook: headless runs may shell
// out to inspect the environment (cat/head/sed/grep/git, like a terminal) so the
// context measurement mirrors a real session, but must not run arbitrary code,
// install packages, hit the network, or mutate state a run reset cannot undo
// (ADR-0006, ADR-0009).
//
// The model under test is cooperative, so the bar is not an airtight sandbox; it
// is to block the actions the run's backstop cannot reverse. Runs execute in an
// ephemeral git worktree whose tracked-file writes are captured and discarded, so
// the guard deliberately does NOT chase in-tool file writes (sort -o, find
// -fprint, uniq IN OUT). It blocks the un-backstopped classes: command-runners,
// interpreters, command/process substitution, the network, and git subcommands
// that would mutate the SHARED .git (refs/objects a worktree reset does not
// restore). Output-to-a-file is still blocked so a run cannot read a file it just
// wrote and mistake it for the environment.
package bashguard

import (
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// readOnlyCmds cannot, by themselves, run another program or reach the network.
// Command-runners (env, xargs, sudo, time, command, exec) and interpreters
// (python, node, perl, awk, bash, sh) are absent: they hide the real command.
var readOnlyCmds = map[string]bool{
	"cat": true, "head": true, "tail": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true, "ls": true, "find": true, "wc": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "comm": true, "diff": true,
	"stat": true, "file": true, "basename": true, "dirname": true,
	"realpath": true, "readlink": true, "tree": true, "echo": true,
	"printf": true, "pwd": true, "true": true, ":": true, "nl": true,
	"fold": true, "od": true, "hexdump": true, "jq": true,
	"column": true, "sed": true, "cd": true, "test": true, "[": true,
	"strings": true, "git": true,
}

// gitReadSubcmds are git subcommands that only ever read. Any subcommand with a
// mutating form (commit/add/checkout/reset/branch/tag/remote/stash/config/...) is
// excluded wholesale: it can write the worktree's SHARED .git (refs, objects),
// which the worktree reset does not restore. A read subcommand that writes a file
// in the cwd (git diff --output) is fine — that is backstopped.
var gitReadSubcmds = map[string]bool{
	"log": true, "show": true, "diff": true, "diff-tree": true,
	"diff-index": true, "blame": true, "status": true, "grep": true,
	"ls-files": true, "ls-tree": true, "rev-parse": true, "rev-list": true,
	"describe": true, "shortlog": true, "cat-file": true, "whatchanged": true,
	"merge-base": true, "name-rev": true,
}

// findExecFlags turn find into an executor or tree mutator — the part a worktree
// reset cannot undo. Pure -fprint* file writes are omitted: they are backstopped.
var findExecFlags = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
}

// sedSlice matches a print-only sed script: a line address (numeric, $, N~step,
// or /regex/) optionally to a second address (numeric, $, +N, ~N, /regex/), with
// an optional trailing p. No w/e/r/s verb can match it, so no sed script that
// writes a file or executes a command is accepted — use grep for anything richer.
var sedSlice = regexp.MustCompile(`^(\$|[0-9]+|[0-9]+~[0-9]+|/[^/]+/)(,(\$|[0-9]+|\+[0-9]+|~[0-9]+|[0-9]+~[0-9]+|/[^/]+/))?p?$`)

// Classify reports whether command is provably read-only. On any parse failure
// or unresolvable construct it returns (false, reason) — fail closed.
func Classify(command string) (allow bool, reason string) {
	src := strings.TrimSpace(command)
	if src == "" {
		return true, ""
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		return false, "unparseable command (guard fails closed): " + err.Error()
	}

	var deny string
	syntax.Walk(file, func(node syntax.Node) bool {
		if deny != "" {
			return false
		}
		switch n := node.(type) {
		case *syntax.CmdSubst:
			deny = "command substitution $(...) / `...` is not allowed"
		case *syntax.ProcSubst:
			deny = "process substitution <(...) / >(...) is not allowed"
		case *syntax.Redirect:
			deny = checkRedirect(n)
		case *syntax.CallExpr:
			deny = checkCall(n)
		}
		return deny == ""
	})
	if deny != "" {
		return false, deny
	}
	return true, ""
}

// checkRedirect blocks output redirection to a file (so a run cannot read back
// something it wrote) and bash's network pseudo-devices. Descriptor duplication
// (>&N) is allowed: the harness passes the child only fds 0/1/2, so there is no
// leaked writable descriptor, and any write a dup reached lands in the reset
// worktree.
func checkRedirect(r *syntax.Redirect) string {
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.DplIn:
		if s, ok := litOf(r.Word); ok && isNetDevice(s) {
			return "network redirection (/dev/tcp, /dev/udp) is not allowed"
		}
		return ""
	case syntax.DplOut:
		return ""
	default:
		// RdrOut >, AppOut >>, ClbOut >|, RdrInOut <>, RdrAll &>, AppAll &>>.
		if s, ok := litOf(r.Word); ok && s == "/dev/null" {
			return ""
		}
		return "output redirection to a file is not allowed (only /dev/null)"
	}
}

// isNetDevice matches bash's network pseudo-devices anywhere in a word, so both
// `</dev/tcp/h/p` and `--random-source=/dev/tcp/h/p` are caught.
func isNetDevice(s string) bool {
	return strings.Contains(s, "/dev/tcp/") || strings.Contains(s, "/dev/udp/")
}

// checkCall validates one simple command, including its environment prefix.
func checkCall(c *syntax.CallExpr) string {
	if len(c.Args) == 0 {
		return "" // pure assignment (e.g. FOO=bar) with no command — harmless
	}
	if len(c.Assigns) > 0 {
		// `GIT_EXTERNAL_DIFF=evil git diff`, `LESSOPEN='|sh' less` etc. — an inline
		// env prefix is an exec/behaviour-injection vector and is never needed to
		// read a file.
		return "inline environment assignment before a command is not allowed"
	}
	return checkCommand(c.Args)
}

func checkCommand(args []*syntax.Word) string {
	name, ok := litOf(args[0])
	if !ok {
		return "dynamic command name is not allowed"
	}
	if strings.ContainsRune(name, '/') {
		// `./cat`, `bin/cat`, `../cat` resolve to a path.Base in the allowlist but
		// run an attacker-controlled file. A read-only run only needs PATH-resolved
		// bare commands.
		return "explicit command paths are not allowed; use a bare command name: " + name
	}
	if !readOnlyCmds[name] {
		return "command not in the read-only allowlist: " + name
	}
	for _, w := range args[1:] {
		if s, ok := litOf(w); ok && isNetDevice(s) {
			return "network pseudo-device argument (/dev/tcp, /dev/udp) is not allowed"
		}
	}
	switch name {
	case "sed":
		return checkSed(args)
	case "find":
		return checkFind(args)
	case "git":
		return checkGit(args)
	}
	return ""
}

// checkGit allows only read-only subcommands and forbids every git global flag
// before the subcommand — that single rule blocks the config-injection RCE
// vectors (`-c core.pager=…`, `-c diff.external=…`, `--exec-path`). The only
// subcommand-level exec vector among the read subcommands is `git grep -O`
// (opens a pager), which is denied explicitly.
func checkGit(args []*syntax.Word) string {
	sub := ""
	for _, w := range args[1:] {
		s, ok := litOf(w)
		if !ok {
			return "git with a dynamic argument is not allowed"
		}
		if sub == "" {
			if strings.HasPrefix(s, "-") {
				return "git global flags are not allowed (config-injection vector): " + s
			}
			sub = s
			if !gitReadSubcmds[sub] {
				return "git subcommand is not in the read-only allowlist: " + sub
			}
			continue
		}
		if s == "--open-files-in-pager" || strings.HasPrefix(s, "--open-files-in-pager=") ||
			(sub == "grep" && (s == "-O" || strings.HasPrefix(s, "-O"))) {
			return "git grep --open-files-in-pager (-O) runs a pager (exec) and is not allowed"
		}
	}
	if sub == "" {
		return "git requires a read-only subcommand"
	}
	return ""
}

// checkSed allows sed only as a print-only slicer: -n/-E/-r/-s/-u/-z flags and -e
// expression scripts, where every script is a print-only line range (see
// sedSlice). It rejects -i/-f and any script with a w/e/r/s verb. A bundled
// trailing -e (the common `-ne 'script'`) routes its next token as the script.
func checkSed(args []*syntax.Word) string {
	var scripts []string
	sawExpr := false
	firstBareIsScript := true
	pendingExpr := false
	for _, w := range args[1:] {
		s, ok := litOf(w)
		if !ok {
			return "sed with a dynamic argument is not allowed"
		}
		if pendingExpr {
			scripts = append(scripts, s)
			sawExpr = true
			pendingExpr = false
			continue
		}
		if strings.HasPrefix(s, "--") {
			switch {
			case strings.HasPrefix(s, "--in-place"):
				return "sed in-place edit is not allowed"
			case strings.HasPrefix(s, "--file"):
				return "sed -f (external script) is not allowed"
			case s == "--expression":
				pendingExpr = true
			case strings.HasPrefix(s, "--expression="):
				scripts = append(scripts, strings.TrimPrefix(s, "--expression="))
				sawExpr = true
			case s == "--quiet" || s == "--silent" || s == "--regexp-extended" ||
				s == "--null-data" || s == "--separate" || s == "--unbuffered" || s == "--posix":
			default:
				return "sed flag not allowed: " + s
			}
			continue
		}
		if strings.HasPrefix(s, "-") && s != "-" {
			body := s[1:]
			for i := 0; i < len(body); i++ {
				c := body[i]
				if c == 'e' {
					if rest := body[i+1:]; rest != "" {
						scripts = append(scripts, rest)
						sawExpr = true
					} else {
						pendingExpr = true
					}
					break
				}
				if !strings.ContainsRune("nrEzsu", rune(c)) {
					return "sed flag not allowed: " + s
				}
			}
			continue
		}
		if !sawExpr && firstBareIsScript {
			scripts = append(scripts, s)
			firstBareIsScript = false
			continue
		}
		// remaining bare args are input file paths — read-only
	}
	if len(scripts) == 0 {
		return "sed requires an explicit print-only script"
	}
	for _, sc := range scripts {
		for _, part := range strings.Split(sc, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !sedSlice.MatchString(part) {
				return "sed script must be a print-only line range (N, N,M, $, /regex/ with optional p); use grep for anything else: " + sc
			}
		}
	}
	return ""
}

func checkFind(args []*syntax.Word) string {
	for _, w := range args[1:] {
		s, ok := litOf(w)
		if !ok {
			continue
		}
		if findExecFlags[s] {
			return "find action that executes or deletes is not allowed: " + s
		}
	}
	return ""
}

// litOf resolves a word to its static string value, reporting ok=false if any
// part is dynamic (parameter/command expansion), since a dynamic value cannot be
// vetted statically.
func litOf(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				lit, ok := dp.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}
