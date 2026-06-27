// Package autodetect resolves the Before and After committishes for the bare
// A/B benchmark from a target repo's git state (ADR-0008): a dirty tree compares
// HEAD against the working tree (temp-committed so uncommitted and new untracked
// env files count); a clean tree compares the fork point against HEAD. Either
// side is overridable. No claude, no run worktrees — pure git, so it is
// unit-testable against fixture repos.
package autodetect

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolution is the committish pair the bare benchmark runs, plus human labels
// for the spend plan. AfterVolatile is set when After is the synthetic
// working-tree snapshot rather than a committed ref, so the Run cache can skip it
// (it changes every iteration — ADR-0016 baseline-only cache).
type Resolution struct {
	Before        string
	After         string
	BeforeDesc    string
	AfterDesc     string
	Dirty         bool
	AfterVolatile bool
}

// Resolve picks Before and After for repo per ADR-0008. beforeOverride and
// afterOverride replace either side independently when non-empty.
func Resolve(ctx context.Context, repo, beforeOverride, afterOverride string) (Resolution, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve target: %w", err)
	}
	if _, err := git(ctx, abs, nil, "rev-parse", "--git-dir"); err != nil {
		return Resolution{}, fmt.Errorf("%s is not a git repository", abs)
	}

	dirty, err := isDirty(ctx, abs)
	if err != nil {
		return Resolution{}, err
	}
	res := Resolution{Dirty: dirty}

	switch {
	case afterOverride != "":
		sha, err := commitOf(ctx, abs, afterOverride)
		if err != nil {
			return Resolution{}, fmt.Errorf("--after %q: %w", afterOverride, err)
		}
		res.After, res.AfterDesc = sha, label(afterOverride, sha)
	case dirty:
		sha, err := tempCommitWorkingTree(ctx, abs)
		if err != nil {
			return Resolution{}, fmt.Errorf("capture working tree: %w", err)
		}
		res.After, res.AfterDesc, res.AfterVolatile = sha, "working tree — uncommitted edits ("+short(sha)+")", true
	default:
		sha, err := commitOf(ctx, abs, "HEAD")
		if err != nil {
			return Resolution{}, err
		}
		res.After, res.AfterDesc = sha, label("HEAD", sha)
	}

	switch {
	case beforeOverride != "":
		sha, err := commitOf(ctx, abs, beforeOverride)
		if err != nil {
			return Resolution{}, fmt.Errorf("--before %q: %w", beforeOverride, err)
		}
		res.Before, res.BeforeDesc = sha, label(beforeOverride, sha)
	case dirty:
		sha, err := commitOf(ctx, abs, "HEAD")
		if err != nil {
			return Resolution{}, err
		}
		res.Before, res.BeforeDesc = sha, label("HEAD", sha)
	default:
		db, err := defaultBranch(ctx, abs)
		if err != nil {
			return Resolution{}, err
		}
		mb, err := git(ctx, abs, nil, "merge-base", db, "HEAD")
		if err != nil {
			return Resolution{}, fmt.Errorf("merge-base %s HEAD: %w", db, err)
		}
		head, err := commitOf(ctx, abs, "HEAD")
		if err != nil {
			return Resolution{}, err
		}
		if mb == head {
			return Resolution{}, fmt.Errorf("clean tree at the default branch (%s): nothing to compare — pass --before <ref>", db)
		}
		res.Before, res.BeforeDesc = mb, "merge-base("+db+") "+short(mb)
	}

	return res, nil
}

// ResolveOne resolves a single Environment ref for the single-env assess (no
// comparison): the override when set, else the working tree — temp-committed when
// dirty so uncommitted and untracked env edits count, HEAD when clean. It mirrors
// the After half of Resolve without its A/B "nothing to compare" error, so a
// clean default branch still assesses fine. volatile is true only for the
// synthetic working-tree snapshot, so the Run cache skips it (ADR-0016).
func ResolveOne(ctx context.Context, repo, override string) (ref, desc string, volatile bool, err error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve target: %w", err)
	}
	if _, err := git(ctx, abs, nil, "rev-parse", "--git-dir"); err != nil {
		return "", "", false, fmt.Errorf("%s is not a git repository", abs)
	}
	if override != "" {
		sha, err := commitOf(ctx, abs, override)
		if err != nil {
			return "", "", false, fmt.Errorf("--ref %q: %w", override, err)
		}
		return sha, label(override, sha), false, nil
	}
	dirty, err := isDirty(ctx, abs)
	if err != nil {
		return "", "", false, err
	}
	if dirty {
		sha, err := tempCommitWorkingTree(ctx, abs)
		if err != nil {
			return "", "", false, fmt.Errorf("capture working tree: %w", err)
		}
		return sha, "working tree — uncommitted edits (" + short(sha) + ")", true, nil
	}
	sha, err := commitOf(ctx, abs, "HEAD")
	if err != nil {
		return "", "", false, err
	}
	return sha, label("HEAD", sha), false, nil
}

// tempCommitWorkingTree snapshots the working tree (tracked edits + untracked
// files, minus .gitignore) as a commit parented on HEAD, using a throwaway
// index so the target's real index, HEAD, and working tree are untouched. The
// commit is unreferenced — git GCs it later; the run worktree checks it out by
// SHA in the meantime.
func tempCommitWorkingTree(ctx context.Context, repo string) (string, error) {
	tmp, err := os.MkdirTemp("", "claude-benchmark-index-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	if _, err := git(ctx, repo, env, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := git(ctx, repo, env, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := git(ctx, repo, env, "write-tree")
	if err != nil {
		return "", err
	}
	return git(ctx, repo, env, "commit-tree", tree, "-p", "HEAD", "-m", "claude-benchmark: working tree under test")
}

// defaultBranch is origin/HEAD when set, else a local main/master, else an
// error asking for --before.
func defaultBranch(ctx context.Context, repo string) (string, error) {
	if ref, err := git(ctx, repo, nil, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimPrefix(ref, "refs/remotes/"), nil
	}
	for _, c := range []string{"main", "master"} {
		if _, err := git(ctx, repo, nil, "rev-parse", "--verify", "--quiet", c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("cannot determine the default branch (no origin/HEAD, main, or master) — pass --before <ref>")
}

func isDirty(ctx context.Context, repo string) (bool, error) {
	out, err := git(ctx, repo, nil, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func commitOf(ctx context.Context, repo, ref string) (string, error) {
	return git(ctx, repo, nil, "rev-parse", "--verify", ref+"^{commit}")
}

func label(ref, sha string) string { return ref + " (" + short(sha) + ")" }

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func git(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=claude-benchmark", "GIT_AUTHOR_EMAIL=benchmark@localhost",
		"GIT_COMMITTER_NAME=claude-benchmark", "GIT_COMMITTER_EMAIL=benchmark@localhost",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
