// Package bashguard classifies a shell command as read-only or not. It is the
// enforcement core behind the harness's PreToolUse hook: headless runs may shell
// out to inspect the Environment (cat/head/sed/grep, like a terminal) but must
// never create or mutate a file, install packages, or hit the network
// (ADR-0006). The model under test is cooperative, not adversarial, but the
// guarantee must hold regardless, so the classifier is allowlist-first and fails
// closed: anything it cannot prove read-only is denied.
package bashguard

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// readOnlyCmds are commands that cannot, by themselves, write a file or reach
// the network. Commands that run other commands (env, xargs, sudo, time,
// timeout, watch, nice, command, exec) are deliberately absent: they would hide
// the real command from this check. Interpreters (python, node, perl, ruby, awk,
// php, bash, sh) are absent for the same reason — they can write. git is handled
// specially (read subcommands only). tee/dd/cp/mv/rm/mkdir/touch/ln/truncate/
// install/chmod/chown are absent because they mutate.
var readOnlyCmds = map[string]bool{
	"cat": true, "head": true, "tail": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true, "ls": true, "find": true, "wc": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "comm": true, "diff": true,
	"stat": true, "file": true, "basename": true, "dirname": true,
	"realpath": true, "readlink": true, "tree": true, "echo": true,
	"printf": true, "pwd": true, "true": true, ":": true, "nl": true,
	"fold": true, "od": true, "xxd": true, "hexdump": true, "jq": true,
	"yq": true, "column": true, "sed": true, "cd": true, "test": true,
	"[": true, "less": true, "more": true, "bat": true, "strings": true,
	"date": true, "git": true,
}

// gitReadSubcmds are git subcommands that are read-only for any arguments.
// Ambiguous ones (remote, tag, branch, config, symbolic-ref) are excluded
// because they read with some args and write with others.
var gitReadSubcmds = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true, "ls-files": true,
	"ls-tree": true, "cat-file": true, "blame": true, "grep": true,
	"rev-parse": true, "describe": true, "shortlog": true, "rev-list": true,
	"name-rev": true, "for-each-ref": true, "whatchanged": true,
	"count-objects": true, "show-ref": true, "var": true,
}

// gitValueFlags are global git flags that consume the following token, so the
// subcommand is two tokens further on (e.g. `git -C /path log`).
var gitValueFlags = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true,
}

// writeFlags map an otherwise-read command to flags that redirect its output to
// a named file (so the command itself writes, with no shell redirection to
// catch). Long flags also match their `--flag=value` form; short flags also
// match their attached-value form (e.g. `-ofile`).
var writeFlags = map[string][]string{
	"sort": {"-o", "--output"},
	"tree": {"-o", "--output"},
	"yq":   {"-i", "--inplace"},
	"git":  {"--output", "--output-directory"},
	"date": {"-s", "--set"},
}

// findWriteFlags turn find from a pure query into a mutator.
var findWriteFlags = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true,
	"-okdir": true, "-fprint": true, "-fprintf": true, "-fls": true,
	"-fprint0": true,
}

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
			if len(n.Args) > 0 {
				deny = checkCommand(n.Args)
			}
		}
		return deny == ""
	})
	if deny != "" {
		return false, deny
	}
	return true, ""
}

// checkRedirect blocks any output redirection whose target is not the null
// device or a file-descriptor duplication. Input redirections are read-only.
func checkRedirect(r *syntax.Redirect) string {
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.DplIn:
		// Input is read-only, except bash's network pseudo-devices, which open a
		// socket (no network during runs — ADR-0006).
		if s, ok := litOf(r.Word); ok && isNetDevice(s) {
			return "network redirection (/dev/tcp, /dev/udp) is not allowed"
		}
		return ""
	case syntax.DplOut:
		// `2>&1`, `>&2`: duplicating a descriptor writes nothing to disk.
		if s, ok := litOf(r.Word); ok && isFD(s) {
			return ""
		}
		return "output descriptor duplication to a non-descriptor target is not allowed"
	default:
		// RdrOut >, AppOut >>, ClbOut >|, RdrInOut <>, RdrAll &>, AppAll &>>.
		s, ok := litOf(r.Word)
		if ok && (s == "/dev/null" || isFD(s)) {
			return ""
		}
		return "output redirection to a file is not allowed (only /dev/null)"
	}
}

func isNetDevice(s string) bool {
	return strings.HasPrefix(s, "/dev/tcp/") || strings.HasPrefix(s, "/dev/udp/")
}

func isFD(s string) bool {
	s = strings.TrimPrefix(s, "&")
	if s == "" || s == "-" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// checkCommand validates the head command of one simple command.
func checkCommand(args []*syntax.Word) string {
	name, ok := litOf(args[0])
	if !ok {
		return "dynamic command name is not allowed"
	}
	base := name
	if strings.ContainsRune(name, '/') {
		base = path.Base(name) // /usr/bin/grep -> grep; ./script.sh -> script.sh
	}
	if !readOnlyCmds[base] {
		return "command not in the read-only allowlist: " + base
	}
	if d := checkWriteFlags(base, args[1:]); d != "" {
		return d
	}
	switch base {
	case "git":
		return checkGit(args)
	case "sed":
		return checkSed(args)
	case "find":
		return checkFind(args)
	}
	return ""
}

func checkWriteFlags(base string, rest []*syntax.Word) string {
	flags := writeFlags[base]
	if flags == nil {
		return ""
	}
	for _, w := range rest {
		s, ok := litOf(w)
		if !ok {
			continue
		}
		for _, f := range flags {
			long := strings.HasPrefix(f, "--")
			if s == f ||
				(long && strings.HasPrefix(s, f+"=")) ||
				(!long && len(s) > len(f) && strings.HasPrefix(s, f)) {
				return base + " " + f + " writes to a file; not allowed"
			}
		}
	}
	return ""
}

func checkGit(args []*syntax.Word) string {
	skipNext := false
	for _, w := range args[1:] {
		s, ok := litOf(w)
		if !ok {
			return "git with a dynamic argument is not allowed"
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(s, "-") {
			if gitValueFlags[s] {
				skipNext = true
			}
			continue
		}
		if !gitReadSubcmds[s] {
			return "git subcommand not in the read-only allowlist: " + s
		}
		return ""
	}
	return "" // bare `git` prints usage
}

func checkSed(args []*syntax.Word) string {
	for _, w := range args[1:] {
		s, ok := litOf(w)
		if !ok {
			continue // a dynamic arg here is a script/path, not a write flag
		}
		if s == "--in-place" || s == "-i" || strings.HasPrefix(s, "-i") {
			return "sed in-place edit (-i) is not allowed"
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
		if findWriteFlags[s] {
			return "find action that mutates or executes is not allowed: " + s
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
