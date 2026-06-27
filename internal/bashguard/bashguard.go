package bashguard

import (
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

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

var gitReadSubcmds = map[string]bool{
	"log": true, "show": true, "diff": true, "diff-tree": true,
	"diff-index": true, "blame": true, "status": true, "grep": true,
	"ls-files": true, "ls-tree": true, "rev-parse": true, "rev-list": true,
	"describe": true, "shortlog": true, "cat-file": true, "whatchanged": true,
	"merge-base": true, "name-rev": true,
}

var findExecFlags = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
}

var sedSlice = regexp.MustCompile(`^(\$|[0-9]+|[0-9]+~[0-9]+|/[^/]+/)(,(\$|[0-9]+|\+[0-9]+|~[0-9]+|[0-9]+~[0-9]+|/[^/]+/))?p?$`)

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

		if s, ok := litOf(r.Word); ok && s == "/dev/null" {
			return ""
		}
		return "output redirection to a file is not allowed (only /dev/null)"
	}
}

func isNetDevice(s string) bool {
	return strings.Contains(s, "/dev/tcp/") || strings.Contains(s, "/dev/udp/")
}

func checkCall(c *syntax.CallExpr) string {
	if len(c.Args) == 0 {
		return ""
	}
	if len(c.Assigns) > 0 {

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
