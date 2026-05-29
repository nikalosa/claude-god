// Package snapshot pins a target repo's Environment to a validator/<name>
// branch the run/calibrate commands consume, so the Before/After comparison is
// reproducible and the developer is not hand-managing git branches.
//
// The branch captures the committed HEAD tree (CLAUDE.md root+nested, Claude
// rules, docs) plus, by default, the project memory copied into
// .validator/memory-snapshot (where the harness's memory swap reads it). It is
// built in a throwaway worktree so the developer's working tree is untouched.
// Commit your Environment edits before snapshotting: the snapshot reflects HEAD.
package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Opts struct {
	TargetRepo    string
	Name          string
	IncludeMemory bool
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Create captures the target's Environment as the branch validator/<name> and
// returns the branch name. Re-snapshotting overwrites the branch.
func Create(ctx context.Context, opts Opts) (string, error) {
	if opts.Name == "" {
		return "", fmt.Errorf("snapshot name is required")
	}
	if !nameRe.MatchString(opts.Name) || strings.Contains(opts.Name, "..") {
		return "", fmt.Errorf("invalid snapshot name %q (use letters, digits, . _ -; no \"..\")", opts.Name)
	}
	abs, err := filepath.Abs(opts.TargetRepo)
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	if _, err := git(ctx, abs, "rev-parse", "--git-dir"); err != nil {
		return "", fmt.Errorf("%s is not a git repository", abs)
	}
	branch := "validator/" + opts.Name

	if !opts.IncludeMemory {
		return branch, pointBranch(ctx, abs, branch, "HEAD")
	}

	memDir, err := MemoryDir(abs)
	if err != nil {
		return "", err
	}
	hasMem, err := dirHasFiles(memDir)
	if err != nil {
		return "", err
	}
	if !hasMem {
		fmt.Fprintf(os.Stderr, "snapshot: no project memory at %s; pinning the environment without memory\n", memDir)
		return branch, pointBranch(ctx, abs, branch, "HEAD")
	}

	return branch, snapshotWithMemory(ctx, abs, branch, opts.Name, memDir)
}

// snapshotWithMemory builds the snapshot commit in a throwaway detached worktree
// (HEAD tree + injected memory), then points the branch at it.
func snapshotWithMemory(ctx context.Context, repo, branch, name, memDir string) error {
	tmp, err := os.MkdirTemp("", "claude-validator-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	wt := filepath.Join(tmp, "wt")
	if _, err := git(ctx, repo, "worktree", "add", "--detach", wt, "HEAD"); err != nil {
		return fmt.Errorf("worktree add: %w", err)
	}
	defer func() { _, _ = git(context.Background(), repo, "worktree", "remove", "--force", wt) }()

	dst := filepath.Join(wt, ".validator", "memory-snapshot")
	if err := copyDir(memDir, dst); err != nil {
		return fmt.Errorf("copy memory: %w", err)
	}
	// -f so memory lands even if the target .gitignores .validator.
	if _, err := git(ctx, wt, "add", "-f", ".validator/memory-snapshot"); err != nil {
		return err
	}
	if _, err := git(ctx, wt, "commit", "-m", "validator snapshot "+name+": pin environment + memory"); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}
	commit, err := git(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	return pointBranch(ctx, repo, branch, commit)
}

func pointBranch(ctx context.Context, repo, branch, at string) error {
	if _, err := git(ctx, repo, "branch", "-f", branch, at); err != nil {
		return fmt.Errorf("point %s at %s: %w", branch, at, err)
	}
	return nil
}

// MemoryDir returns the canonical project-memory directory for a target repo:
// ~/.claude/projects/<abs-target-with-slashes-as-dashes>/memory.
func MemoryDir(targetRepo string) (string, error) {
	abs, err := filepath.Abs(targetRepo)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(abs, "/", "-")
	return filepath.Join(home, ".claude", "projects", slug, "memory"), nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=claude-validator", "GIT_AUTHOR_EMAIL=validator@localhost",
		"GIT_COMMITTER_NAME=claude-validator", "GIT_COMMITTER_EMAIL=validator@localhost",
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func dirHasFiles(dir string) (bool, error) {
	found := false
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			found = true
		}
		return nil
	})
	if os.IsNotExist(err) {
		return false, nil
	}
	return found, err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
