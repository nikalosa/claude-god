package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nikalosa/claude-god/internal/parser"
)

// worktreeMu serializes `git worktree add`/`remove` against the target repo's
// shared .git: concurrent runs would otherwise race on git's index lock. Held
// only across the millisecond-scale git call, never across the run itself.
var worktreeMu sync.Mutex

type Opts struct {
	TargetRepo    string
	Branch        string
	Prompt        string
	NoMemSnapshot bool
	MemorySource  string
}

type Result struct {
	Record       *parser.RunRecord
	StreamPath   string
	DiffPath     string
	DiffStatPath string
	WorktreePath string
}

func Run(ctx context.Context, opts Opts) (*Result, error) {
	if !filepath.IsAbs(opts.TargetRepo) {
		return nil, errors.New("TargetRepo must be absolute")
	}
	if opts.Branch == "" {
		return nil, errors.New("Branch required")
	}
	if opts.Prompt == "" {
		return nil, errors.New("Prompt required")
	}

	artifacts, err := os.MkdirTemp("", "claude-validator-run-*")
	if err != nil {
		return nil, fmt.Errorf("artifacts dir: %w", err)
	}

	wt := filepath.Join(artifacts, "wt")
	// Only the worktree admin op races on the shared .git, so lock just that
	// (fast: --no-checkout writes no files) and populate the tree outside the
	// lock. The slow checkout then parallelizes across runs instead of
	// serializing one-at-a-time. Per-run worktree keeps each claude session in
	// its own cwd (claude keys project state by realpath, so a shared cwd would
	// collide under concurrency).
	worktreeMu.Lock()
	addErr := gitC(ctx, opts.TargetRepo, "worktree", "add", "--no-checkout", "--detach", wt, opts.Branch)
	worktreeMu.Unlock()
	if addErr != nil {
		return nil, fmt.Errorf("worktree add: %w", addErr)
	}
	if err := gitC(ctx, wt, "reset", "--hard", "HEAD"); err != nil {
		return nil, fmt.Errorf("worktree checkout: %w", err)
	}
	defer func() {
		worktreeMu.Lock()
		_ = gitC(context.Background(), opts.TargetRepo, "worktree", "remove", "--force", wt)
		worktreeMu.Unlock()
	}()

	if src := memorySource(wt, opts); src != "" {
		restore, err := swapMemory(wt, src)
		if err != nil {
			return nil, fmt.Errorf("memory swap: %w", err)
		}
		defer restore()
	}

	streamPath := filepath.Join(artifacts, "stream.jsonl")
	if err := invokeClaude(ctx, wt, opts.Prompt, streamPath); err != nil {
		return nil, fmt.Errorf("claude -p: %w", err)
	}

	f, err := os.Open(streamPath)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	rec, parseErr := parser.Parse(f)
	_ = f.Close()
	if parseErr != nil {
		return nil, fmt.Errorf("parse stream: %w", parseErr)
	}

	diffPath := filepath.Join(artifacts, "diff.patch")
	diffStatPath := filepath.Join(artifacts, "diff.stat")
	if err := captureDiff(ctx, wt, diffPath, diffStatPath); err != nil {
		return nil, fmt.Errorf("capture diff: %w", err)
	}

	return &Result{
		Record:       rec,
		StreamPath:   streamPath,
		DiffPath:     diffPath,
		DiffStatPath: diffStatPath,
		WorktreePath: wt,
	}, nil
}

// memorySource picks what to inject into the run worktree: an explicit live
// source (the bare command's current project memory) wins; --no-memory-snapshot
// disables injection; otherwise the committed snapshot pinned in the worktree.
func memorySource(worktree string, opts Opts) string {
	if opts.MemorySource != "" {
		return opts.MemorySource
	}
	if opts.NoMemSnapshot {
		return ""
	}
	return filepath.Join(worktree, ".validator", "memory-snapshot")
}

func swapMemory(worktree, src string) (restore func(), err error) {
	if _, statErr := os.Stat(src); statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprintf(os.Stderr, "warning: no project memory at %s; proceeding without injection\n", src)
			return func() {}, nil
		}
		return nil, statErr
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	slug := strings.ReplaceAll(worktree, "/", "-")
	dest := filepath.Join(home, ".claude", "projects", slug, "memory")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := copyDir(src, dest); err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	return func() {
		_ = os.RemoveAll(filepath.Join(home, ".claude", "projects", slug))
	}, nil
}

func invokeClaude(ctx context.Context, cwd, prompt, streamPath string) error {
	out, err := os.Create(streamPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Runs are read-only (ADR-0006): grading reads the assistant text, so the
	// model needs Read/Grep/Glob to inspect the env but nothing that mutates the
	// tree, hits the network, or opens a browser. Bare-name disallows hold even
	// under bypassPermissions; --disable-slash-commands drops skills, which are
	// not part of the Environment under test.
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		"--disallowedTools", "Agent", "Bash", "Edit", "Write", "WebFetch",
		"--disable-slash-commands")
	cmd.Dir = cwd
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func captureDiff(ctx context.Context, worktree, diffPath, diffStatPath string) error {
	if err := gitC(ctx, worktree, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := gitToFile(ctx, worktree, diffPath, "diff", "--cached"); err != nil {
		return err
	}
	if err := gitToFile(ctx, worktree, diffStatPath, "diff", "--cached", "--stat"); err != nil {
		return err
	}
	if err := gitC(ctx, worktree, "reset"); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}
	return nil
}

func gitC(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitToFile(ctx context.Context, dir, path string, args ...string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
